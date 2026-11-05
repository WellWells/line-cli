package line

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/ncruces/go-sqlite3"
)

// officialContactType is _contact._type of official accounts. _type alone
// misses some; a _buddyDetail row or _capableBuddy=1 also marks one.
const officialContactType = 8

const (
	DefaultContactsLimit = 200
	DefaultMembersLimit  = 200
)

// idShape matches every LINE id seen in _message._from and the name tables
// (u user, p open-chat member, v bot, ...).
var idShape = regexp.MustCompile(`^[a-z][0-9a-f]{32}$`)

// chatIDPrefix matches the start of a chat_id, like a short git commit id: at
// least 4 characters (git's minimum too), its kind letter (u, c, m, t, r) and 3
// or more hex digits, lowercase.
var chatIDPrefix = regexp.MustCompile(`^[ucmtr][0-9a-f]{3,31}$`)

// scan runs a query and calls fn per row; a missing table yields no rows.
func (r *Reader) scan(sql string, fn func(st *sqlite3.Stmt), params ...any) error {
	st, err := r.query(sql, params...)
	if err != nil {
		return err
	}
	if st == nil {
		return nil
	}
	defer st.Close()
	for st.Step() {
		fn(st)
	}
	return stepErr(st)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type chatRow struct {
	ChatSummary
	alt    string
	lastMs int64
}

// loadChats returns every chat, most recently active first. Activity is the
// later of _lastUpdatedTime and the newest stored message.
func (r *Reader) loadChats() ([]chatRow, error) {
	if err := r.buildMaps(); err != nil {
		return nil, err
	}
	var rows []chatRow
	at := map[string]int{}
	if err := r.scan("SELECT _id, _lastUpdatedTime, (SELECT max(_createdTime) FROM _message m WHERE m._chatId=c._id) FROM _chat c ORDER BY _lastUpdatedTime DESC", func(st *sqlite3.Stmt) {
		id := st.ColumnText(0)
		info := r.maps.resolveChat(id)
		last := max(st.ColumnInt64(1), st.ColumnInt64(2))
		at[id] = len(rows)
		rows = append(rows, chatRow{ChatSummary: ChatSummary{
			ChatID: id, Name: info.name, Type: info.typ, LastMessageAt: tsToIso(last),
			ParentChatID: info.parent, MemberCount: r.maps.members[id],
		}, alt: info.alt, lastMs: last})
	}); err != nil {
		return nil, err
	}
	if err := r.scan("SELECT _id, _hidden=1, _notification=0, _pinnedTime>0 FROM _chatOption", func(st *sqlite3.Stmt) {
		if i, ok := at[st.ColumnText(0)]; ok {
			rows[i].Hidden, rows[i].Muted, rows[i].Pinned = st.ColumnBool(1), st.ColumnBool(2), st.ColumnBool(3)
		}
	}); err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].lastMs > rows[j].lastMs })
	return rows, nil
}

func chatAmbiguous(arg string, hits []chatRow) error {
	return ambiguous(fmt.Sprintf("%d chats match: %s; use chat_id", len(hits), arg), hits)
}

// prefixAmbiguous is for a chat_id prefix that several chats start with, or
// that a chat name also contains: like git, never pick one silently.
func prefixAmbiguous(arg string, hits []chatRow) error {
	return ambiguous(fmt.Sprintf("%d chats match %s (the start of a chat_id or part of a name); add characters of the chat_id or pick from candidates", len(hits), arg), hits)
}

// personAmbiguous is for the full name of a person without a 1:1 chat: chats
// that merely contain it are candidates, never picked silently.
func personAmbiguous(arg string, hits []chatRow) error {
	return ambiguous(fmt.Sprintf("%s is %s; %d chats contain it in their name, use chat_id", arg, personHint, len(hits)), hits)
}

func ambiguous(msg string, hits []chatRow) error {
	if len(hits) > maxCandidates {
		msg += fmt.Sprintf(" (candidates: the %d most recently active)", maxCandidates)
		hits = hits[:maxCandidates]
	}
	cands := make([]ChatSummary, len(hits))
	for i, h := range hits {
		cands[i] = ChatSummary{ChatID: h.ChatID, Name: h.Name, Type: h.Type, LastMessageAt: h.LastMessageAt}
	}
	return &Error{Code: ErrChatAmbiguous, Message: msg, Candidates: cands}
}

const personHint = "a contact or open-chat member without a 1:1 chat; use search --from to find their messages"

// chatNotFound says so, and points to search --from when arg is a person.
func chatNotFound(arg string, person bool) error {
	msg := "no chat matches: " + arg
	if person {
		msg += " (" + personHint + ")"
	}
	return &Error{Code: ErrChatNotFound, Message: msg}
}

// isPerson reports whether arg is the id or, ignoring case, the full name of
// you, a contact or an open-chat member.
func (r *Reader) isPerson(arg string) (bool, error) {
	people, err := r.loadPeople()
	if err != nil {
		return false, err
	}
	for _, p := range people {
		if p.id == arg || (p.name != "" && strings.EqualFold(p.name, arg)) {
			return true, nil
		}
	}
	return false, nil
}

// scanOpt is scan for columns older LINE versions may lack: a missing column
// reads as no rows.
func (r *Reader) scanOpt(sql string, fn func(st *sqlite3.Stmt), params ...any) error {
	st, _, err := r.db.conn.Prepare(sql)
	if err != nil {
		if strings.Contains(err.Error(), "no such column") {
			return nil
		}
		return classifyQueryErr(err)
	}
	st.Close()
	return r.scan(sql, fn, params...)
}

// person is one name a sender id is known by.
type person struct {
	id, name, source, community string
}

// loadPeople lists you, contacts (own name and your nickname for them) and
// open-chat members, each with every name they can be found by.
func (r *Reader) loadPeople() ([]person, error) {
	if err := r.buildOwnMids(); err != nil {
		return nil, err
	}
	var out []person
	if err := r.scan("SELECT _mid, _displayName FROM _profile", func(st *sqlite3.Stmt) {
		out = append(out, person{id: st.ColumnText(0), name: st.ColumnText(1), source: "me"})
	}); err != nil {
		return nil, err
	}
	if err := r.scan("SELECT _mid, _displayName, _displayNameOverridden FROM _contact WHERE _mid<>''", func(st *sqlite3.Stmt) {
		id, name, nick := st.ColumnText(0), st.ColumnText(1), st.ColumnText(2)
		out = append(out, person{id: id, name: name, source: "contact"})
		if nick != "" && nick != name {
			out = append(out, person{id: id, name: nick, source: "contact"})
		}
	}); err != nil {
		return nil, err
	}
	communities := map[string]string{}
	if err := r.scan("SELECT _mid, _name FROM _square", func(st *sqlite3.Stmt) {
		communities[st.ColumnText(0)] = st.ColumnText(1)
	}); err != nil {
		return nil, err
	}
	if err := r.scan("SELECT _squareMemberMid, _displayName, _squareMid FROM _squareMember", func(st *sqlite3.Stmt) {
		id, sq := st.ColumnText(0), st.ColumnText(2)
		src := "open_chat_member"
		if r.ownMids[id] {
			src = "me"
		}
		out = append(out, person{id: id, name: st.ColumnText(1), source: src, community: orElse(communities[sq], sq)})
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// knownIDs takes needle as an id, or a comma-separated list of ids, when each
// is in a name table or has sent a message. ok is false when needle is not ids.
func (r *Reader) knownIDs(needle string, people []person) (ids []string, ok bool, err error) {
	known := map[string]bool{}
	for _, p := range people {
		known[p.id] = true
	}
	parts := strings.Split(needle, ",")
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if !known[id] && !r.ownMids[id] {
			if !idShape.MatchString(id) {
				return nil, false, nil
			}
			sent, err := r.hasSent(id)
			if err != nil {
				return nil, false, err
			}
			if !sent {
				if len(parts) == 1 {
					return nil, false, nil
				}
				return nil, false, &Error{Code: ErrSenderNotFound, Message: "unknown sender id: " + id}
			}
		}
		ids = append(ids, id)
	}
	return ids, true, nil
}

func (r *Reader) hasSent(id string) (bool, error) {
	found := false
	err := r.scan("SELECT 1 FROM _message WHERE _from=? LIMIT 1", func(*sqlite3.Stmt) { found = true }, id)
	return found, err
}

// SenderCandidate is one person a --from name could mean. ChatID is their 1:1
// chat with you, as in contacts; LastChatID is where they last posted.
type SenderCandidate struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Source     string `json:"source"`
	Community  string `json:"community,omitempty"`
	ChatID     string `json:"chat_id,omitempty"`
	LastChatID string `json:"last_chat_id,omitempty"`
	LastSentAt string `json:"last_sent_at,omitempty"`
}

// senderAmbiguous lists the people a name matched, most recently active first.
func (r *Reader) senderAmbiguous(needle string, hits []person) error {
	type seen struct {
		ms   int64
		chat string
	}
	act := map[string]seen{}
	params := make([]any, len(hits))
	for i, h := range hits {
		params[i] = h.id
	}
	in := "(" + placeholders(len(hits)) + ")"
	if err := r.scan("SELECT _from, max(_createdTime), _chatId FROM _message WHERE _from IN "+in+" AND "+sqlPlainKind+" GROUP BY _from", func(st *sqlite3.Stmt) {
		act[st.ColumnText(0)] = seen{st.ColumnInt64(1), st.ColumnText(2)}
	}, params...); err != nil {
		return err
	}
	ownChat := map[string]bool{}
	if err := r.scan("SELECT _id FROM _chat WHERE _id IN "+in, func(st *sqlite3.Stmt) { ownChat[st.ColumnText(0)] = true }, params...); err != nil {
		return err
	}
	rank := map[string]int{"me": 0, "contact": 1, "open_chat_member": 2}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if act[a.id].ms != act[b.id].ms {
			return act[a.id].ms > act[b.id].ms
		}
		if rank[a.source] != rank[b.source] {
			return rank[a.source] < rank[b.source]
		}
		return a.name < b.name
	})
	msg := fmt.Sprintf("%d people match --from %s; pass one id from candidates, or several ids comma-separated", len(hits), needle)
	if len(hits) > maxCandidates {
		msg += fmt.Sprintf(" (candidates: the %d most recently active)", maxCandidates)
		hits = hits[:maxCandidates]
	}
	cands := make([]SenderCandidate, len(hits))
	for i, h := range hits {
		a := act[h.id]
		cands[i] = SenderCandidate{ID: h.id, Name: h.name, Source: h.source, Community: h.community, LastChatID: a.chat, LastSentAt: tsToIso(a.ms)}
		if ownChat[h.id] {
			cands[i].ChatID = h.id
		}
	}
	return &Error{Code: ErrSenderAmbiguous, Message: msg, Senders: cands}
}

type ContactsOptions struct {
	Query          string
	Limit          int
	IncludeMembers bool
}

// ContactList is one page of people. MoreMembers counts the open-chat members
// that match but are not listed: left out without IncludeMembers (counted only
// for a query), or cut by the limit.
type ContactList struct {
	Contacts    []ContactSummary
	MoreMembers int
}

// Contacts is ListContacts for a query alone, with the largest limit.
func (r *Reader) Contacts(query string) ([]ContactSummary, error) {
	res, err := r.ListContacts(ContactsOptions{Query: query, Limit: MaxLimit})
	if err != nil {
		return nil, err
	}
	return res.Contacts, nil
}

// ListContacts lists you, then contacts with a 1:1 chat (most recently active
// first), then the other contacts; with IncludeMembers those are merged with
// open-chat members, latest sender first. A query equal to an id returns that
// person from any source; otherwise it filters by name substring.
func (r *Reader) ListContacts(opt ContactsOptions) (*ContactList, error) {
	if err := r.buildOwnMids(); err != nil {
		return nil, err
	}
	chats, err := r.loadChats()
	if err != nil {
		return nil, err
	}
	lastMs := map[string]int64{}
	for _, c := range chats {
		lastMs[c.ChatID] = c.lastMs
	}
	chatOf := func(id string) string {
		if _, ok := lastMs[id]; ok {
			return id
		}
		return ""
	}
	var me, withChat, rest, members []ContactSummary
	if err := r.scan("SELECT _mid, _displayName FROM _profile", func(st *sqlite3.Stmt) {
		id := st.ColumnText(0)
		me = append(me, ContactSummary{ContactID: id, DisplayName: orElse(st.ColumnText(1), id), IsMe: true, ChatID: chatOf(id), Source: "me"})
	}); err != nil {
		return nil, err
	}
	friend := map[string]bool{}
	if err := r.scanOpt("SELECT _mid FROM _contact WHERE _status IN (1, 2)", func(st *sqlite3.Stmt) { // friend, friend you blocked
		friend[st.ColumnText(0)] = true
	}); err != nil {
		return nil, err
	}
	if err := r.scan("SELECT _mid, _displayName, _displayNameOverridden FROM _contact WHERE _mid<>''", func(st *sqlite3.Stmt) {
		id, name, nick := st.ColumnText(0), st.ColumnText(1), st.ColumnText(2)
		c := ContactSummary{ContactID: id, DisplayName: orElse(displayName(name, nick), id), IsMe: r.ownMids[id], Nickname: nick, ChatID: chatOf(id), Type: "other", Source: "contact"}
		if name != c.DisplayName {
			c.ProfileName = name
		}
		switch {
		case r.maps.official[id]:
			c.Type = "official"
		case friend[id]:
			c.Type = "friend"
		}
		if c.ChatID != "" {
			withChat = append(withChat, c)
		} else {
			rest = append(rest, c)
		}
	}); err != nil {
		return nil, err
	}
	needle := strings.TrimSpace(opt.Query)
	if opt.IncludeMembers || needle != "" {
		communities := map[string]string{}
		if err := r.scan("SELECT _mid, _name FROM _square", func(st *sqlite3.Stmt) {
			communities[st.ColumnText(0)] = st.ColumnText(1)
		}); err != nil {
			return nil, err
		}
		if err := r.scan("SELECT _squareMemberMid, _displayName, _squareMid FROM _squareMember", func(st *sqlite3.Stmt) {
			id, sq := st.ColumnText(0), st.ColumnText(2)
			c := ContactSummary{ContactID: id, DisplayName: orElse(st.ColumnText(1), id), IsMe: r.ownMids[id], Source: "open_chat_member", Community: orElse(communities[sq], sq)}
			if c.IsMe {
				c.Source = "me"
			}
			members = append(members, c)
		}); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(withChat, func(i, j int) bool { return lastMs[withChat[i].ChatID] > lastMs[withChat[j].ChatID] })
	others, extra := rest, members
	if opt.IncludeMembers {
		sent := map[string]int64{}
		if err := r.scan("SELECT _from, max(_createdTime) FROM _message WHERE "+sqlPlainKind+" GROUP BY _from", func(st *sqlite3.Stmt) {
			sent[st.ColumnText(0)] = st.ColumnInt64(1)
		}); err != nil {
			return nil, err
		}
		others, extra = append(rest, members...), nil
		sort.SliceStable(others, func(i, j int) bool { return sent[others[i].ContactID] > sent[others[j].ContactID] })
	}
	pool := slices.Concat(me, withChat, others)

	res := &ContactList{Contacts: []ContactSummary{}}
	if needle != "" {
		for _, c := range slices.Concat(pool, extra) {
			if c.ContactID == needle {
				res.Contacts = append(res.Contacts, c)
			}
		}
		if len(res.Contacts) > 0 {
			return res, nil
		}
	}
	low := strings.ToLower(needle)
	match := func(c ContactSummary) bool {
		return low == "" || strings.Contains(strings.ToLower(c.DisplayName), low) ||
			strings.Contains(strings.ToLower(c.Nickname), low) || strings.Contains(strings.ToLower(c.ProfileName), low)
	}
	limit := saneLimit(opt.Limit, DefaultContactsLimit)
	for _, c := range pool {
		if !match(c) {
			continue
		}
		if len(res.Contacts) < limit {
			res.Contacts = append(res.Contacts, c)
		} else if c.Source == "open_chat_member" {
			res.MoreMembers++
		}
	}
	if needle != "" {
		for _, c := range extra {
			if c.Source == "open_chat_member" && match(c) {
				res.MoreMembers++
			}
		}
	}
	return res, nil
}
