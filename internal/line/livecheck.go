package line

import "github.com/ncruces/go-sqlite3"

// OfficialChatsSQL counts 1:1 chats with an official account, independently of
// the Reader, so live tests can check chats --chat-type official against it.
const OfficialChatsSQL = `SELECT count(*) FROM _chat c WHERE EXISTS(SELECT 1 FROM _buddyDetail b WHERE b._mid=c._id)
	OR EXISTS(SELECT 1 FROM _contact k WHERE k._mid=c._id AND (k._type=8 OR k._capableBuddy=1))`

// CountSQL returns the integer a read-only count query yields.
func (r *Reader) CountSQL(sql string) (int, error) {
	n := -1
	err := r.scan(sql, func(st *sqlite3.Stmt) { n = st.ColumnInt(0) })
	if err == nil && n < 0 {
		err = &Error{Code: ErrDBUnreadable, Message: "count query returned no row"}
	}
	return n, err
}
