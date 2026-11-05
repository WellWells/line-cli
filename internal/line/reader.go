package line

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/ncruces/go-sqlite3"
)

type Reader struct {
	db       *DB
	maps     *nameMaps
	contacts map[string]string
	ownMids  map[string]bool
	roots    map[string]threadRoot // root message id -> its thread
	srv      *server               // LINE's server, when read may use it (UseServer)
}

// threadRoot is the thread an open-chat message starts and LINE's count of
// its replies.
type threadRoot struct {
	thread  string
	replies int64
}

func NewReader(db *DB) *Reader { return &Reader{db: db} }

func (r *Reader) query(sql string, params ...any) (*sqlite3.Stmt, error) {
	st, _, err := r.db.conn.Prepare(sql)
	if err != nil {
		return nil, classifyQueryErr(err)
	}
	for i, p := range params {
		var be error
		switch v := p.(type) {
		case string:
			be = st.BindText(i+1, v)
		case int64:
			be = st.BindInt64(i+1, v)
		case int:
			be = st.BindInt(i+1, v)
		default:
			be = st.BindNull(i + 1)
		}
		if be != nil {
			st.Close()
			return nil, classifyQueryErr(be)
		}
	}
	return st, nil
}

// optionalTables may be absent (or differ) in some LINE versions; a query on a
// missing one reads as empty. A missing core table or column (_message, _chat,
// _contact, _profile, or anything not listed here) means the schema changed, and
// that must fail loudly instead of looking like "no data".
var optionalTables = map[string]bool{
	"_squarechat": true, "_squaremember": true, "_square": true, "_squarethread": true,
	"_room": true, "_groupchat": true, "_buddydetail": true, "_chatoption": true,
}

func classifyQueryErr(err error) error {
	detail := ""
	if i := strings.Index(err.Error(), "no such table: "); i >= 0 {
		detail = err.Error()[i:]
	} else if i := strings.Index(err.Error(), "no such column: "); i >= 0 {
		detail = err.Error()[i:]
	}
	if detail != "" {
		table := ""
		if t, ok := strings.CutPrefix(detail, "no such table: "); ok {
			table = strings.ToLower(t)
		} else if se := (*sqlite3.Error)(nil); errors.As(err, &se) {
			// "no such column" names no table; the SQL from the bad column on does.
			fields := strings.Fields(strings.ToLower(se.SQL()))
			for j := 0; j+1 < len(fields); j++ {
				if fields[j] == "from" {
					table = fields[j+1]
					break
				}
			}
		}
		table = strings.TrimPrefix(strings.Trim(table, "\"`[];,() "), "main.")
		if optionalTables[table] {
			return nil
		}
		return &Error{Code: ErrDBUnreadable, Message: "The LINE database schema changed (" + detail + "); this line-cli version cannot read it.", schema: true}
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "locked") || strings.Contains(msg, "busy") {
		return &Error{Code: ErrDBLocked, Message: "The LINE database is locked."}
	}
	return &Error{Code: ErrDBUnreadable, Message: "The LINE database could not be read: " + err.Error()}
}

func stepErr(st *sqlite3.Stmt) error {
	if err := st.Err(); err != nil {
		if ce := classifyQueryErr(err); ce != nil {
			return ce
		}
	}
	return nil
}

func colIndex(st *sqlite3.Stmt) map[string]int {
	idx := map[string]int{}
	for i := 0; i < st.ColumnCount(); i++ {
		idx[st.ColumnName(i)] = i
	}
	return idx
}

func (r *Reader) buildMaps() error {
	if r.maps != nil {
		return nil
	}
	m := &nameMaps{
		group:    map[string]string{},
		square:   map[string]string{},
		contact:  map[string]string{},
		ownName:  map[string]string{},
		official: map[string]bool{},
		room:     map[string]bool{},
		thread:   map[string]string{},
		midType:  map[string]int64{},
		members:  map[string]int{},
	}
	if err := r.scan(`SELECT _chatMid, _chatName, CASE WHEN json_valid(_extra) THEN (SELECT count(*) FROM json_each(_extra, '$.groupExtra.memberMids')) END FROM _groupChat`, func(st *sqlite3.Stmt) {
		m.group[st.ColumnText(0)] = st.ColumnText(1)
		m.members[st.ColumnText(0)] = st.ColumnInt(2)
	}); err != nil {
		return err
	}
	if err := r.scan("SELECT _squareChatMid, _name FROM _squareChat", func(st *sqlite3.Stmt) {
		m.square[st.ColumnText(0)] = st.ColumnText(1)
	}); err != nil {
		return err
	}
	if err := r.scan("SELECT _threadMid, _chatMid FROM _squareThread", func(st *sqlite3.Stmt) {
		m.thread[st.ColumnText(0)] = st.ColumnText(1)
	}); err != nil {
		return err
	}
	if err := r.scan("SELECT _mid, _displayName, _displayNameOverridden, _type FROM _contact WHERE _mid<>''", func(st *sqlite3.Stmt) {
		mid, name, nick := st.ColumnText(0), st.ColumnText(1), st.ColumnText(2)
		m.contact[mid] = displayName(name, nick)
		if nick != "" && name != "" && name != nick {
			m.ownName[mid] = name
		}
		if st.ColumnInt64(3) == officialContactType {
			m.official[mid] = true
		}
	}); err != nil {
		return err
	}
	official := func(st *sqlite3.Stmt) { m.official[st.ColumnText(0)] = true }
	if err := r.scanOpt("SELECT _mid FROM _contact WHERE _capableBuddy=1", official); err != nil {
		return err
	}
	if err := r.scan("SELECT _mid FROM _buddyDetail", official); err != nil {
		return err
	}
	if err := r.scan("SELECT _mid FROM _room", func(st *sqlite3.Stmt) {
		m.room[st.ColumnText(0)] = true
	}); err != nil {
		return err
	}
	if err := r.scanOpt("SELECT _id, _midType FROM _chat WHERE _midType IS NOT NULL", func(st *sqlite3.Stmt) {
		m.midType[st.ColumnText(0)] = st.ColumnInt64(1)
	}); err != nil {
		return err
	}
	if err := r.scan("SELECT _mid, _displayName FROM _profile LIMIT 1", func(st *sqlite3.Stmt) {
		m.self, m.selfName = st.ColumnText(0), st.ColumnText(1)
	}); err != nil {
		return err
	}
	r.maps = m
	return nil
}

func (r *Reader) buildContacts() error {
	if r.contacts != nil {
		return nil
	}
	c := map[string]string{}
	scan := func(sql string, fn func(st *sqlite3.Stmt)) error {
		st, err := r.query(sql)
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
	if err := scan("SELECT _mid, _displayName, _displayNameOverridden FROM _contact WHERE _mid<>''", func(st *sqlite3.Stmt) {
		c[st.ColumnText(0)] = displayName(st.ColumnText(1), st.ColumnText(2))
	}); err != nil {
		return err
	}
	if err := scan("SELECT _squareMemberMid, _displayName FROM _squareMember", func(st *sqlite3.Stmt) {
		if _, ok := c[st.ColumnText(0)]; !ok {
			c[st.ColumnText(0)] = st.ColumnText(1)
		}
	}); err != nil {
		return err
	}
	if err := scan("SELECT _mid, _displayName FROM _profile", func(st *sqlite3.Stmt) {
		if _, ok := c[st.ColumnText(0)]; !ok {
			c[st.ColumnText(0)] = st.ColumnText(1)
		}
	}); err != nil {
		return err
	}
	roots := map[string]threadRoot{}
	if err := r.scanOpt("SELECT _messageId, _threadMid, _totalMessageCount FROM _squareThread WHERE _messageId<>''", func(st *sqlite3.Stmt) {
		roots[st.ColumnText(0)] = threadRoot{thread: st.ColumnText(1), replies: st.ColumnInt64(2)}
	}); err != nil {
		return err
	}
	r.contacts, r.roots = c, roots
	return nil
}

func (r *Reader) buildOwnMids() error {
	if r.ownMids != nil {
		return nil
	}
	own := map[string]bool{}
	scan := func(sql string, fn func(st *sqlite3.Stmt)) error {
		st, err := r.query(sql)
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
	if err := scan("SELECT _mid FROM _profile", func(st *sqlite3.Stmt) {
		own[st.ColumnText(0)] = true
	}); err != nil {
		return err
	}
	if err := scan("SELECT _myMemberId FROM _square", func(st *sqlite3.Stmt) {
		if v := st.ColumnText(0); v != "" {
			own[v] = true
		}
	}); err != nil {
		return err
	}
	r.ownMids = own
	return nil
}

func (r *Reader) senderName(mid string) string {
	if n, ok := r.contacts[mid]; ok && n != "" {
		return n
	}
	return mid
}

// displaySender is the sender name read shows for a row: a contact or member
// name, else the name LINE stored with the row (see storedName), else the raw id.
func (r *Reader) displaySender(mid, contentInfo, eventJSON string) string {
	if n := r.senderName(mid); n != mid {
		return n
	}
	var ev eventInfo
	if eventJSON != "" {
		_ = json.Unmarshal([]byte(eventJSON), &ev)
	}
	return orElse(storedName(mid, parseMeta(contentInfo), ev), mid)
}

// senderMids maps a --from value to sender ids: "me" is every id you post
// under, a known id (or a comma-separated list of them) is taken as-is, and a
// name goes through the tiers exact, case-insensitive, then substring; a tier
// that names more than one person is SENDER_AMBIGUOUS.
func (r *Reader) senderMids(nameOrMid string) ([]string, error) {
	needle := strings.TrimSpace(nameOrMid)
	if needle == "" {
		return nil, &Error{Code: ErrInvalidArgument, Message: "--from is empty"}
	}
	if err := r.buildOwnMids(); err != nil {
		return nil, err
	}
	if needle == "me" {
		return sortedKeys(r.ownMids), nil
	}
	people, err := r.loadPeople()
	if err != nil {
		return nil, err
	}
	if ids, ok, err := r.knownIDs(needle, people); ok || err != nil {
		return ids, err
	}
	low := strings.ToLower(needle)
	tiers := []func(string) bool{
		func(n string) bool { return n == needle },
		func(n string) bool { return strings.EqualFold(n, needle) },
		func(n string) bool { return strings.Contains(strings.ToLower(n), low) },
	}
	for _, match := range tiers {
		var hits []person
		seen := map[string]bool{}
		allMe := true
		for _, p := range people {
			if p.name != "" && match(p.name) && !seen[p.id] {
				seen[p.id] = true
				hits = append(hits, p)
				allMe = allMe && r.ownMids[p.id]
			}
		}
		switch {
		case len(hits) == 0:
			continue
		case allMe:
			return sortedKeys(r.ownMids), nil
		case len(hits) == 1:
			return []string{hits[0].id}, nil
		}
		return nil, r.senderAmbiguous(needle, hits)
	}
	return nil, &Error{Code: ErrSenderNotFound, Message: "no contact, open-chat member or sender id matches: " + needle}
}

func (r *Reader) readMessageRow(st *sqlite3.Stmt, idx map[string]int) Message {
	text := func(col string) string {
		if i, ok := idx[col]; ok {
			return st.ColumnText(i)
		}
		return ""
	}
	num := func(col string, null int64) int64 {
		if i, ok := idx[col]; ok && st.ColumnType(i) != sqlite3.NULL {
			return st.ColumnInt64(i)
		}
		return null
	}
	raw := rawMessage{
		From: text("_from"), Text: text("_text"), Meta: text("_contentMetadata"), Info: text("_contentInfo"),
		EventInfo: text("_eventInfo"), Location: text("_location"), URLPreview: text("_urlPreview"),
		Reactions: text("_reactions"), ReactionStatus: text("_reactionStatus"), RelatedID: text("_relatedMessageId"),
		Created: num("_createdTime", 0), ContentType: num("_contentType", 0), Type: num("_type", 0),
		Attribute: num("_attribute", 0), RelationType: num("_messageRelationType", -1),
	}
	if i, ok := idx["_chunks"]; ok && raw.Text == "" && raw.ContentType == 0 {
		raw.HasChunks = len(st.ColumnRawText(i)) > 0
	}
	fromMe := raw.From != "" && r.ownMids[raw.From]
	m := parseMessage(raw, r.senderName(raw.From), fromMe, r.knownName)
	m.ID = text("_id")
	r.markThreadRoot(&m)
	return m
}

// markThreadRoot adds thread_id and thread_replies to a message that starts
// an open-chat thread.
func (r *Reader) markThreadRoot(m *Message) {
	if t, ok := r.roots[m.ID]; ok && m.ID != "" {
		m.ThreadID, m.ThreadReplies = t.thread, t.replies
	}
}

// knownName is a mid's display name from the name tables, or "".
func (r *Reader) knownName(mid string) string {
	if n := r.senderName(mid); n != mid {
		return n
	}
	return ""
}

type ChatsOptions struct {
	Query    string
	ChatType string
	Limit    int
}

func (r *Reader) ListChats(opt ChatsOptions) ([]ChatSummary, error) {
	rows, err := r.loadChats()
	if err != nil {
		return nil, err
	}
	limit := saneLimit(opt.Limit, DefaultChatsLimit)
	q := strings.ToLower(strings.TrimSpace(opt.Query))
	idPrefix := chatIDPrefix.MatchString(q)
	out := []ChatSummary{}
	for _, c := range rows {
		if len(out) >= limit {
			break
		}
		if opt.ChatType != "" && c.Type != opt.ChatType {
			continue
		}
		if q != "" && c.ChatID != opt.Query && !(idPrefix && strings.HasPrefix(c.ChatID, q)) &&
			!strings.Contains(strings.ToLower(c.Name), q) && !strings.Contains(strings.ToLower(c.alt), q) {
			continue
		}
		out = append(out, c.ChatSummary)
	}
	return out, nil
}

const maxCandidates = 20

func (r *Reader) ResolveChat(arg string, exactOnly bool) (string, error) {
	c, err := r.ResolveChatInfo(arg, exactOnly)
	return c.ChatID, err
}

// ResolveChatInfo finds one chat by exact chat_id, then by name: exact, then
// case-insensitive, then a chat_id prefix (chatIDPrefix, like a short git
// commit id; ambiguous with any chat whose name contains it), then a name
// substring (exactOnly stops after exact, so send never takes a prefix). A name
// is the chat name or, for a contact you gave a nickname, their own name too. Threads
// and your own chat count only when no other chat matches, except with
// exactOnly (send), where they compete like any other chat. Several hits in the
// deciding tier give CHAT_AMBIGUOUS, most recently active first; so does any
// substring hit for the full name of a person without a 1:1 chat.
func (r *Reader) ResolveChatInfo(arg string, exactOnly bool) (ChatSummary, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return ChatSummary{}, &Error{Code: ErrInvalidArgument, Message: "chat is empty"}
	}
	rows, err := r.loadChats()
	if err != nil {
		return ChatSummary{}, err
	}
	for _, c := range rows {
		if c.ChatID == arg {
			return c.ChatSummary, nil
		}
	}
	low := strings.ToLower(arg)
	tiers := []func(string) bool{
		func(n string) bool { return n == arg },
		func(n string) bool { return strings.EqualFold(n, arg) },
		func(n string) bool { return strings.Contains(strings.ToLower(n), low) },
	}
	if exactOnly {
		tiers = tiers[:1]
	}
	for i, match := range tiers {
		if i == 2 && chatIDPrefix.MatchString(low) {
			// Like a short git commit id: a prefix that only one chat_id starts
			// with picks that chat. A chat whose name contains the same text
			// competes too, so "cafe" never silently beats a chat named Cafe.
			var hits []chatRow
			prefixed := false
			for _, c := range rows {
				p := strings.HasPrefix(c.ChatID, low)
				if p || (c.Name != c.ChatID && match(c.Name)) || (c.alt != "" && match(c.alt)) {
					hits = append(hits, c)
				}
				prefixed = prefixed || p
			}
			if prefixed {
				if len(hits) == 1 {
					return hits[0].ChatSummary, nil
				}
				return ChatSummary{}, prefixAmbiguous(arg, hits)
			}
		}
		var hits, fallback []chatRow
		for _, c := range rows {
			if (c.Name != c.ChatID && match(c.Name)) || (c.alt != "" && match(c.alt)) {
				if !exactOnly && (c.Type == "thread" || c.Type == "self") {
					fallback = append(fallback, c)
				} else {
					hits = append(hits, c)
				}
			}
		}
		if len(hits) == 0 {
			hits = fallback
		}
		if i == 2 && len(hits) > 0 {
			person, err := r.isPerson(arg)
			if err != nil {
				return ChatSummary{}, err
			}
			if person {
				return ChatSummary{}, personAmbiguous(arg, hits)
			}
		}
		switch len(hits) {
		case 0:
			continue
		case 1:
			return hits[0].ChatSummary, nil
		}
		return ChatSummary{}, chatAmbiguous(arg, hits)
	}
	person, err := r.isPerson(arg)
	if err != nil {
		return ChatSummary{}, err
	}
	return ChatSummary{}, chatNotFound(arg, person)
}

type ReadOptions struct {
	ChatArg string
	SinceTs int64
	UntilTs int64
	Limit   int
	Around  string // message id; reads the messages around it instead of a range
}

// ReadResult is read's output for a range of one chat.
type ReadResult struct {
	Messages []Message
	// Counted is what --limit counts: an open chat's main-chat messages, every
	// message of other chats.
	Counted []Message
	// Complete reports that LINE's server filled in what this PC lacks in the
	// span read, so notes about activity that is not stored do not apply.
	Complete bool
	// ThreadsNotRead counts an open chat's threads with replies that may fall in
	// the span read, when it was read from this PC alone; RepliesNotRead is
	// LINE's count of their replies, which this PC does not store.
	ThreadsNotRead, RepliesNotRead int64
}

// EffectiveLimit is the message count ReadHistory returns at most.
func (o ReadOptions) EffectiveLimit() int {
	if o.Around != "" {
		return saneLimit(o.Limit, DefaultAroundLimit)
	}
	if o.SinceTs != 0 || o.UntilTs != 0 {
		return saneLimit(o.Limit, DefaultReadRangeLimit)
	}
	return saneLimit(o.Limit, DefaultReadLimit)
}

func (r *Reader) ReadHistory(opt ReadOptions) ([]Message, error) {
	if err := r.buildContacts(); err != nil {
		return nil, err
	}
	if err := r.buildOwnMids(); err != nil {
		return nil, err
	}
	if opt.Around != "" {
		return r.readAround(opt)
	}
	out, _, err := r.readRange(opt)
	return out, err
}

// readRange is ReadHistory for a range, with each message's _createdTime (ms).
func (r *Reader) readRange(opt ReadOptions) ([]Message, []int64, error) {
	chatID, err := r.ResolveChat(opt.ChatArg, false)
	if err != nil {
		return nil, nil, err
	}
	// Anchor on --since when given (first N from it), otherwise on the newest end
	// (last N up to --until or now); output is always chronological.
	where := "_chatId=?"
	params := []any{chatID}
	if opt.SinceTs != 0 {
		where += " AND _createdTime>=?"
		params = append(params, opt.SinceTs*1000)
	}
	if opt.UntilTs != 0 {
		where += " AND _createdTime<=?"
		params = append(params, untilMs(opt.UntilTs))
	}
	newest := opt.SinceTs == 0
	order := "ASC"
	if newest {
		order = "DESC"
	}
	st, err := r.query("SELECT * FROM _message WHERE "+where+" ORDER BY _createdTime "+order+" LIMIT ?",
		append(params, opt.EffectiveLimit())...)
	if err != nil {
		return nil, nil, err
	}
	if st == nil {
		return nil, nil, nil
	}
	defer st.Close()
	idx := colIndex(st)
	var out []Message
	var created []int64
	for st.Step() {
		out = append(out, r.readMessageRow(st, idx))
		created = append(created, st.ColumnInt64(idx["_createdTime"]))
	}
	if newest {
		reverse(out)
		reverse(created)
	}
	return out, created, stepErr(st)
}

// readStored reads a range of a chat from this PC alone. For an open chat it
// also counts what that leaves out: the threads with replies that may fall in
// the span read, on the page --limit cut.
func (r *Reader) readStored(chat ChatSummary, opt ReadOptions) (ReadResult, error) {
	if err := r.buildContacts(); err != nil {
		return ReadResult{}, err
	}
	if err := r.buildOwnMids(); err != nil {
		return ReadResult{}, err
	}
	msgs, created, err := r.readRange(opt)
	res := ReadResult{Messages: msgs, Counted: msgs}
	if err != nil || chat.Type != "open" {
		return res, err
	}
	lo, hi := rangeMs(opt.SinceTs, opt.UntilTs)
	if n := len(created); n > 0 && n == opt.EffectiveLimit() {
		lo, hi = pageSpan(lo, hi, created[0], created[n-1], opt.SinceTs != 0)
	}
	threads, err := r.threadsInSpan(chat.ChatID, lo, hi)
	for _, t := range threads {
		if t.replies > 0 {
			res.ThreadsNotRead++
			res.RepliesNotRead += t.replies
		}
	}
	return res, err
}

// pageSpan narrows a span [lo, hi] to the page --limit cut, whose first and last
// messages were created at first and last (ms): a page from --since ends at its
// last message's second, one from the newest end starts at its first's.
func pageSpan(lo, hi, first, last int64, fromSince bool) (int64, int64) {
	if fromSince {
		return lo, last/1000*1000 + 999
	}
	return first / 1000 * 1000, hi
}

// threadMeta is a thread's row in _squareThread.
type threadMeta struct {
	id, chat, rootID string
	lastAt, replies  int64
	rootAt           int64 // the root message's _createdTime, 0 when not stored
}

// threadsInSpan lists an open chat's threads with replies that may fall in
// [lo, hi] (0 = open), most recently active first.
func (r *Reader) threadsInSpan(chatID string, lo, hi int64) ([]threadMeta, error) {
	var out []threadMeta
	err := r.scanOpt(`SELECT t._threadMid, t._messageId, coalesce(t._lastMessageAt,0), coalesce(t._totalMessageCount,0), coalesce(m._createdTime,0)
		FROM _squareThread t LEFT JOIN _message m ON m._id=t._messageId
		WHERE t._chatMid=? AND (t._lastMessageAt IS NULL OR t._lastMessageAt>=?) AND (?=0 OR m._createdTime IS NULL OR m._createdTime<=?)
		ORDER BY t._lastMessageAt DESC, t._threadMid`, func(st *sqlite3.Stmt) {
		out = append(out, threadMeta{id: st.ColumnText(0), chat: chatID, rootID: st.ColumnText(1), lastAt: st.ColumnInt64(2), replies: st.ColumnInt64(3), rootAt: st.ColumnInt64(4)})
	}, chatID, lo, hi, hi)
	return out, err
}

// rangeMs turns --since/--until (unix seconds, 0 = open) into inclusive ms.
func rangeMs(sinceTs, untilTs int64) (lo, hi int64) {
	if sinceTs != 0 {
		lo = sinceTs * 1000
	}
	if untilTs != 0 {
		hi = untilMs(untilTs)
	}
	return lo, hi
}

func reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

type SearchOptions struct {
	Query       string
	ChatID      string
	ChatType    string
	From        string
	ContentType string
	SinceTs     int64
	UntilTs     int64
	Limit       int
	Order       string
}

func (r *Reader) chatIdsOfType(chatType string) ([]string, error) {
	rows, err := r.loadChats()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, c := range rows {
		if c.Type == chatType {
			out = append(out, c.ChatID)
		}
	}
	return out, nil
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func (r *Reader) Search(opt SearchOptions) ([]SearchHit, error) {
	if err := r.buildMaps(); err != nil {
		return nil, err
	}
	if err := r.buildContacts(); err != nil {
		return nil, err
	}
	if err := r.buildOwnMids(); err != nil {
		return nil, err
	}
	var where []string
	var params []any
	if q := strings.TrimSpace(opt.Query); q != "" {
		terms := searchTerms(q)
		if len(terms) == 0 {
			return nil, &Error{Code: ErrInvalidArgument, Message: "search text has no words: " + q}
		}
		if opt.ContentType == "unsent" {
			return nil, &Error{Code: ErrInvalidArgument, Message: "unsent messages have no text to search; use --type unsent without text"}
		}
		if err := registerMatch(r.db.conn); err != nil {
			return nil, &Error{Code: ErrDBUnreadable, Message: "cannot register search function: " + err.Error()}
		}
		where = append(where, "linecli_match("+visibleSQL+", ?)")
		params = append(params, strings.Join(terms, "\x1f"))
	}
	if opt.ContentType != "" {
		clause, args, ok := typeFilter(opt.ContentType)
		if !ok {
			return nil, &Error{Code: ErrInvalidArgument, Message: "invalid --type: " + opt.ContentType + " (valid: " + strings.Join(SearchTypeNames(), ", ") + ")"}
		}
		where = append(where, clause)
		params = append(params, args...)
	}
	if opt.From != "" {
		mids, err := r.senderMids(opt.From)
		if err != nil {
			return nil, err
		}
		where = append(where, "_from IN ("+placeholders(len(mids))+")")
		for _, m := range mids {
			params = append(params, m)
		}
	}
	if opt.ChatID != "" {
		chatID, err := r.ResolveChat(opt.ChatID, false)
		if err != nil {
			return nil, err
		}
		where = append(where, "_chatId=?")
		params = append(params, chatID)
	} else if opt.ChatType != "" {
		ids, err := r.chatIdsOfType(opt.ChatType)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, nil
		}
		where = append(where, "_chatId IN ("+placeholders(len(ids))+")")
		for _, id := range ids {
			params = append(params, id)
		}
	}
	if opt.SinceTs != 0 {
		where = append(where, "_createdTime>=?")
		params = append(params, opt.SinceTs*1000)
	}
	if opt.UntilTs != 0 {
		where = append(where, "_createdTime<=?")
		params = append(params, untilMs(opt.UntilTs))
	}
	order := "DESC"
	if strings.EqualFold(opt.Order, "asc") {
		order = "ASC"
	}
	sql := "SELECT * FROM _message"
	if len(where) > 0 {
		sql += " WHERE " + strings.Join(where, " AND ")
	}
	sql += " ORDER BY _createdTime " + order + " LIMIT ?"
	params = append(params, saneLimit(opt.Limit, DefaultSearchLimit))

	st, err := r.query(sql, params...)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, nil
	}
	defer st.Close()
	idx := colIndex(st)
	cidIdx := idx["_chatId"]
	var out []SearchHit
	for st.Step() {
		chatID := st.ColumnText(cidIdx)
		info := r.maps.resolveChat(chatID)
		out = append(out, SearchHit{
			ChatID: chatID, ChatName: info.name, ChatType: info.typ,
			Message: r.readMessageRow(st, idx),
		})
	}
	return out, stepErr(st)
}

type UnreadOptions struct {
	LimitChats      int
	IncludeOfficial bool
	IncludeHidden   bool
	PerChatLimit    int
	LineStopped     bool // LINE is not running, so nothing counts as fully synced
}

// Unread boundaries say where a chat's unread messages start: LINE's first
// unread message (groups, 1:1), or after the read marker (open chats, whose
// _firstUnreadId is not a message id, and chats whose first unread message is
// not stored). A marker covering more than _unreadCount messages lags behind,
// so only the newest _unreadCount after it are taken (unread_count).
const (
	BoundaryFirstUnread = "first_unread"
	BoundaryReadMarker  = "read_marker"
	BoundaryUnreadCount = "unread_count"
	BoundaryNotStored   = "not_stored"
	BoundaryNone        = "none"
)

func (r *Reader) Unread(opt UnreadOptions) ([]UnreadChat, error) {
	if err := r.buildMaps(); err != nil {
		return nil, err
	}
	if err := r.buildContacts(); err != nil {
		return nil, err
	}
	if err := r.buildOwnMids(); err != nil {
		return nil, err
	}
	st, err := r.query("SELECT _id, _unreadCount FROM _chat WHERE _unreadCount>0 ORDER BY _lastUpdatedTime DESC")
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, nil
	}
	type chatRow struct {
		id     string
		unread int64
	}
	var rows []chatRow
	for st.Step() {
		rows = append(rows, chatRow{st.ColumnText(0), st.ColumnInt64(1)})
	}
	err = stepErr(st)
	st.Close()
	if err != nil {
		return nil, err
	}
	opts, err := r.chatOptions()
	if err != nil {
		return nil, err
	}

	limitChats := saneLimit(opt.LimitChats, DefaultUnreadChats)
	perChat := saneLimit(opt.PerChatLimit, DefaultUnreadPerChat)
	var out []UnreadChat
	for _, cr := range rows {
		if len(out) >= limitChats {
			break
		}
		info := r.maps.resolveChat(cr.id)
		if info.typ == "official" && !opt.IncludeOfficial {
			continue
		}
		o := opts[cr.id]
		if o.hidden && !opt.IncludeHidden {
			continue
		}
		u := UnreadChat{
			ChatID: cr.id, Name: info.name, Type: info.typ, UnreadCount: cr.unread,
			Muted: o.muted, Hidden: o.hidden, Boundary: BoundaryNone, Messages: []Message{},
		}
		if err := r.fillUnread(&u, perChat, opt.LineStopped); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

// fillUnread sets the boundary, counts and messages of one unread chat.
// Event and unsent rows, which LINE does not count as unread, are returned but
// neither counted nor charged to perChat.
func (r *Reader) fillUnread(u *UnreadChat, perChat int, lineStopped bool) error {
	f, err := r.freshness(u.ChatID)
	if err != nil {
		return err
	}
	if f != nil {
		u.Stale = f.Stale()
		u.LastActivityAt, u.NewestStoredAt = tsToIso(f.LastActivity), tsToIso(f.NewestStored)
	}
	where, params, err := r.unreadBoundary(u)
	if err != nil {
		return err
	}
	// Online, an open chat's messages after the newest stored one come from
	// LINE's server; all are newer than any stored message, so unread.
	var gap []Message
	var gapPlain []bool
	var gapCount int64
	if r.online() && f != nil && f.behind() && u.Boundary == BoundaryReadMarker {
		g, gp, gc, complete, err := r.unreadGap(u.ChatID, f)
		if err != nil {
			return err
		}
		gap, gapPlain, gapCount = g, gp, gc
		if complete {
			u.Stale = false
		}
	}
	want := u.UnreadCount // non-event messages to return
	switch u.Boundary {
	case BoundaryNotStored:
		want = 0
	case BoundaryFirstUnread, BoundaryReadMarker:
		n, err := r.count("SELECT count(*) FROM _message WHERE "+where+" AND "+sqlPlainKind, params...)
		if err != nil {
			return err
		}
		n += gapCount
		if n > u.UnreadCount {
			u.Boundary, n = BoundaryUnreadCount, u.UnreadCount
		}
		u.AvailableCount, want = n, n
	}
	u.MissingCount = max(0, u.UnreadCount-u.AvailableCount)
	exact := u.Boundary == BoundaryFirstUnread || u.Boundary == BoundaryReadMarker
	u.FullySynced = exact && u.MissingCount == 0 && !u.Stale && !lineStopped
	quota := min(want, int64(perChat))
	whole := exact && quota == want // the whole range fits, events before the first unread one included
	if quota == 0 && !whole {
		return nil
	}
	// Newest first: the messages from LINE's server, then the stored rows.
	var taken int64
	for i := len(gap) - 1; i >= 0; i-- {
		stop := len(u.Messages) >= MaxLimit
		if gapPlain[i] {
			stop = stop || taken == quota
			if !stop {
				taken++
			}
		} else {
			stop = stop || (taken == quota && !whole)
		}
		if stop {
			u.MoreUnread = taken < want
			reverse(u.Messages)
			return nil
		}
		u.Messages = append(u.Messages, gap[i])
	}

	ms, err := r.query("SELECT *, "+sqlEventOrUnsent+" AS linecli_event FROM _message WHERE "+where+" ORDER BY _createdTime DESC", params...)
	if err != nil || ms == nil {
		reverse(u.Messages)
		return err
	}
	defer ms.Close()
	idx := colIndex(ms)
	ev := idx["linecli_event"]
	rows := true
	for len(u.Messages) < MaxLimit {
		if rows = ms.Step(); !rows {
			break
		}
		if ms.ColumnInt64(ev) == 0 {
			if taken == quota {
				break
			}
			taken++
		} else if taken == quota && !whole {
			break
		}
		u.Messages = append(u.Messages, r.readMessageRow(ms, idx))
	}
	u.MoreUnread = rows && taken < want
	reverse(u.Messages)
	return stepErr(ms)
}

// unreadBoundary picks u.Boundary and returns the matching _message filter.
func (r *Reader) unreadBoundary(u *UnreadChat) (string, []any, error) {
	where, params := "_chatId=?", []any{u.ChatID}
	st, err := r.query(`SELECT coalesce(_firstUnreadId,''), coalesce(_sendChatCheckedId,''),
		(SELECT _createdTime FROM _message m WHERE m._id=c._firstUnreadId AND m._chatId=c._id),
		(SELECT _createdTime FROM _message m WHERE m._id=c._sendChatCheckedId AND m._chatId=c._id)
		FROM _chat c WHERE c._id=?`, u.ChatID)
	if err != nil || st == nil {
		return where, params, err
	}
	defer st.Close()
	if st.Step() {
		firstUnread := st.ColumnText(0)
		if isOpenChatID(u.ChatID) {
			firstUnread = "" // not a message id in open chats
		}
		switch {
		case firstUnread != "" && st.ColumnType(2) != sqlite3.NULL:
			u.Boundary = BoundaryFirstUnread
			where += " AND _createdTime>=?"
			params = append(params, st.ColumnInt64(2))
		case st.ColumnType(3) != sqlite3.NULL:
			u.Boundary = BoundaryReadMarker
			where += " AND _createdTime>?"
			params = append(params, st.ColumnInt64(3))
		case firstUnread != "" || st.ColumnText(1) != "":
			u.Boundary = BoundaryNotStored
		}
	}
	return where, params, stepErr(st)
}

func (r *Reader) count(sql string, params ...any) (int64, error) {
	st, err := r.query(sql, params...)
	if err != nil || st == nil {
		return 0, err
	}
	defer st.Close()
	var n int64
	if st.Step() {
		n = st.ColumnInt64(0)
	}
	return n, stepErr(st)
}
