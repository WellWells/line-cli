package line

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ncruces/go-sqlite3"
)

// Synthetic database: every id and name below is made up.

func fid(prefix string, n int) string { return fmt.Sprintf("%s%032x", prefix, n) }

var (
	me       = fid("u", 1)
	myMember = fid("p", 900)
	alice    = fid("u", 2)  // contact, nickname set
	ann      = fid("u", 3)  // contact
	anna     = fid("p", 4)  // open-chat member
	oa       = fid("u", 5)  // official via _buddyDetail only
	oa8      = fid("u", 6)  // official via _type=8
	stranger = fid("u", 7)  // 1:1 chat, no contact row
	bot      = fid("v", 8)  // sends messages, in no name table
	annUpper = fid("p", 9)  // open-chat member named ANN
	noChat   = fid("u", 10) // contact without a chat
	group    = fid("c", 20)
	orphan   = fid("c", 21) // group with no _groupChat row
	square   = fid("s", 30)
	open     = fid("m", 31)
	thread   = fid("t", 32)
	room     = fid("r", 33)
)

const identitySchema = `
CREATE TABLE _chat (_id TEXT PRIMARY KEY, _midType INTEGER, _lastUpdatedTime INTEGER);
CREATE TABLE _chatOption (_id TEXT PRIMARY KEY, _hidden INTEGER, _notification INTEGER, _pinnedTime INTEGER);
CREATE TABLE _groupChat (_chatMid TEXT PRIMARY KEY, _chatName TEXT, _extra TEXT);
CREATE TABLE _squareChat (_squareChatMid TEXT PRIMARY KEY, _squareMid TEXT, _name TEXT);
CREATE TABLE _squareThread (_threadMid TEXT PRIMARY KEY, _chatMid TEXT, _squareMid TEXT);
CREATE TABLE _square (_mid TEXT PRIMARY KEY, _name TEXT, _myMemberId TEXT);
CREATE TABLE _squareMember (_squareMemberMid TEXT PRIMARY KEY, _squareMid TEXT, _displayName TEXT, _role INTEGER, _membershipState INTEGER);
CREATE TABLE _contact (_mid TEXT, _displayName TEXT, _displayNameOverridden TEXT, _type INTEGER, _capableBuddy INTEGER, _status INTEGER);
CREATE TABLE _buddyDetail (_mid TEXT PRIMARY KEY);
CREATE TABLE _room (_mid TEXT PRIMARY KEY);
CREATE TABLE _profile (_mid TEXT, _displayName TEXT);
CREATE TABLE _message (_id TEXT, _chatId TEXT, _from TEXT, _createdTime INTEGER, _text TEXT, _contentType INTEGER, _contentMetadata TEXT, _contentInfo TEXT, _type INTEGER, _attribute INTEGER);
`

func testReader(t *testing.T, extra ...string) *Reader {
	t.Helper()
	conn, err := sqlite3.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	stmts := []string{identitySchema,
		fmt.Sprintf("INSERT INTO _profile VALUES (%s, 'Me Myself')", q(me)),
		fmt.Sprintf("INSERT INTO _square VALUES (%s, 'Hiking Club', %s)", q(square), q(myMember)),
		fmt.Sprintf("INSERT INTO _squareChat VALUES (%s, %s, 'Hiking Talk')", q(open), q(square)),
		fmt.Sprintf("INSERT INTO _squareThread VALUES (%s, %s, %s)", q(thread), q(open), q(square)),
		fmt.Sprintf("INSERT INTO _squareMember VALUES (%s, %s, 'Me Myself', 10, 2), (%s, %s, 'Anna', 1, 2), (%s, %s, 'ANN', 10, 4)",
			q(myMember), q(square), q(anna), q(square), q(annUpper), q(square)),
		fmt.Sprintf(`INSERT INTO _contact VALUES (%s, 'Alice Wong', 'Ali', 0, 0, 1), (%s, 'Ann', '', 0, 0, 0), (%s, 'Shop', '', 0, NULL, 6),
			(%s, 'News', '', 8, 1, 1), (%s, 'Zed', '', 0, 0, 2), (NULL, '', '', NULL, NULL, NULL), ('', '', '', NULL, NULL, NULL)`,
			q(alice), q(ann), q(oa), q(oa8), q(noChat)),
		fmt.Sprintf("INSERT INTO _buddyDetail VALUES (%s), (%s)", q(oa), q(oa8)),
		fmt.Sprintf("INSERT INTO _room VALUES (%s)", q(room)),
		fmt.Sprintf(`INSERT INTO _groupChat VALUES (%s, 'Family', '{"groupExtra":{"memberMids":{%s:1700000000000,%s:1700000001000,%s:1700000002000},"inviteeMids":{%s:1700000003000},"creator":%s}}')`,
			q(group), `"`+me+`"`, `"`+alice+`"`, `"`+ann+`"`, `"`+noChat+`"`, `"`+alice+`"`),
		fmt.Sprintf(`INSERT INTO _chat VALUES (%s,0,1000), (%s,0,9000), (%s,0,2000), (%s,0,3000), (%s,0,4000), (%s,0,5000),
			(%s,2,6000), (%s,2,0), (%s,4,7000), (%s,7,8000), (%s,1,500), (%s,NULL,100)`,
			q(me), q(alice), q(ann), q(oa), q(oa8), q(stranger), q(group), q(orphan), q(open), q(thread), q(room), q("x123")),
		fmt.Sprintf("INSERT INTO _chatOption VALUES (%s, 1, 1, 0), (%s, 0, 0, 0), (%s, 0, 1, 1700000000000)", q(stranger), q(open), q(group)),
	}
	msgs := []struct {
		chat, from string
		at         int64
		typ, ct    int
	}{
		{group, alice, 1_700_000_100_000, 1, 0},
		{group, alice, 1_700_000_200_000, 1, 0},
		{group, ann, 1_700_000_300_000, 1, 0},
		{group, ann, 1_700_000_400_000, 3, 0},  // member event, not a message
		{group, ann, 1_700_000_500_000, 1, 18}, // notice
		{orphan, ann, 1_700_000_600_000, 1, 0}, // newer than _lastUpdatedTime 0
		{open, anna, 1_700_000_700_000, 1, 0},  // open-chat member
		{open, bot, 1_700_000_800_000, 1, 0},   // bot id in no name table
		{open, myMember, 1_700_000_900_000, 1, 0},
		{me, me, 1_700_001_000_000, 1, 0},
		{stranger, stranger, 1_700_009_000_000, 1, 0}, // newest of all
	}
	for i, m := range msgs {
		stmts = append(stmts, fmt.Sprintf("INSERT INTO _message VALUES ('%d', %s, %s, %d, 'x', %d, '', '{\"fromName\":\"Snap\"}', %d, 0)", i+1, q(m.chat), q(m.from), m.at, m.ct, m.typ))
	}
	for _, s := range append(stmts, extra...) {
		if err := conn.Exec(s); err != nil {
			t.Fatalf("%v: %s", err, s)
		}
	}
	return NewReader(&DB{conn: conn})
}

func chatByID(t *testing.T, r *Reader) map[string]ChatSummary {
	t.Helper()
	rows, err := r.ListChats(ChatsOptions{Limit: 5000})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]ChatSummary{}
	for _, c := range rows {
		out[c.ChatID] = c
	}
	return out
}

func TestChatClassification(t *testing.T) {
	r := testReader(t)
	chats := chatByID(t, r)
	for id, want := range map[string][2]string{
		me:       {"self", "Me Myself"},
		alice:    {"personal", "Ali"},
		ann:      {"personal", "Ann"},
		oa:       {"official", "Shop"},
		oa8:      {"official", "News"},
		stranger: {"personal", stranger},
		group:    {"group", "Family"},
		orphan:   {"group", orphan},
		open:     {"open", "Hiking Talk"},
		thread:   {"thread", "Hiking Talk (thread)"},
		room:     {"multi", "多人聊天"},
		"x123":   {"unknown", "x123"},
	} {
		if c := chats[id]; c.Type != want[0] || c.Name != want[1] {
			t.Errorf("%s: got (%s, %s), want %v", id, c.Type, c.Name, want)
		}
	}
	if c := chats[thread]; c.ParentChatID != open {
		t.Errorf("thread parent = %q", c.ParentChatID)
	}
	if c := chats[group]; c.MemberCount != 3 || !c.Pinned || c.Muted || c.Hidden {
		t.Errorf("group flags: %+v", c)
	}
	if c := chats[open]; !c.Muted || c.Pinned {
		t.Errorf("open flags: %+v", c)
	}
	if c := chats[stranger]; !c.Hidden {
		t.Errorf("stranger not hidden: %+v", c)
	}
	for _, typ := range ChatTypeNames() {
		if !IsChatTypeFilter(typ) {
			t.Errorf("%s not accepted as --chat-type", typ)
		}
		rows, err := r.ListChats(ChatsOptions{ChatType: typ, Limit: 5000})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range rows {
			if c.Type != typ {
				t.Errorf("--chat-type %s returned %s", typ, c.Type)
			}
		}
	}
	ids, err := r.chatIdsOfType("official")
	if err != nil || !slices.Contains(ids, oa) || !slices.Contains(ids, oa8) || len(ids) != 2 {
		t.Errorf("official chat ids: %v %v", ids, err)
	}
}

func TestListChatsActivity(t *testing.T) {
	r := testReader(t)
	rows, err := r.ListChats(ChatsOptions{Limit: 5000})
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].ChatID != stranger {
		t.Errorf("newest stored message does not lead: first is %s", rows[0].ChatID)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i-1].LastMessageAt < rows[i].LastMessageAt {
			t.Errorf("not sorted by last_message_at at %d", i)
		}
	}
	if c := chatByID(t, r)[orphan]; c.LastMessageAt != tsToIso(1_700_000_600_000) {
		t.Errorf("orphan last_message_at = %q", c.LastMessageAt)
	}
	got, _ := r.ListChats(ChatsOptions{Query: "alice", Limit: 5000})
	if len(got) != 1 || got[0].ChatID != alice {
		t.Errorf("query by own name of a nicknamed contact: %v", got)
	}
}

func TestResolveChatTiers(t *testing.T) {
	r := testReader(t)
	for _, c := range []struct {
		arg   string
		exact bool
		want  string
		code  ErrorCode
	}{
		{alice, true, alice, ""},
		{"Ali", true, alice, ""},
		{"Alice Wong", true, alice, ""}, // own name of a nicknamed contact
		{"alice wong", true, "", ErrChatNotFound},
		{"alice wong", false, alice, ""}, // case-insensitive tier
		{"family", false, group, ""},
		{"Hiking", false, open, ""}, // threads only match when nothing else does
		{"(thread)", false, thread, ""},
		{"Ann", false, ann, ""}, // exact name, though "n" alone is ambiguous
		{"n", false, "", ErrChatAmbiguous},
		{"Zed", false, "", ErrChatNotFound},
		{"nothing here", false, "", ErrChatNotFound},
		{" ", false, "", ErrInvalidArgument},
	} {
		got, err := r.ResolveChatInfo(c.arg, c.exact)
		le, _ := AsError(err)
		switch {
		case c.code != "" && (le == nil || le.Code != c.code):
			t.Errorf("%q: err %v, want %s", c.arg, err, c.code)
		case c.code == "" && (err != nil || got.ChatID != c.want):
			t.Errorf("%q: got %q err %v, want %q", c.arg, got.ChatID, err, c.want)
		}
	}
	_, err := r.ResolveChatInfo("Zed", false)
	if le, _ := AsError(err); le == nil || !strings.Contains(le.Message, "search --from") {
		t.Errorf("not-found contact gives no hint: %v", err)
	}
}

// A chat_id prefix works like a short git commit id: 4 characters or more, used
// when only one chat matches, after exact names and before name substrings, and
// ambiguous with a chat whose name contains it; send (exact) never takes one.
func TestResolveChatIDPrefix(t *testing.T) {
	g1, g2 := "cabcdef01"+strings.Repeat("0", 24), "cabcdef02"+strings.Repeat("0", 24)
	cafeID, cafeName := "cafe1"+strings.Repeat("0", 28), "c9"+strings.Repeat("0", 31)
	r := testReader(t,
		"INSERT INTO _groupChat VALUES ('"+g1+"', 'Alpha Team', '{}')", "INSERT INTO _chat VALUES ('"+g1+"', 2, 50001)",
		"INSERT INTO _groupChat VALUES ('"+g2+"', 'Beta Team', '{}')", "INSERT INTO _chat VALUES ('"+g2+"', 2, 50002)",
		"INSERT INTO _groupChat VALUES ('"+cafeID+"', 'Hikers', '{}')", "INSERT INTO _chat VALUES ('"+cafeID+"', 2, 50003)",
		"INSERT INTO _groupChat VALUES ('"+cafeName+"', 'Cafe Lovers', '{}')", "INSERT INTO _chat VALUES ('"+cafeName+"', 2, 50004)")
	for _, c := range []struct {
		arg   string
		exact bool
		want  string
		code  ErrorCode
	}{
		{"cabcdef01", false, g1, ""},
		{"CABCDEF02", false, g2, ""},              // letter case does not matter
		{"cabcdef0", false, "", ErrChatAmbiguous}, // both match: give more digits
		{"cabc", false, "", ErrChatAmbiguous},     // 4 characters, git's minimum, still both
		{"cab", false, "", ErrChatNotFound},       // 3 characters: only a name, which matches nothing
		{"cafe", false, "", ErrChatAmbiguous},     // a chat_id starts with it and a name contains it
		{"cafe1", false, cafeID, ""},
		{"Cafe Lovers", false, cafeName, ""},     // an exact name comes before any prefix
		{"cabcdef01", true, "", ErrChatNotFound}, // send takes the whole chat_id or an exact name
		{g1, true, g1, ""},
		{"Alpha", false, g1, ""}, // names still work
	} {
		got, err := r.ResolveChatInfo(c.arg, c.exact)
		le, _ := AsError(err)
		switch {
		case c.code != "" && (le == nil || le.Code != c.code):
			t.Errorf("%q exact=%v: err %v, want %s", c.arg, c.exact, err, c.code)
		case c.code == "" && (err != nil || got.ChatID != c.want):
			t.Errorf("%q exact=%v: got %q err %v, want %q", c.arg, c.exact, got.ChatID, err, c.want)
		}
	}
	for arg, want := range map[string][]string{"cabcdef0": {g2, g1}, "cafe": {cafeName, cafeID}} {
		_, err := r.ResolveChatInfo(arg, false)
		le, _ := AsError(err)
		var got []string
		if le != nil {
			for _, c := range le.Candidates {
				got = append(got, c.ChatID)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%q candidates %v, want %v (most recent first)", arg, got, want)
		}
	}
	got, _ := r.ListChats(ChatsOptions{Query: "cabcdef0", Limit: 5000})
	if len(got) != 2 {
		t.Errorf("chats by id prefix: %v", got)
	}
}

func TestChatAmbiguousCandidates(t *testing.T) {
	var extra []string
	for i := range 25 {
		id := fid("c", 100+i)
		extra = append(extra, fmt.Sprintf("INSERT INTO _groupChat VALUES ('%s', 'Team %d', '{}')", id, i),
			fmt.Sprintf("INSERT INTO _chat VALUES ('%s', 2, %d)", id, 10000+i))
	}
	r := testReader(t, extra...)
	_, err := r.ResolveChatInfo("team", false)
	le, _ := AsError(err)
	if le == nil || le.Code != ErrChatAmbiguous {
		t.Fatalf("want CHAT_AMBIGUOUS, got %v", err)
	}
	if len(le.Candidates) != maxCandidates || !strings.Contains(le.Message, "25 chats match") {
		t.Errorf("candidates %d, message %q", len(le.Candidates), le.Message)
	}
	if le.Candidates[0].Name != "Team 24" {
		t.Errorf("most recent first: got %q", le.Candidates[0].Name)
	}
	for _, c := range le.Candidates {
		if c.LastMessageAt == "" || c.MemberCount != 0 || c.Muted {
			t.Errorf("candidate shape changed: %+v", c)
		}
	}
	if _, ok := le.CandidateList().([]ChatSummary); !ok {
		t.Errorf("CandidateList is %T", le.CandidateList())
	}
}

func TestResolveSenders(t *testing.T) {
	r := testReader(t)
	for _, c := range []struct {
		from string
		want []string
		code ErrorCode
	}{
		{"me", []string{me, myMember}, ""},
		{"Me Myself", []string{me, myMember}, ""}, // your own name means all your ids
		{bot, []string{bot}, ""},                  // only seen in _message._from
		{noChat, []string{noChat}, ""},            // contact without messages
		{alice + "," + bot, []string{alice, bot}, ""},
		{alice + "," + fid("u", 999), nil, ErrSenderNotFound},
		{fid("u", 999), nil, ErrSenderNotFound},
		{"Ali", []string{alice}, ""},        // nickname
		{"Alice Wong", []string{alice}, ""}, // own name
		{"Anna", []string{anna}, ""},        // exact beats substring
		{"anna", []string{anna}, ""},        // case-insensitive tier
		{"Ann", []string{ann}, ""},          // exact, although ANN folds to it
		{"ann", nil, ErrSenderAmbiguous},    // Ann and ANN
		{"wong", []string{alice}, ""},       // substring, one person
		{"A", nil, ErrSenderAmbiguous},
		{"nobody", nil, ErrSenderNotFound},
		{" ", nil, ErrInvalidArgument},
	} {
		got, err := r.senderMids(c.from)
		le, _ := AsError(err)
		slices.Sort(got)
		want := slices.Clone(c.want)
		slices.Sort(want)
		switch {
		case c.code != "" && (le == nil || le.Code != c.code):
			t.Errorf("%q: err %v, want %s", c.from, err, c.code)
		case c.code == "" && (err != nil || !slices.Equal(got, want)):
			t.Errorf("%q: got %v err %v, want %v", c.from, got, err, want)
		}
	}
}

func TestSenderAmbiguousCandidates(t *testing.T) {
	r := testReader(t)
	_, err := r.senderMids("an")
	le, _ := AsError(err)
	if le == nil || le.Code != ErrSenderAmbiguous || !strings.HasPrefix(le.Message, "3 people match") {
		t.Fatalf("got %v", err)
	}
	if le.Candidates != nil || len(le.Senders) != 3 {
		t.Fatalf("senders %d, chats %d", len(le.Senders), len(le.Candidates))
	}
	first := le.Senders[0]
	if first.ID != anna || first.Source != "open_chat_member" || first.Community != "Hiking Club" || first.LastChatID != open || first.ChatID != "" || first.LastSentAt == "" {
		t.Errorf("most recent sender first with source and community: %+v", first)
	}
	sources := map[string]string{}
	for _, s := range le.Senders {
		sources[s.ID] = s.Source
		if s.ID == ann && (s.ChatID != ann || s.LastChatID != orphan) {
			t.Errorf("chat_id is the 1:1 chat, last_chat_id where they last posted: %+v", s)
		}
	}
	if sources[ann] != "contact" || sources[annUpper] != "open_chat_member" {
		t.Errorf("sources: %v", sources)
	}
	if _, ok := le.CandidateList().([]SenderCandidate); !ok {
		t.Errorf("CandidateList is %T", le.CandidateList())
	}
	if (&Error{Code: ErrChatNotFound}).CandidateList() != nil {
		t.Error("CandidateList without candidates is not nil")
	}
}

func TestContacts(t *testing.T) {
	r := testReader(t)
	list := func(opt ContactsOptions) *ContactList {
		t.Helper()
		res, err := r.ListContacts(opt)
		if err != nil || res.Contacts == nil {
			t.Fatalf("%+v: %v %v", opt, res, err)
		}
		return res
	}
	all := list(ContactsOptions{Limit: 5000}).Contacts
	if len(all) != 6 || !all[0].IsMe || all[0].Source != "me" || all[0].ChatID != me {
		t.Fatalf("got %d rows, first %+v", len(all), all[0])
	}
	by := map[string]ContactSummary{}
	for _, c := range all {
		if c.ContactID == "" {
			t.Error("row with empty contact_id")
		}
		by[c.ContactID] = c
	}
	// display_name is the name chats and senders show: your nickname first.
	if c := by[alice]; c.DisplayName != "Ali" || c.ProfileName != "Alice Wong" || c.Nickname != "Ali" || c.Type != "friend" || c.ChatID != alice || c.Source != "contact" {
		t.Errorf("alice: %+v", c)
	}
	if c := chatByID(t, r)[alice]; c.Name != by[alice].DisplayName {
		t.Errorf("chat name %q, contacts display_name %q", c.Name, by[alice].DisplayName)
	}
	if c := by[oa]; c.Type != "official" || c.Nickname != "" || c.ProfileName != "" {
		t.Errorf("oa: %+v", c)
	}
	if by[ann].Type != "other" || by[noChat].Type != "friend" || by[noChat].ChatID != "" {
		t.Errorf("ann %+v, noChat %+v", by[ann], by[noChat])
	}
	if all[1].ContactID != alice || all[len(all)-1].ContactID != noChat {
		t.Errorf("order: most recently active 1:1 first, contacts without a chat last")
	}
	if got := list(ContactsOptions{Limit: 2}).Contacts; len(got) != 2 {
		t.Errorf("limit 2 gave %d", len(got))
	}
	for _, q := range []string{"ali", "wong"} { // nickname, own name
		if got := list(ContactsOptions{Query: q}).Contacts; len(got) != 1 || got[0].ContactID != alice {
			t.Errorf("query %s: %v", q, got)
		}
	}
	res := list(ContactsOptions{Query: "ann"})
	if len(res.Contacts) != 1 || res.Contacts[0].ContactID != ann || res.MoreMembers != 2 {
		t.Errorf("query ann without --include-members: %v, %d members left out", res.Contacts, res.MoreMembers)
	}
	if res := list(ContactsOptions{Query: "anna", IncludeMembers: true}); len(res.Contacts) != 1 || res.MoreMembers != 0 ||
		res.Contacts[0].Source != "open_chat_member" || res.Contacts[0].Community != "Hiking Club" || res.Contacts[0].Type != "" {
		t.Errorf("--include-members anna: %+v", res)
	}
	if got := list(ContactsOptions{Query: myMember}).Contacts; len(got) != 1 || !got[0].IsMe || got[0].Source != "me" {
		t.Errorf("exact own member id: %v", got)
	}
	if got := list(ContactsOptions{Query: anna}).Contacts; len(got) != 1 || got[0].Source != "open_chat_member" {
		t.Errorf("exact member id: %v", got)
	}
	if got := list(ContactsOptions{Query: me}).Contacts; len(got) != 1 || !got[0].IsMe {
		t.Errorf("exact own id: %v", got)
	}
	if res := list(ContactsOptions{Query: "zzz"}); len(res.Contacts) != 0 || res.MoreMembers != 0 {
		t.Errorf("no match: %+v", res)
	}
	if got, err := r.Contacts(""); err != nil || len(got) != 6 {
		t.Errorf("Contacts(query): %d rows, %v", len(got), err)
	}
}

func TestContactsIncludeMembers(t *testing.T) {
	r := testReader(t)
	res, err := r.ListContacts(ContactsOptions{IncludeMembers: true, Limit: 5000})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range res.Contacts {
		ids = append(ids, c.ContactID)
	}
	// You, contacts with a 1:1 chat, then everyone else by their latest message.
	want := []string{me, alice, oa8, oa, ann, myMember, anna, noChat, annUpper}
	if !slices.Equal(ids, want) || res.MoreMembers != 0 {
		t.Errorf("order %v, want %v (%d more)", ids, want, res.MoreMembers)
	}
	res, _ = r.ListContacts(ContactsOptions{IncludeMembers: true, Limit: 6})
	if len(res.Contacts) != 6 || res.Contacts[5].ContactID != myMember || res.MoreMembers != 2 {
		t.Errorf("limit 6: %d rows, %d members cut", len(res.Contacts), res.MoreMembers)
	}
}

func TestMissingOptionalIdentityColumns(t *testing.T) {
	r := testReader(t,
		"DROP TABLE _contact",
		"CREATE TABLE _contact (_mid TEXT, _displayName TEXT, _displayNameOverridden TEXT, _type INTEGER)",
		fmt.Sprintf("INSERT INTO _contact VALUES ('%s', 'Alice Wong', 'Ali', 0), ('%s', 'Shop', '', 0), ('%s', 'News', '', 8)", alice, oa, oa8),
		"DROP TABLE _chat",
		"CREATE TABLE _chat (_id TEXT PRIMARY KEY, _lastUpdatedTime INTEGER)",
		fmt.Sprintf("INSERT INTO _chat VALUES ('%s', 1), ('%s', 2), ('%s', 3), ('%s', 4), ('%s', 5)", alice, oa, oa8, orphan, thread),
	)
	chats := chatByID(t, r)
	for id, want := range map[string][2]string{
		alice: {"personal", "Ali"}, oa: {"official", "Shop"}, oa8: {"official", "News"},
		orphan: {"group", orphan}, thread: {"thread", "Hiking Talk (thread)"},
	} {
		if c := chats[id]; c.Type != want[0] || c.Name != want[1] {
			t.Errorf("%s: got (%s, %s), want %v", id, c.Type, c.Name, want)
		}
	}
	res, err := r.ListContacts(ContactsOptions{})
	if err != nil || len(res.Contacts) != 4 {
		t.Fatalf("contacts without _status: %v %v", res, err)
	}
	types := map[string]string{}
	for _, c := range res.Contacts {
		types[c.ContactID] = c.Type
	}
	if types[alice] != "other" || types[oa] != "official" || types[oa8] != "official" {
		t.Errorf("types without _status and _capableBuddy: %v", types)
	}
}

func TestResolvePersonAndSelfNames(t *testing.T) {
	band := fid("c", 40)
	r := testReader(t,
		fmt.Sprintf("INSERT INTO _groupChat VALUES ('%s', 'Zed and Friends', '{}')", band),
		fmt.Sprintf("INSERT INTO _chat VALUES ('%s', 2, 100)", band))
	// People without a 1:1 chat: a chat that merely contains the name is only a
	// candidate, never read silently.
	for arg, code := range map[string]ErrorCode{"Zed": ErrChatAmbiguous, "zed": ErrChatAmbiguous, "ANNA": ErrChatNotFound} {
		_, err := r.ResolveChatInfo(arg, false)
		le, _ := AsError(err)
		if le == nil || le.Code != code || !strings.Contains(le.Message, "search --from") {
			t.Errorf("%q: %v", arg, err)
		} else if code == ErrChatAmbiguous && (len(le.Candidates) != 1 || le.Candidates[0].ChatID != band) {
			t.Errorf("%q: candidates %+v", arg, le.Candidates)
		}
	}
	if c, err := r.ResolveChatInfo("Zed and", false); err != nil || c.ChatID != band {
		t.Errorf("substring that is no person's name: %v %v", c.ChatID, err)
	}
	if c, err := r.ResolveChatInfo("Me Myself", true); err != nil || c.ChatID != me || c.Type != "self" {
		t.Errorf("own name resolves to the self chat: %v %v", c, err)
	}

	same := fid("c", 41)
	r = testReader(t,
		fmt.Sprintf("INSERT INTO _groupChat VALUES ('%s', 'Me Myself', '{}')", same),
		fmt.Sprintf("INSERT INTO _chat VALUES ('%s', 2, 100)", same))
	if c, err := r.ResolveChatInfo("Me Myself", false); err != nil || c.ChatID != same {
		t.Errorf("group named like you: %v %v", c.ChatID, err)
	}
	// send (exactOnly) never prefers another chat over your own: both are candidates.
	if _, err := r.ResolveChatInfo("Me Myself", true); err == nil {
		t.Error("exact: group named like you resolved silently")
	}
}

func TestMembers(t *testing.T) {
	r := testReader(t)
	get := func(arg string) (*MemberList, map[string]Member) {
		t.Helper()
		res, err := r.Members(MembersOptions{ChatArg: arg})
		if err != nil {
			t.Fatalf("%s: %v", arg, err)
		}
		by := map[string]Member{}
		for _, m := range res.Members {
			by[m.ID] = m
		}
		return res, by
	}
	res, by := get("Family")
	if res.Chat.ChatID != group || len(res.Members) != 4 || res.Note != "" {
		t.Fatalf("group: chat %s, %d members, note %q", res.Chat.ChatID, len(res.Members), res.Note)
	}
	if m := by[alice]; m.Role != "creator" || m.State != "joined" || m.MessageCount != 2 || m.JoinedAt == "" || m.ChatID != alice {
		t.Errorf("alice: %+v", m)
	}
	if m := by[ann]; m.MessageCount != 1 {
		t.Errorf("events and notices counted as messages: %+v", m)
	}
	if m := by[noChat]; !m.Invited || m.State != "invited" || m.Name != "Zed" {
		t.Errorf("invitee: %+v", m)
	}
	if !by[me].IsMe || res.Members[len(res.Members)-1].ID != noChat {
		t.Errorf("me %+v; invitees sort last", by[me])
	}

	res, by = get(open)
	if len(res.Members) != 3 || !strings.Contains(res.Note, "not a complete member list") {
		t.Fatalf("open: %d members, note %q", len(res.Members), res.Note)
	}
	if m := by[anna]; m.Role != "admin" || m.State != "joined" || m.MessageCount != 1 {
		t.Errorf("anna: %+v", m)
	}
	if m := by[annUpper]; m.State != "left" || res.Members[2].ID != annUpper {
		t.Errorf("left member should sort last: %+v", m)
	}
	if res, _ := get(thread); len(res.Members) != 3 || res.Chat.Type != "thread" {
		t.Errorf("thread uses its community: %d", len(res.Members))
	}

	res, by = get(orphan)
	if len(res.Members) != 1 || by[ann].MessageCount != 1 || !strings.Contains(res.Note, "sent messages") {
		t.Errorf("orphan group falls back to senders: %+v note %q", res.Members, res.Note)
	}
	res, by = get(stranger)
	if len(res.Members) != 2 || !by[me].IsMe || by[stranger].Name != "Snap" {
		t.Errorf("1:1 without contact uses the stored sender name: %+v", res.Members)
	}
	if res, _ := get(me); len(res.Members) != 1 || res.Chat.Type != "self" {
		t.Errorf("self chat: %+v", res.Members)
	}
	if res, _ := r.Members(MembersOptions{ChatArg: "Family", Limit: 2}); len(res.Members) != 2 || res.Total != 4 {
		t.Errorf("limit: %d of %d", len(res.Members), res.Total)
	}
	if _, err := r.Members(MembersOptions{ChatArg: "nothing here"}); err == nil {
		t.Error("unknown chat resolved")
	}
}
