package line

import (
	"strings"

	"github.com/ncruces/go-sqlite3"
)

// staleSlackMs is how far LINE's last activity may run ahead of the newest
// stored message before a chat counts as not synced.
const staleSlackMs = 60_000

// Freshness compares what LINE knows about a chat (_chat._lastMessage and
// _lastUpdatedTime) with the messages stored on this PC. Times are unix ms.
type Freshness struct {
	ChatID       string
	LastActivity int64
	LastMissing  bool // _lastMessage.id is not in _message
	NewestStored int64
	OldestStored int64
	OldestID     string
}

// Stale reports that LINE knows of activity the local database does not hold.
func (f *Freshness) Stale() bool {
	return f.LastMissing || f.LastActivity > f.NewestStored+staleSlackMs
}

// behind reports that LINE knows of activity later than the newest stored
// message (a last message that is merely not stored does not count).
func (f *Freshness) behind() bool { return f.LastActivity > f.NewestStored+staleSlackMs }

// StaleFor reports whether a range ending at untilTs (unix seconds, 0 = now)
// reaches past the newest stored message into the chat's unsynced activity.
// With nothing stored, only a range reaching LINE's last activity does.
func (f *Freshness) StaleFor(untilTs int64) bool {
	if !f.Stale() || untilTs == 0 {
		return f.Stale()
	}
	if f.NewestStored == 0 {
		return untilMs(untilTs) >= f.LastActivity
	}
	return untilMs(untilTs) > f.NewestStored+staleSlackMs
}

// TimeISO formats unix ms (or seconds) like sent_at; "" for 0.
func TimeISO(ms int64) string { return tsToIso(ms) }

const chatStateSQL = `SELECT _id, max(coalesce(_lastUpdatedTime,0), lct),
 lid<>'' AND NOT EXISTS(SELECT 1 FROM _message m WHERE m._id=x.lid),
 coalesce((SELECT max(_createdTime) FROM _message m WHERE m._chatId=x._id),0),
 coalesce((SELECT min(_createdTime) FROM _message m WHERE m._chatId=x._id),0)
FROM (SELECT _id, _lastUpdatedTime,
 CASE WHEN json_valid(_lastMessage) THEN coalesce(json_extract(_lastMessage,'$.id'),'') ELSE '' END AS lid,
 CASE WHEN json_valid(_lastMessage) THEN coalesce(json_extract(_lastMessage,'$.createdTime'),0) ELSE 0 END AS lct
 FROM _chat WHERE ? IN ('', _id)) x
ORDER BY 2 DESC`

// chatStates returns the freshness of one chat, or of every chat (newest
// activity first) when chatID is "".
func (r *Reader) chatStates(chatID string) ([]Freshness, error) {
	st, err := r.query(chatStateSQL, chatID)
	if err != nil || st == nil {
		return nil, err
	}
	defer st.Close()
	var out []Freshness
	for st.Step() {
		out = append(out, Freshness{
			ChatID:       st.ColumnText(0),
			LastActivity: st.ColumnInt64(1),
			LastMissing:  st.ColumnInt64(2) != 0,
			NewestStored: st.ColumnInt64(3),
			OldestStored: st.ColumnInt64(4),
		})
	}
	return out, stepErr(st)
}

func (r *Reader) freshness(chatID string) (*Freshness, error) {
	fs, err := r.chatStates(chatID)
	if err != nil || len(fs) == 0 {
		return nil, err
	}
	return &fs[0], nil
}

// ChatFreshness resolves a chat like read does and reports its freshness and
// the span of its stored history. It returns nil when the chat has no state.
func (r *Reader) ChatFreshness(chatArg string) (*Freshness, error) {
	chatID, err := r.ResolveChat(chatArg, false)
	if err != nil {
		return nil, err
	}
	f, err := r.freshness(chatID)
	if err != nil || f == nil || f.OldestStored == 0 {
		return f, err
	}
	st, err := r.query("SELECT _id FROM _message WHERE _chatId=? ORDER BY _createdTime ASC LIMIT 1", chatID)
	if err != nil || st == nil {
		return f, err
	}
	defer st.Close()
	if st.Step() {
		f.OldestID = st.ColumnText(0)
	}
	return f, stepErr(st)
}

type chatOption struct {
	hidden, muted bool
}

// chatOptions reads _chatOption: _hidden=1 hides a chat from LINE's list and
// _notification=0 mutes it. A missing table yields no options.
func (r *Reader) chatOptions() (map[string]chatOption, error) {
	out := map[string]chatOption{}
	st, err := r.query("SELECT _id, _hidden, _notification FROM _chatOption")
	if err != nil || st == nil {
		return out, err
	}
	defer st.Close()
	for st.Step() {
		out[st.ColumnText(0)] = chatOption{
			hidden: st.ColumnInt64(1) == 1,
			muted:  st.ColumnType(2) != sqlite3.NULL && st.ColumnInt64(2) == 0,
		}
	}
	return out, stepErr(st)
}

func isOpenChatID(id string) bool { return strings.HasPrefix(id, "m") }

// IsOpenChat reports an open chat id (m...).
func IsOpenChat(id string) bool { return isOpenChatID(id) }

// IsThreadChat reports a thread chat id. LINE keeps thread replies on the
// server, so such a chat always looks stale and opening it does not help.
func IsThreadChat(id string) bool { return strings.HasPrefix(id, "t") }
