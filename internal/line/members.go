package line

import (
	"encoding/json"
	"sort"
	"strconv"

	"github.com/ncruces/go-sqlite3"
)

type Member struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	IsMe         bool   `json:"is_me,omitempty"`
	Role         string `json:"role,omitempty"`
	State        string `json:"state,omitempty"`
	Invited      bool   `json:"invited,omitempty"`
	JoinedAt     string `json:"joined_at,omitempty"`
	ChatID       string `json:"chat_id,omitempty"`
	MessageCount int64  `json:"message_count"`
	LastSentAt   string `json:"last_sent_at,omitempty"`
	lastMs       int64
}

type MembersOptions struct {
	ChatArg string
	Limit   int
}

type MemberList struct {
	Chat    ChatSummary
	Members []Member
	Total   int
	Note    string
}

var squareRoles = map[int64]string{1: "admin", 2: "co_admin", 10: "member"}

var squareStates = map[int64]string{1: "join_requested", 2: "joined", 3: "rejected", 4: "left", 5: "kicked", 6: "banned", 7: "deleted"}

func codeName(names map[int64]string, prefix string, v int64) string {
	if s, ok := names[v]; ok {
		return s
	}
	return prefix + strconv.FormatInt(v, 10)
}

type senderStat struct {
	count    int64
	lastMs   int64
	fromName string
}

// chatSenders counts each sender's messages in a chat, leaving out system
// events, notices and unsent messages, with the name LINE stored on their latest one.
func (r *Reader) chatSenders(chatID string) (map[string]senderStat, error) {
	out := map[string]senderStat{}
	err := r.scan("SELECT _from, count(*), max(_createdTime), _contentInfo FROM _message WHERE _chatId=? AND "+sqlPlainKind+" GROUP BY _from", func(st *sqlite3.Stmt) {
		out[st.ColumnText(0)] = senderStat{st.ColumnInt64(1), st.ColumnInt64(2), metaString(parseMeta(st.ColumnText(3)), "fromName")}
	}, chatID)
	return out, err
}

// Members lists who is in a chat: a group's members and invitees, the known
// members of an open chat's community, or you and the other person in a 1:1.
func (r *Reader) Members(opt MembersOptions) (*MemberList, error) {
	chat, err := r.ResolveChatInfo(opt.ChatArg, false)
	if err != nil {
		return nil, err
	}
	if err := r.buildContacts(); err != nil {
		return nil, err
	}
	if err := r.buildOwnMids(); err != nil {
		return nil, err
	}
	stats, err := r.chatSenders(chat.ChatID)
	if err != nil {
		return nil, err
	}
	chats, err := r.loadChats()
	if err != nil {
		return nil, err
	}
	hasChat := map[string]bool{}
	for _, c := range chats {
		hasChat[c.ChatID] = true
	}
	res := &MemberList{Chat: chat}
	var out []Member
	add := func(id, name string) *Member {
		s := stats[id]
		m := Member{ID: id, Name: name, IsMe: r.ownMids[id], MessageCount: s.count, LastSentAt: tsToIso(s.lastMs), lastMs: s.lastMs}
		if m.Name == "" {
			m.Name = r.senderName(id)
		}
		if m.Name == id && s.fromName != "" {
			m.Name = s.fromName
		}
		if hasChat[id] {
			m.ChatID = id
		}
		out = append(out, m)
		return &out[len(out)-1]
	}
	switch chat.Type {
	case "group":
		found, err := r.groupMembers(chat.ChatID, add)
		if err != nil {
			return nil, err
		}
		if !found {
			res.Note = "no member list is stored for this group; listing the people who sent messages in it"
			addSenders(stats, add)
		}
	case "open", "thread":
		found, err := r.squareMembers(chat.ChatID, chat.Type, add)
		if err != nil {
			return nil, err
		}
		res.Note = "open-chat members are the users LINE has seen in this community, including people who left (see state); not a complete member list"
		if !found {
			res.Note = "no member list is stored for this open chat; listing the people who sent messages in it"
			addSenders(stats, add)
		}
	case "personal", "official", "self":
		if r.maps.self != "" {
			add(r.maps.self, r.maps.selfName)
		}
		if chat.Type != "self" {
			add(chat.ChatID, "")
		}
	default:
		res.Note = "no member list is stored for this chat; listing the people who sent messages in it"
		addSenders(stats, add)
	}
	sortMembers(out)
	res.Total = len(out)
	if limit := saneLimit(opt.Limit, DefaultMembersLimit); len(out) > limit {
		out = out[:limit]
	}
	res.Members = out
	if res.Members == nil {
		res.Members = []Member{}
	}
	return res, nil
}

func addSenders(stats map[string]senderStat, add func(id, name string) *Member) {
	for id := range stats {
		if id != "" {
			add(id, "")
		}
	}
}

func (r *Reader) groupMembers(chatID string, add func(id, name string) *Member) (bool, error) {
	var extra string
	found := false
	if err := r.scan("SELECT _extra FROM _groupChat WHERE _chatMid=?", func(st *sqlite3.Stmt) {
		extra, found = st.ColumnText(0), true
	}, chatID); err != nil || !found {
		return false, err
	}
	var ex struct {
		GroupExtra struct {
			MemberMids  map[string]float64 `json:"memberMids"`
			InviteeMids map[string]float64 `json:"inviteeMids"`
			Creator     string             `json:"creator"`
		} `json:"groupExtra"`
	}
	if json.Unmarshal([]byte(extra), &ex) != nil || len(ex.GroupExtra.MemberMids) == 0 {
		return false, nil
	}
	g := ex.GroupExtra
	for id, ts := range g.MemberMids {
		m := add(id, "")
		m.State, m.JoinedAt = "joined", tsToIso(int64(ts))
		if id == g.Creator {
			m.Role = "creator"
		}
	}
	for id := range g.InviteeMids {
		if _, ok := g.MemberMids[id]; !ok {
			m := add(id, "")
			m.State, m.Invited = "invited", true
		}
	}
	return true, nil
}

func (r *Reader) squareMembers(chatID, typ string, add func(id, name string) *Member) (bool, error) {
	q := "SELECT _squareMid FROM _squareChat WHERE _squareChatMid=?"
	if typ == "thread" {
		q = "SELECT _squareMid FROM _squareThread WHERE _threadMid=?"
	}
	var square string
	if err := r.scan(q, func(st *sqlite3.Stmt) { square = st.ColumnText(0) }, chatID); err != nil || square == "" {
		return false, err
	}
	found := false
	err := r.scan("SELECT _squareMemberMid, _displayName, _role, _membershipState FROM _squareMember WHERE _squareMid=?", func(st *sqlite3.Stmt) {
		found = true
		m := add(st.ColumnText(0), st.ColumnText(1))
		m.Role = codeName(squareRoles, "role_", st.ColumnInt64(2))
		m.State = codeName(squareStates, "state_", st.ColumnInt64(3))
	}, square)
	return found, err
}

// sortMembers puts current members first, then admins, then the most active.
func sortMembers(ms []Member) {
	stateRank := func(s string) int {
		switch s {
		case "", "joined":
			return 0
		case "invited":
			return 1
		}
		return 2
	}
	roleRank := func(s string) int {
		switch s {
		case "admin", "creator":
			return 0
		case "co_admin":
			return 1
		}
		return 2
	}
	sort.SliceStable(ms, func(i, j int) bool {
		a, b := ms[i], ms[j]
		if x, y := stateRank(a.State), stateRank(b.State); x != y {
			return x < y
		}
		if x, y := roleRank(a.Role), roleRank(b.Role); x != y {
			return x < y
		}
		if a.MessageCount != b.MessageCount {
			return a.MessageCount > b.MessageCount
		}
		if a.lastMs != b.lastMs {
			return a.lastMs > b.lastMs
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
}
