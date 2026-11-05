package line

import (
	"fmt"
	"testing"

	"github.com/ncruces/go-sqlite3"
)

// The DDL mirrors LINE PC's tables (schema only); every row below is synthetic.
const fakeSchema = `
CREATE TABLE _profile(_allowSearchByEmail INTEGER, _allowSearchByUserid INTEGER, _displayName TEXT, _email TEXT, _mid TEXT, _phone TEXT, _phoneticName TEXT, _picturePath TEXT, _pictureStatus TEXT, _regionCode TEXT, _statusMessage TEXT, _thumbnailUrl TEXT, _userid TEXT, _videoProfile TEXT, _musicProfile TEXT, _statusMessageContentMetadata TEXT, PRIMARY KEY ( _mid ) );
CREATE TABLE _room(_mid TEXT, _createdTime INTEGER, _contacts TEXT, _notificationDisabled INTEGER, _invitor TEXT, _memberMids TEXT, PRIMARY KEY ( _mid ) );
CREATE TABLE _contact(_mid TEXT, _createdTime INTEGER, _type INTEGER, _status INTEGER, _displayName TEXT, _displayNameOverridden TEXT, _phoneDisplayName TEXT, _pictureStatus TEXT, _thumbnailUrl TEXT, _statusMessage TEXT, _favoriteTime INTEGER, _capableVoiceCall INTEGER, _capableVideoCall INTEGER, _capableMyhome INTEGER, _capableBuddy INTEGER, _attributes INTEGER, _settings INTEGER, _picturePath TEXT, _recordId TEXT, _addedTime INTEGER, _recommendParams TEXT, _unregisteredContact INTEGER, _videoProfile TEXT, _musicProfile TEXT, _profileUpdateSign INTEGER, _statusMessageContentMetadata TEXT, _userType INTEGER, _targetProfileDetail TEXT, _friendDetail TEXT, _blockDetail TEXT, _recommendationOrNot TEXT, _notificationSettingEntry TEXT, PRIMARY KEY ( _mid ) );
CREATE TABLE _buddyDetail(_mid TEXT, _memberCount INTEGER, _onAir INTEGER, _businessAccount INTEGER, PRIMARY KEY ( _mid ) );
CREATE TABLE _groupChat(_type INTEGER, _chatMid TEXT, _createdTime INTEGER, _notificationDisabled INTEGER, _favoriteTimestamp INTEGER, _chatName TEXT, _picturePath TEXT, _extra TEXT, _status INTEGER, _addedTime INTEGER, _inviterMId TEXT, PRIMARY KEY ( _chatMid ) );
CREATE TABLE _square(_mid TEXT, _name TEXT, _welcomeMessage TEXT, _profileImageObsHash TEXT, _desc TEXT, _searchable INTEGER, _type INTEGER, _categoryId INTEGER, _invitationURL TEXT, _revision INTEGER, _ableToUseInvitationTicket INTEGER, _state INTEGER, _emblems TEXT, _joinMethod TEXT, _adultOnly INTEGER, _svcTags TEXT, _createdAt INTEGER, _authority TEXT, _creatorId TEXT, _status TEXT, _myMemberId TEXT, _featureSet TEXT, _lastJoinRequestAt INTEGER, _lastVisitTime INTEGER, _noteStatus TEXT, _metadata TEXT, PRIMARY KEY ( _mid ) );
CREATE TABLE _squareChat(_squareChatMid TEXT, _squareMid TEXT, _type INTEGER, _name TEXT, _chatImageObsHash TEXT, _squareChatRevision INTEGER, _maxMemberCount INTEGER, _state INTEGER, _status TEXT, _myMember TEXT, _peerMember TEXT, _joinedAt INTEGER, _announcement TEXT, _featureSet TEXT, PRIMARY KEY ( _squareChatMid ) );
CREATE TABLE _squareMember(_squareMemberMid TEXT, _squareMid TEXT, _displayName TEXT, _profileImageObsHash TEXT, _ableToReceiveMessage INTEGER, _membershipState INTEGER, _role INTEGER, _revision INTEGER, _preference TEXT, _joinMessage TEXT, _singleChatId TEXT, _relation TEXT, _myMember TEXT, PRIMARY KEY ( _squareMemberMid ) );
CREATE TABLE _squareThread(_threadMid TEXT, _chatMid TEXT, _squareMid TEXT, _messageId TEXT, _state INTEGER, _expiresAt INTEGER, _readOnlyAt INTEGER, _revision INTEGER, _squareThreadMember TEXT, _totalMessageCount INTEGER, _unreadCount INTEGER, _lastMessage TEXT, _rootMessage TEXT, _lastMessageAt INTEGER, PRIMARY KEY ( _threadMid ) );
CREATE TABLE _chat(_id TEXT, _midType INTEGER, _lastMessage TEXT, _lastUpdatedTime INTEGER, _sendChatCheckedId TEXT, _messageBoxNextId INTEGER, _unreadCount INTEGER, _onAir INTEGER, _showMember INTEGER, _status INTEGER, _memberHistory TEXT, _lastReceivedOpRev INTEGER, _announcement TEXT, _firstUnreadId TEXT, _messageBoxNextCreatedTime INTEGER, _lastSyncToken TEXT, _mediaMessageFlow TEXT, _messageEditNotice TEXT, PRIMARY KEY ( _id ) );
CREATE TABLE _chatOption(_id TEXT, _hidden INTEGER, _notification INTEGER, _pinnedTime INTEGER, _utilityData TEXT, PRIMARY KEY ( _id ) );
CREATE TABLE _message(_from TEXT, _to TEXT, _toType INTEGER, _id TEXT, _createdTime INTEGER, _deliveredTime INTEGER, _text TEXT, _location TEXT, _hasContent INTEGER, _contentType INTEGER, _contentMetadata TEXT, _contentPreview TEXT, _sessionId INTEGER, _chunks TEXT, _relatedMessageId TEXT, _messageRelationType INTEGER, _readCount INTEGER, _relatedMessageServiceCode INTEGER, _appExtensionType INTEGER, _reactions TEXT, _type INTEGER, _status INTEGER, _chatId TEXT, _reqSeqV2 INTEGER, _reqSeq INTEGER, _contentInfo TEXT, _eventInfo TEXT, _rev INTEGER, _errorCode INTEGER, _urlPreview TEXT, _hasUrlPreview INTEGER, _syncToken TEXT, _fromType INTEGER, _attribute INTEGER, _flag INTEGER, _reactionStatus TEXT, PRIMARY KEY ( _id ) );
CREATE INDEX _message__chatId__createdTime ON _message ( _chatId, _createdTime );
`

const t0 = int64(1_700_000_000_000)

func at(i int) int64 { return t0 + int64(i)*60_000 }

type fakeDB struct {
	t    *testing.T
	conn *sqlite3.Conn
}

func (f fakeDB) exec(format string, args ...any) {
	f.t.Helper()
	if err := f.conn.Exec(fmt.Sprintf(format, args...)); err != nil {
		f.t.Fatalf("%v: %s", err, fmt.Sprintf(format, args...))
	}
}

// msg stores a message; kind is "", "event" (_type 3) or "notice" (ct 18).
func (f fakeDB) msg(chat, id string, i int, kind string) {
	typ, ct := 1, 0
	switch kind {
	case "event":
		typ = 3
	case "notice":
		ct = 18
	}
	toType := map[byte]int{'u': 0, 'c': 2, 'm': 4, 't': 7}[chat[0]]
	f.exec(`INSERT INTO _message(_from,_to,_toType,_id,_createdTime,_text,_contentType,_contentMetadata,_type,_status,_chatId,_attribute)
		VALUES('ufake01','%s',%d,'%s',%d,'synthetic %s',%d,'',%d,1,'%s',0)`, chat, toType, id, at(i), id, ct, typ, chat)
}

func (f fakeDB) chat(id string, unread int, firstUnread, readMarker, lastID string, lastAt, updated int64) {
	last := ""
	if lastID != "" {
		last = fmt.Sprintf(`{"id":"%s","createdTime":%d}`, lastID, lastAt)
	}
	f.exec(`INSERT INTO _chat(_id,_lastMessage,_lastUpdatedTime,_sendChatCheckedId,_unreadCount,_firstUnreadId) VALUES('%s','%s',%d,'%s',%d,'%s')`,
		id, last, updated, readMarker, unread, firstUnread)
}

func newFakeReader(t *testing.T) *Reader {
	t.Helper()
	db := newFakeDB(t)
	t.Cleanup(func() { db.Close() })
	return NewReader(db)
}

// newFakeDB returns an unencrypted in-memory DB the caller must close.
func newFakeDB(t *testing.T) *DB {
	t.Helper()
	conn, err := sqlite3.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	f := fakeDB{t, conn}
	f.exec("%s", fakeSchema)
	f.exec(`INSERT INTO _profile(_mid,_displayName) VALUES('ufakeme','me')`)
	f.exec(`INSERT INTO _contact(_mid,_type,_displayName) VALUES('ufake01',0,'fake one'),('u0000000001',0,'fake two'),('u0000000002',0,'fake three')`)
	f.exec(`INSERT INTO _groupChat(_chatMid,_chatName) VALUES('c0000000001','fake group')`)
	f.exec(`INSERT INTO _squareChat(_squareChatMid,_name) VALUES('m0000000001','fake open chat')`)

	// Group: 1-5 read; LINE's first unread is 6; 8 is a notice and 9 an event.
	for i := 1; i <= 10; i++ {
		kind := map[int]string{8: "notice", 9: "event"}[i]
		f.msg("c0000000001", fmt.Sprintf("g%02d", i), i, kind)
	}
	f.chat("c0000000001", 3, "g06", "g05", "g10", at(10), at(10))
	f.exec(`INSERT INTO _chatOption(_id,_hidden,_notification) VALUES('c0000000001',0,0)`)

	// Open chat: read up to o03; _firstUnreadId is not an id; LINE's last
	// message (an hour later) is not stored, so 10 unread but only o04, o05 here.
	for i := 1; i <= 5; i++ {
		kind := map[int]string{4: "event"}[i]
		f.msg("m0000000001", fmt.Sprintf("o%02d", i), i, kind)
	}
	f.msg("m0000000001", "o06", 6, "")
	f.chat("m0000000001", 10, "0", "o03", "o99", at(66), at(66))
	f.exec(`INSERT INTO _chatOption(_id,_hidden,_notification) VALUES('m0000000001',0,1)`)

	// 1:1 without any stored boundary.
	for i := 1; i <= 4; i++ {
		f.msg("u0000000001", fmt.Sprintf("p%02d", i), i, "")
	}
	f.chat("u0000000001", 2, "", "", "p04", at(4), at(4))

	// Hidden 1:1 with a normal boundary.
	f.msg("u0000000002", "h01", 1, "")
	f.chat("u0000000002", 1, "h01", "", "h01", at(1), at(1))
	f.exec(`INSERT INTO _chatOption(_id,_hidden,_notification) VALUES('u0000000002',1,1)`)

	// Thread with activity but nothing stored, and a read group with nothing unread.
	f.chat("t0000000001", 0, "", "", "", 0, at(50))
	f.msg("c0000000002", "r01", 1, "")
	f.chat("c0000000002", 0, "", "r01", "r01", at(1), at(1))

	return &DB{conn: conn}
}

func unreadByID(t *testing.T, r *Reader, opt UnreadOptions) map[string]UnreadChat {
	t.Helper()
	res, err := r.Unread(opt)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]UnreadChat{}
	for _, u := range res {
		out[u.ChatID] = u
	}
	return out
}

func msgIDs(ms []Message) string {
	s := ""
	for _, m := range ms {
		s += m.ID + " "
	}
	return s
}

func TestUnreadUsesReadBoundary(t *testing.T) {
	r := newFakeReader(t)
	got := unreadByID(t, r, UnreadOptions{})
	if len(got) != 3 {
		t.Fatalf("got %d chats, want 3 (hidden chat skipped)", len(got))
	}

	g := got["c0000000001"]
	if g.Boundary != BoundaryFirstUnread || g.AvailableCount != 3 || g.MissingCount != 0 || !g.FullySynced || g.Stale || !g.Muted || g.MoreUnread {
		t.Errorf("group: %+v", g)
	}
	if ids := msgIDs(g.Messages); ids != "g06 g07 g08 g09 g10 " {
		t.Errorf("group messages = %q, want the unread ones plus the notice and event among them", ids)
	}

	o := got["m0000000001"]
	if o.Boundary != BoundaryReadMarker || o.AvailableCount != 2 || o.MissingCount != 8 || o.FullySynced || !o.Stale || o.Muted {
		t.Errorf("open chat: %+v", o)
	}
	if ids := msgIDs(o.Messages); ids != "o04 o05 o06 " {
		t.Errorf("open chat messages = %q, want those after the read marker", ids)
	}
	if o.LastActivityAt != tsToIso(at(66)) || o.NewestStoredAt != tsToIso(at(6)) {
		t.Errorf("open chat times: %q %q", o.LastActivityAt, o.NewestStoredAt)
	}

	p := got["u0000000001"]
	if p.Boundary != BoundaryNone || p.AvailableCount != 0 || p.MissingCount != 2 || p.FullySynced || p.Stale {
		t.Errorf("no boundary: %+v", p)
	}
	if ids := msgIDs(p.Messages); ids != "p03 p04 " {
		t.Errorf("no boundary messages = %q, want the newest unread_count", ids)
	}
}

func TestUnreadEventsDoNotUsePerChatLimit(t *testing.T) {
	r := newFakeReader(t)
	g := unreadByID(t, r, UnreadOptions{PerChatLimit: 2})["c0000000001"]
	if ids := msgIDs(g.Messages); ids != "g07 g08 g09 g10 " {
		t.Errorf("messages = %q, want the newest 2 unread with the events between them", ids)
	}
	if !g.MoreUnread || g.AvailableCount != 3 {
		t.Errorf("want MoreUnread with 3 available: %+v", g)
	}
	g = unreadByID(t, r, UnreadOptions{PerChatLimit: 3})["c0000000001"]
	if g.MoreUnread || len(g.Messages) != 5 {
		t.Errorf("limit 3: MoreUnread=%v messages=%q", g.MoreUnread, msgIDs(g.Messages))
	}
}

// A read marker that lags behind LINE's unread count must not offer read
// messages as unread.
func TestUnreadLaggingMarkerKeepsUnreadCount(t *testing.T) {
	r := newFakeReader(t)
	f := fakeDB{t, r.db.conn}
	for i := 1; i <= 6; i++ {
		f.msg("m0000000002", fmt.Sprintf("q%02d", i), i, map[int]string{5: "notice"}[i])
	}
	f.chat("m0000000002", 2, "", "q01", "q06", at(6), at(6))
	for i := 1; i <= 5; i++ {
		f.msg("u0000000003", fmt.Sprintf("v%02d", i), i, "")
	}
	f.chat("u0000000003", 1, "", "v02", "v05", at(5), at(5))

	got := unreadByID(t, r, UnreadOptions{})
	o := got["m0000000002"]
	if o.Boundary != BoundaryUnreadCount || o.AvailableCount != 2 || o.MissingCount != 0 || o.FullySynced || o.MoreUnread {
		t.Errorf("open chat: %+v", o)
	}
	if ids := msgIDs(o.Messages); ids != "q04 q05 q06 " {
		t.Errorf("open chat messages = %q, want the newest 2 unread and the notice between them", ids)
	}
	p := got["u0000000003"]
	if p.Boundary != BoundaryUnreadCount || p.AvailableCount != 1 || msgIDs(p.Messages) != "v05 " {
		t.Errorf("1:1 without first unread: %+v", p)
	}
	o = unreadByID(t, r, UnreadOptions{PerChatLimit: 1})["m0000000002"]
	if !o.MoreUnread || msgIDs(o.Messages) != "q06 " {
		t.Errorf("limit 1: MoreUnread=%v messages=%q", o.MoreUnread, msgIDs(o.Messages))
	}
}

// When LINE's boundary message is not stored, unread must not fall back to
// the newest stored messages, which are already read.
func TestUnreadBoundaryNotStored(t *testing.T) {
	r := newFakeReader(t)
	f := fakeDB{t, r.db.conn}
	for i := 1; i <= 3; i++ {
		f.msg("u0000000003", fmt.Sprintf("s%02d", i), i, "")
		f.msg("u0000000004", fmt.Sprintf("w%02d", i), i, "")
		f.msg("m0000000003", fmt.Sprintf("x%02d", i), i, "")
	}
	f.chat("u0000000003", 2, "s99", "s03", "s99", at(9), at(9))
	f.chat("u0000000004", 2, "w99", "w98", "w99", at(9), at(9))
	f.chat("m0000000003", 2, "0", "x99", "x99", at(9), at(9))

	got := unreadByID(t, r, UnreadOptions{})
	s := got["u0000000003"]
	if s.Boundary != BoundaryReadMarker || s.AvailableCount != 0 || s.MissingCount != 2 || s.FullySynced || len(s.Messages) != 0 || s.MoreUnread {
		t.Errorf("first unread not stored, read marker stored: %+v", s)
	}
	for _, id := range []string{"u0000000004", "m0000000003"} {
		w := got[id]
		if w.Boundary != BoundaryNotStored || w.AvailableCount != 0 || w.MissingCount != 2 || w.FullySynced || len(w.Messages) != 0 || w.MoreUnread {
			t.Errorf("%s: no boundary message stored: %+v", id, w)
		}
	}
}

func TestUnreadNoBoundaryCutByLimit(t *testing.T) {
	r := newFakeReader(t)
	p := unreadByID(t, r, UnreadOptions{PerChatLimit: 1})["u0000000001"]
	if p.Boundary != BoundaryNone || !p.MoreUnread || msgIDs(p.Messages) != "p04 " {
		t.Errorf("boundary none cut by --per-chat-limit: MoreUnread=%v messages=%q", p.MoreUnread, msgIDs(p.Messages))
	}
	f := fakeDB{t, r.db.conn}
	f.msg("u0000000003", "y01", 1, "")
	f.chat("u0000000003", 5, "", "", "y01", at(1), at(1))
	y := unreadByID(t, r, UnreadOptions{PerChatLimit: 3})["u0000000003"]
	if y.MoreUnread || msgIDs(y.Messages) != "y01 " {
		t.Errorf("fewer stored than unread is not a --per-chat-limit cut: MoreUnread=%v messages=%q", y.MoreUnread, msgIDs(y.Messages))
	}
}

func TestUnreadNotSyncedWhileLineStopped(t *testing.T) {
	r := newFakeReader(t)
	for id, u := range unreadByID(t, r, UnreadOptions{IncludeHidden: true, LineStopped: true}) {
		if u.FullySynced {
			t.Errorf("%s fully synced while LINE is not running", id)
		}
	}
	if g := unreadByID(t, r, UnreadOptions{})["c0000000001"]; !g.FullySynced {
		t.Errorf("group not synced while LINE runs: %+v", g)
	}
}

func TestUnreadIncludeHidden(t *testing.T) {
	r := newFakeReader(t)
	h, ok := unreadByID(t, r, UnreadOptions{IncludeHidden: true})["u0000000002"]
	if !ok || !h.Hidden || h.Boundary != BoundaryFirstUnread || h.AvailableCount != 1 || !h.FullySynced {
		t.Fatalf("hidden chat: ok=%v %+v", ok, h)
	}
	if res, _ := r.Unread(UnreadOptions{LimitChats: 1}); len(res) != 1 {
		t.Fatalf("--limit-chats 1 returned %d chats", len(res))
	}
}

func TestChatFreshness(t *testing.T) {
	r := newFakeReader(t)
	for _, c := range []struct {
		chat          string
		stale         bool
		missing       bool
		oldest        string
		newest, first int64
	}{
		{"c0000000001", false, false, "g01", at(10), at(1)},
		{"fake group", false, false, "g01", at(10), at(1)},
		{"m0000000001", true, true, "o01", at(6), at(1)},
		{"t0000000001", true, false, "", 0, 0},
		{"u0000000001", false, false, "p01", at(4), at(1)},
	} {
		f, err := r.ChatFreshness(c.chat)
		if err != nil || f == nil {
			t.Fatalf("%s: %v", c.chat, err)
		}
		if f.Stale() != c.stale || f.LastMissing != c.missing || f.OldestID != c.oldest || f.NewestStored != c.newest || f.OldestStored != c.first {
			t.Errorf("%s: stale=%v %+v", c.chat, f.Stale(), *f)
		}
	}
	if _, err := r.ChatFreshness("zzz no such chat"); err == nil {
		t.Error("unknown chat: want CHAT_NOT_FOUND")
	}
}

func TestStaleFor(t *testing.T) {
	f := Freshness{LastActivity: at(66), NewestStored: at(6)}
	sec := func(ms int64) int64 { return ms / 1000 }
	for _, c := range []struct {
		until int64
		want  bool
	}{
		{0, true},
		{sec(at(6)), false},                  // ends at the newest stored message
		{sec(at(6)+staleSlackMs) - 1, false}, // still within the slack (--until covers its whole second)
		{sec(at(8)), true},                   // reaches into the unsynced span
		{sec(at(1)), false},
	} {
		if got := f.StaleFor(c.until); got != c.want {
			t.Errorf("StaleFor(%d) = %v, want %v", c.until, got, c.want)
		}
	}
	fresh := Freshness{LastActivity: at(6) + staleSlackMs, NewestStored: at(6)}
	if fresh.Stale() || fresh.StaleFor(0) {
		t.Error("activity within the slack is not stale")
	}
	if !(&Freshness{LastMissing: true, LastActivity: at(6), NewestStored: at(6)}).Stale() {
		t.Error("an unstored last message is stale")
	}
	empty := Freshness{LastMissing: true, LastActivity: at(50)}
	if empty.StaleFor(sec(at(49))) || !empty.StaleFor(sec(at(50))) || !empty.StaleFor(0) {
		t.Error("with nothing stored, only a range reaching the last activity is stale")
	}
}

func TestFillStatus(t *testing.T) {
	r := newFakeReader(t)
	var s Status
	if err := r.fillStatus(&s); err != nil {
		t.Fatal(err)
	}
	if s.NewestMessageAt != tsToIso(at(10)) || s.OldestMessageAt != tsToIso(at(1)) {
		t.Errorf("span %q..%q", s.OldestMessageAt, s.NewestMessageAt)
	}
	// The thread is stale too but keeps its replies on the server.
	if s.StaleChats == nil || s.StaleChats.Count != 1 || len(s.StaleChats.ChatIDs) != 1 || s.StaleChats.ChatIDs[0] != "m0000000001" {
		t.Errorf("stale_chats %+v", s.StaleChats)
	}
}
