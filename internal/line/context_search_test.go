package line

import (
	"reflect"
	"sort"
	"testing"
)

const (
	chatX = "c0000000000000000000000000000000a"
	chatY = "c0000000000000000000000000000000b"
)

type testRow struct {
	id, chat          string
	at, ct, typ, attr int64
	text, meta, info  string
	event, preview    string
	location          string
}

func newTestReader(t *testing.T, rows []testRow) *Reader {
	t.Helper()
	db, err := OpenSynthetic("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	r := NewReader(db)
	exec := func(sql string, args ...any) {
		t.Helper()
		st, err := r.query(sql, args...)
		if err != nil || st == nil {
			t.Fatalf("%s: %v", sql, err)
		}
		st.Step()
		if err := st.Err(); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		st.Close()
	}
	for _, c := range []string{chatX, chatY} {
		exec("INSERT INTO _chat (_id, _midType, _lastUpdatedTime, _unreadCount, _status) VALUES (?, 2, 1, 0, 0)", c)
	}
	exec("INSERT INTO _groupChat (_type, _chatMid, _chatName, _status) VALUES (0, ?, 'Group X', 0), (0, ?, 'Group Y', 0)", chatX, chatY)
	exec("INSERT INTO _contact (_mid, _type, _status, _displayName, _displayNameOverridden) VALUES (?, 0, 1, 'Alice', ''), (?, 0, 1, 'Bob', '')", midA, midB)
	exec("INSERT INTO _profile (_mid, _displayName) VALUES (?, 'Me')", midB)
	for _, m := range rows {
		if m.chat == "" {
			m.chat = chatX
		}
		if m.typ == 0 {
			m.typ = 1
		}
		exec(`INSERT INTO _message (_id, _chatId, _from, _toType, _createdTime, _text, _contentType, _type, _status, _attribute,
			_contentMetadata, _contentInfo, _eventInfo, _urlPreview, _location) VALUES (?,?,?,2,?,?,?,?,1,?,?,?,?,?,?)`,
			m.id, m.chat, midA, m.at, m.text, m.ct, m.typ, m.attr, m.meta, m.info, m.event, m.preview, m.location)
	}
	return r
}

func hitIDs(t *testing.T, r *Reader, opt SearchOptions) []string {
	t.Helper()
	if opt.Limit == 0 {
		opt.Limit = 100
	}
	hits, err := r.Search(opt)
	if err != nil {
		t.Fatalf("%+v: %v", opt, err)
	}
	out := []string{}
	for _, h := range hits {
		out = append(out, h.ID)
	}
	sort.Strings(out)
	return out
}

var searchRows = []testRow{
	{id: "t1", at: 1000, text: "alpha beta gamma"},
	{id: "t2", at: 2000, text: "ＯＫ Café　ＧＯ"},
	{id: "t3", at: 3000, text: "see https://example.invalid/x", preview: `{"urlPreview":[{"url":"https://example.invalid/x","title":"Unique Headline","desc":"Body words"}]}`},
	{id: "f1", at: 4000, ct: 14, info: `{"fileName":"report.pdf"}`, meta: `{"FILE_SIZE":"12","FILE_EXPIRE_TIMESTAMP":"1"}`},
	{id: "x1", at: 5000, ct: 22, meta: `{"ALT_TEXT":"Hello card","FLEX_JSON":"{\"type\":\"bubble\",\"color\":\"#fff\"}"}`},
	{id: "u1", at: 6000, ct: 0, typ: 3, attr: 1, text: "secret taken back", meta: `{"UNSENT":true}`},
	{id: "e1", at: 7000, ct: 0, typ: 3, event: `{"type":14,"midList":[]}`},
	{id: "n1", at: 8000, ct: 18, meta: `{"LOC_KEY":"C_MJ","LOC_ARGS":"` + midA + `"}`},
	{id: "p1", at: 9000, ct: 16, meta: `{"albumName":"Trip photos","postEndUrl":"line://album"}`},
	{id: "l1", at: 10000, ct: 15, text: "Station", location: `{"address":"9 Harbor Road","latitude":1.5,"longitude":2.5}`},
	{id: "s1", at: 11000, ct: 7, text: "", meta: `{"STKTXT":"[Sticker]"}`},
	{id: "c1", at: 12000, ct: 6, text: "Ca internal size Cause: 16", meta: `{"TYPE":"A","RESULT":"NORMAL","DURATION":"1000"}`},
	{id: "t4", at: 13000, text: "plain words without link"},
	{id: "r1", at: 14000, ct: 18, meta: `{"LOC_KEY":"C_PN","LOC_ARGS":"` + midA + `\u001eRenamed Crew"}`},
	{id: "r2", at: 15000, ct: 0, typ: 3, event: `{"type":18,"midList":[],"param":"Night Owls"}`},
	{id: "j1", at: 16000, ct: 0, typ: 3, event: `{"type":14,"midList":[],"displayNameMap":{"` + midA + `":"Crew member"}}`},
}

func TestSearchMatchesVisibleTextOnly(t *testing.T) {
	r := newTestReader(t, searchRows)
	for _, c := range []struct {
		q    string
		want []string
	}{
		{"size", []string{"c1"}},         // call content is shown; FILE_SIZE key is not
		{"color", []string{}},            // FLEX_JSON structure
		{"bubble", []string{}},           // FLEX_JSON structure
		{"UNSENT", []string{}},           // metadata key
		{"secret", []string{}},           // text of an unsent message
		{"report", []string{"f1"}},       // file name in _contentInfo only
		{"hello", []string{"x1"}},        // bot alt text
		{"headline", []string{"t3"}},     // link preview title
		{"body", []string{"t3"}},         // link preview description
		{"trip", []string{"p1"}},         // post title
		{"harbor", []string{"l1"}},       // location address
		{"alpha gamma", []string{"t1"}},  // words are ANDed in any order
		{"gamma alpha", []string{"t1"}},  // order does not matter
		{`"alpha gamma"`, []string{}},    // a quoted phrase must be contiguous
		{`"alpha beta"`, []string{"t1"}}, // phrase
		{"alpha delta", []string{}},      // every word must match
		{"ok café", []string{"t2"}},      // full-width letters fold to ASCII
		{"CAFÉ", []string{"t2"}},         // non-ASCII case folding
		{`"ok go"`, []string{}},          // "ＯＫ" and "ＧＯ" are not adjacent
		{`"café go"`, []string{"t2"}},    // ideographic space folds to a space
		{"%", []string{}},                // no LIKE wildcards
		{"_", []string{}},                // no LIKE wildcards
		{"https", []string{"t3"}},        // URLs in text
		{"words", []string{"t3", "t4"}},  // text and preview description
		{"ＷＯＲＤＳ", []string{"t3", "t4"}},  // full-width query
		{"crew", []string{"r1"}},         // new name of a renamed notice, not a member name
		{"owls", []string{"r2"}},         // new name of a renamed open chat
		{"member", []string{}},           // displayNameMap of an event is not content
	} {
		if got := hitIDs(t, r, SearchOptions{Query: c.q}); !reflect.DeepEqual(got, c.want) {
			t.Errorf("search %q = %v, want %v", c.q, got, c.want)
		}
	}
	if _, err := r.Search(SearchOptions{Query: `""`}); err == nil {
		t.Error(`search "" should fail`)
	}
	if got := hitIDs(t, r, SearchOptions{Query: "owls", ContentType: "event"}); !reflect.DeepEqual(got, []string{"r2"}) {
		t.Errorf("text with --type event = %v", got)
	}
	_, err := r.Search(SearchOptions{Query: "secret", ContentType: "unsent"})
	if le, ok := AsError(err); !ok || le.Code != ErrInvalidArgument {
		t.Errorf("text with --type unsent: want INVALID_ARGUMENT, got %v", err)
	}
}

func TestSearchTypeFilters(t *testing.T) {
	r := newTestReader(t, searchRows)
	for _, c := range []struct {
		typ  string
		want []string
	}{
		{"text", []string{"t1", "t2", "t3", "t4"}},
		{"event", []string{"e1", "j1", "n1", "r1", "r2"}},
		{"notice", []string{"n1", "r1"}},
		{"type_18", []string{"n1", "r1"}},
		{"unsent", []string{"u1"}},
		{"url", []string{"t3"}},
		{"link", []string{"p1"}},
		{"post", []string{"p1"}},
		{"location", []string{"l1"}},
		{"type_15", []string{"l1"}},
		{"file", []string{"f1"}},
		{"flex", []string{"x1"}},
		{"call", []string{"c1"}},
		{"sticker", []string{"s1"}},
		{"image", []string{}},
	} {
		got := hitIDs(t, r, SearchOptions{ContentType: c.typ})
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("--type %s = %v, want %v", c.typ, got, c.want)
		}
		hits, _ := r.Search(SearchOptions{ContentType: c.typ, Limit: 100})
		for _, h := range hits {
			if want := map[string]string{"notice": "event", "type_18": "event", "url": "text", "link": "post", "type_15": "location"}[c.typ]; h.Type != c.typ && h.Type != want {
				t.Errorf("--type %s returned type %s", c.typ, h.Type)
			}
		}
	}
	for _, bad := range []string{"bogus", "type_x", "type_-1"} {
		if _, err := r.Search(SearchOptions{ContentType: bad}); err == nil {
			t.Errorf("--type %s should fail", bad)
		} else if le, ok := AsError(err); !ok || le.Code != ErrInvalidArgument {
			t.Errorf("--type %s: %v", bad, err)
		}
	}
}

func TestSearchTermsAndFold(t *testing.T) {
	for q, want := range map[string][]string{
		`a  b`:            {"a", "b"},
		`"a b" c`:         {"a b", "c"},
		`x"a b"`:          {"x", "a b"},
		`"unterminated b`: {"unterminated b"},
		"Ａ　Ｂ":             {"a", "b"},
		"“Curly Quote” Ｘ": {"curly quote", "x"},
		`""`:              nil,
	} {
		if got := searchTerms(q); !reflect.DeepEqual(got, want) {
			t.Errorf("searchTerms(%q) = %q, want %q", q, got, want)
		}
	}
	if got := foldString("ÀÉÎ ＡＢＣ！～ Straße"); got != "àéî abc!~ straße" {
		t.Errorf("fold = %q", got)
	}
}

func TestReadAround(t *testing.T) {
	var rows []testRow
	for i, at := range []int64{1000, 2000, 3000, 3000, 4000, 5000, 6000} {
		rows = append(rows, testRow{id: "m" + string(rune('1'+i)), at: at, text: "x"})
	}
	rows = append(rows, testRow{id: "other", chat: chatY, at: 3000, text: "y"})
	r := newTestReader(t, rows)
	ids := func(opt ReadOptions) []string {
		t.Helper()
		msgs, err := r.ReadHistory(opt)
		if err != nil {
			t.Fatalf("%+v: %v", opt, err)
		}
		out := []string{}
		for _, m := range msgs {
			out = append(out, m.ID)
		}
		return out
	}
	for _, c := range []struct {
		opt  ReadOptions
		want []string
	}{
		{ReadOptions{Around: "m4", Limit: 4}, []string{"m2", "m3", "m4", "m5"}},
		{ReadOptions{Around: "m4", Limit: 5}, []string{"m2", "m3", "m4", "m5", "m6"}},
		{ReadOptions{Around: "m3", Limit: 2}, []string{"m2", "m3"}},
		{ReadOptions{Around: "m4"}, []string{"m1", "m2", "m3", "m4", "m5", "m6", "m7"}},
		{ReadOptions{Around: "m1", Limit: 4}, []string{"m1", "m2"}},
		{ReadOptions{Around: "m7", Limit: 4}, []string{"m5", "m6", "m7"}},
		{ReadOptions{Around: "m4", Limit: 1}, []string{"m4"}},
		{ReadOptions{Around: "m4", Limit: 2, ChatArg: "Group X"}, []string{"m3", "m4"}},
		{ReadOptions{Around: "other", Limit: 3}, []string{"other"}},
	} {
		if got := ids(c.opt); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v = %v, want %v", c.opt, got, c.want)
		}
	}
	if (ReadOptions{Around: "m1"}).EffectiveLimit() != DefaultAroundLimit {
		t.Error("--around default limit")
	}
	for _, opt := range []ReadOptions{{Around: "nope"}, {Around: "m4", ChatArg: chatY}} {
		_, err := r.ReadHistory(opt)
		if le, ok := AsError(err); !ok || le.Code != ErrInvalidArgument {
			t.Errorf("%+v: want INVALID_ARGUMENT, got %v", opt, err)
		}
	}
	if _, err := r.ReadHistory(ReadOptions{Around: "m4", ChatArg: "no such chat"}); err == nil {
		t.Error("unknown chat with --around should fail")
	}
}

func TestReadMessageRowColumns(t *testing.T) {
	r := newTestReader(t, []testRow{
		{id: "r1", at: 1000, text: "hi @Bob", meta: `{"MENTION":"{\"MENTIONEES\":[{\"S\":\"3\",\"E\":\"7\",\"M\":\"` + midB + `\"}]}"}`},
	})
	st, err := r.query("UPDATE _message SET _relatedMessageId='r0', _messageRelationType=3, _reactionStatus=?, _chunks='' WHERE _id='r1'",
		`{"countByType":{"3":2},"totalCount":2}`)
	if err != nil || st == nil {
		t.Fatal(err)
	}
	st.Step()
	st.Close()
	msgs, err := r.ReadHistory(ReadOptions{ChatArg: chatX})
	if err != nil || len(msgs) != 1 {
		t.Fatalf("read: %v %d", err, len(msgs))
	}
	m := msgs[0]
	if m.SenderID != midA || m.Sender != "Alice" || m.ReplyTo != "r0" || len(m.Mentions) != 1 || m.Mentions[0].Name != "Bob" ||
		m.Reactions == nil || m.Reactions.ByType["love"] != 2 || m.SentAt == "" {
		t.Errorf("row: %s", mustJSON(t, m))
	}
}
