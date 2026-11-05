package line

import (
	"slices"
	"sort"
	"strings"

	"github.com/ncruces/go-sqlite3"
)

// DefaultStatsLimit is how many groups Stats returns when no limit is given.
const DefaultStatsLimit = 100

// StatsGroupings lists the values Stats accepts for StatsOptions.By.
var StatsGroupings = []string{"sender", "day", "hour", "type"}

// StatsOptions selects the messages of one chat that Stats counts and how it groups them.
type StatsOptions struct {
	ChatArg       string
	SinceTs       int64
	UntilTs       int64
	By            string // sender (default) | day | hour | type
	Limit         int
	IncludeEvents bool
}

// StatsGroup is one group of a Stats result; only the fields of the grouping are set.
type StatsGroup struct {
	SenderID    string `json:"sender_id,omitempty"`
	Sender      string `json:"sender,omitempty"`
	FromMe      bool   `json:"from_me,omitempty"`
	Day         string `json:"day,omitempty"`
	Hour        *int   `json:"hour,omitempty"`
	Type        string `json:"type,omitempty"`
	Count       int64  `json:"count"`
	FirstSentAt string `json:"first_sent_at"`
	LastSentAt  string `json:"last_sent_at"`
}

// StatsResult is a chat's message counts grouped one way.
type StatsResult struct {
	ChatID        string       `json:"chat_id"`
	ChatName      string       `json:"chat_name"`
	ChatType      string       `json:"chat_type"`
	By            string       `json:"by"`
	IncludeEvents bool         `json:"include_events"`
	Total         int64        `json:"total"`
	FirstSentAt   string       `json:"first_sent_at,omitempty"`
	LastSentAt    string       `json:"last_sent_at,omitempty"`
	GroupCount    int          `json:"group_count"`
	Groups        []StatsGroup `json:"groups"`
}

// Stats counts a chat's messages with one SQL GROUP BY. Days and hours are in
// local time; hour is the hour of day (0-23) over the whole range. Senders and
// types come most frequent first, days and hours in time order. At most Limit
// groups are returned: the most frequent senders or types, or the newest days.
func (r *Reader) Stats(opt StatsOptions) (*StatsResult, error) {
	if err := r.buildMaps(); err != nil {
		return nil, err
	}
	if err := r.buildContacts(); err != nil {
		return nil, err
	}
	if err := r.buildOwnMids(); err != nil {
		return nil, err
	}
	by := orElse(opt.By, "sender")
	key, ok := map[string]string{
		"sender": "_from",
		"day":    "strftime('%Y-%m-%d', _createdTime/1000, 'unixepoch', 'localtime')",
		"hour":   "CAST(strftime('%H', _createdTime/1000, 'unixepoch', 'localtime') AS INTEGER)",
		"type":   "CASE WHEN " + sqlUnsent + " THEN -2 WHEN " + sqlEvent + " THEN -1 ELSE _contentType END",
	}[by]
	if !ok {
		return nil, &Error{Code: ErrInvalidArgument, Message: "invalid --by: " + by + " (valid: " + strings.Join(StatsGroupings, ", ") + ")"}
	}
	chatID, err := r.ResolveChat(opt.ChatArg, false)
	if err != nil {
		return nil, err
	}
	where := []string{"_chatId=?"}
	params := []any{chatID}
	if opt.SinceTs != 0 {
		where = append(where, "_createdTime>=?")
		params = append(params, opt.SinceTs*1000)
	}
	if opt.UntilTs != 0 {
		where = append(where, "_createdTime<=?")
		params = append(params, untilMs(opt.UntilTs))
	}
	if !opt.IncludeEvents {
		where = append(where, sqlPlainKind)
	}
	st, err := r.query("SELECT "+key+" AS k, count(*), min(_createdTime), max(_createdTime) FROM _message WHERE "+
		strings.Join(where, " AND ")+" GROUP BY k", params...)
	if err != nil {
		return nil, err
	}
	info := r.maps.resolveChat(chatID)
	res := &StatsResult{ChatID: chatID, ChatName: info.name, ChatType: info.typ, By: by, IncludeEvents: opt.IncludeEvents, Groups: []StatsGroup{}}
	if st == nil {
		return res, nil
	}
	defer st.Close()
	var first, last int64
	for st.Step() {
		g := StatsGroup{Count: st.ColumnInt64(1), FirstSentAt: tsToIso(st.ColumnInt64(2)), LastSentAt: tsToIso(st.ColumnInt64(3))}
		switch by {
		case "sender":
			g.SenderID = st.ColumnText(0)
			g.Sender = orElse(r.senderName(g.SenderID), "(system)")
			g.FromMe = g.SenderID != "" && r.ownMids[g.SenderID]
		case "day":
			g.Day = st.ColumnText(0)
		case "hour":
			h := st.ColumnInt(0)
			g.Hour = &h
		case "type":
			g.Type = statsTypeName(st.ColumnInt64(0))
		}
		res.Total += g.Count
		if f := st.ColumnInt64(2); first == 0 || f < first {
			first = f
		}
		last = max(last, st.ColumnInt64(3))
		res.Groups = append(res.Groups, g)
	}
	if err := stepErr(st); err != nil {
		return nil, err
	}
	res.FirstSentAt, res.LastSentAt = tsToIso(first), tsToIso(last)
	res.GroupCount = len(res.Groups)
	if by == "sender" {
		if err := r.statsSenderNames(res.Groups, where, params); err != nil {
			return nil, err
		}
	}

	gs := res.Groups
	switch by {
	case "sender", "type":
		sort.SliceStable(gs, func(i, j int) bool {
			if gs[i].Count != gs[j].Count {
				return gs[i].Count > gs[j].Count
			}
			return gs[i].SenderID+gs[i].Type < gs[j].SenderID+gs[j].Type
		})
	case "day":
		sort.Slice(gs, func(i, j int) bool { return gs[i].Day < gs[j].Day })
	case "hour":
		sort.Slice(gs, func(i, j int) bool { return *gs[i].Hour < *gs[j].Hour })
	}
	if limit := saneLimit(opt.Limit, DefaultStatsLimit); len(gs) > limit && by != "hour" {
		if by == "day" {
			gs = gs[len(gs)-limit:]
		} else {
			gs = gs[:limit]
		}
	}
	res.Groups = gs
	return res, nil
}

// statsSenderNames names the senders no name table knows as read does: with
// the name LINE stored on their latest message in the counted range.
func (r *Reader) statsSenderNames(gs []StatsGroup, where []string, params []any) error {
	unknown := map[string]int{}
	for i, g := range gs {
		if g.SenderID != "" && g.Sender == g.SenderID {
			unknown[g.SenderID] = i
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	ps := slices.Clone(params)
	for id := range unknown {
		ps = append(ps, id)
	}
	return r.scan("SELECT _from, max(_createdTime), _contentInfo, _eventInfo FROM _message WHERE "+strings.Join(where, " AND ")+
		" AND _from IN ("+placeholders(len(unknown))+") GROUP BY _from", func(st *sqlite3.Stmt) {
		g := &gs[unknown[st.ColumnText(0)]]
		g.Sender = r.displaySender(g.SenderID, st.ColumnText(2), st.ColumnText(3))
	}, ps...)
}

// statsTypeName names a type key: the message type, or event/unsent for the
// rows the shared event and unsent definitions match.
func statsTypeName(ct int64) string {
	switch ct {
	case -1:
		return "event"
	case -2:
		return "unsent"
	}
	return contentTypeName(ct)
}
