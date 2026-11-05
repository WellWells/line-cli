package line

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Synthetic rows only: fake mids, ids and texts.

const (
	midA = "u00000000000000000000000000000001"
	midB = "u00000000000000000000000000000002"
	midC = "p00000000000000000000000000000003"
)

func testNames(mid string) string {
	return map[string]string{midA: "Alice", midB: "Bob"}[mid]
}

func parse(r rawMessage) Message {
	if r.RelationType == 0 && r.RelatedID == "" {
		r.RelationType = -1
	}
	sender := testNames(r.From)
	if sender == "" {
		sender = r.From
	}
	return parseMessage(r, sender, false, testNames)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseUnsentHasNoContent(t *testing.T) {
	for ct, kind := range map[int64]string{0: "text", 1: "image", 7: "sticker", 14: "file"} {
		m := parse(rawMessage{From: midA, ContentType: ct, Type: 3, Attribute: 1, Meta: `{"UNSENT":true,"FILE_NAME":"x.pdf","FILE_SIZE":"10"}`, EventInfo: `{"type":20}`})
		if m.Type != "unsent" || m.OriginalType != kind || m.Content != nil || m.Filename != "" || m.FileSize != nil || m.Event != "" {
			t.Errorf("ct %d: %s", ct, mustJSON(t, m))
		}
	}
}

func TestParseOpenChatEvents(t *testing.T) {
	for _, c := range []struct {
		info    string
		event   string
		targets []Person
		content string
	}{
		{`{"type":14,"fromId":"` + midA + `","midList":[],"displayNameMap":{},"param":""}`, "joined", nil, ""},
		{`{"type":13,"fromId":"` + midA + `","midList":[],"displayNameMap":{},"param":""}`, "left", nil, ""},
		{`{"type":15,"fromId":"` + midA + `","midList":["` + midC + `"],"displayNameMap":{"` + midC + `":"Carol"},"param":""}`, "kicked", []Person{{ID: midC, Name: "Carol"}}, ""},
		{`{"type":15,"midList":["` + midB + `"],"displayNameMap":{}}`, "kicked", []Person{{ID: midB, Name: "Bob"}}, ""},
		{`{"type":18,"fromId":"","midList":["` + midA + `"],"displayNameMap":{},"param":"New name"}`, "renamed", []Person{{ID: midA, Name: "Alice"}}, "New name"},
		{`{"type":99}`, "type_99", nil, ""},
		{``, "unknown", nil, ""},
	} {
		m := parse(rawMessage{From: midA, ContentType: 0, Type: 3, EventInfo: c.info})
		content := ""
		if m.Content != nil {
			content = *m.Content
		}
		if m.Type != "event" || m.Event != c.event || !reflect.DeepEqual(m.Targets, c.targets) || content != c.content {
			t.Errorf("%s: %s", c.info, mustJSON(t, m))
		}
	}
}

func TestParseNotices(t *testing.T) {
	for _, c := range []struct {
		key, args string
		event     string
		targets   []Person
		content   string
	}{
		{"C_MJ", midA, "joined", nil, ""},
		{"C_ML", midA, "left", nil, ""},
		{"C_MI", midA + "\x1e" + midB + "\x1f" + midC, "invited", []Person{{ID: midB, Name: "Bob"}, {ID: midC}}, ""},
		{"C_GI", midA + "\x1e" + midB, "invited", []Person{{ID: midB, Name: "Bob"}}, ""},
		{"C_MR", midA + "\x1e" + midB, "removed", []Person{{ID: midB, Name: "Bob"}}, ""},
		{"C_PN", midA + "\x1eNew group name", "renamed", nil, "New group name"},
		{"C_PI", midA, "photo_changed", nil, ""},
		// Auto groups (chats created with several people at once) have A_ keys.
		{"A_MC", midA + "\x1e" + midB + "\x1f" + midC, "invited", []Person{{ID: midB, Name: "Bob"}, {ID: midC}}, ""},
		{"A_MI", midA + "\x1e" + midB, "invited", []Person{{ID: midB, Name: "Bob"}}, ""},
		{"A_MJ", midA, "joined", nil, ""},
		{"A_ML", midA, "left", nil, ""},
		{"A_MR", midA + "\x1e" + midB, "removed", []Person{{ID: midB, Name: "Bob"}}, ""},
		{"C_IC", midA + "\x1e" + midB, "invite_canceled", []Person{{ID: midB, Name: "Bob"}}, ""},
		{"A_IC", midA + "\x1e" + midB, "invite_canceled", []Person{{ID: midB, Name: "Bob"}}, ""},
		{"C_SN", midA, "join_by_link_on", nil, ""},
		{"C_SP", midA, "join_by_link_off", nil, ""},
		{"C_BG", midA, "became_group", nil, ""},
		{"C_OL", midA + "\x1e" + midB, "invite_failed", []Person{{ID: midB, Name: "Bob"}}, ""},
		{"BB", "Old album\x1eNew album", "album_renamed", nil, ""},
		{"BD", midA, "album_deleted", nil, ""},
		{"BO", midA, "album_photo_deleted", nil, ""},
		// A key line-cli does not know stays LINE's raw code.
		{"C_MA", midA, "C_MA", nil, ""},
		{"", "", "notice", nil, ""},
	} {
		meta := mustJSON(t, map[string]string{"LOC_KEY": c.key, "LOC_ARGS": c.args})
		m := parse(rawMessage{From: midA, ContentType: 18, Type: 1, Meta: meta})
		content := ""
		if m.Content != nil {
			content = *m.Content
		}
		if m.Type != "event" || m.Event != c.event || !reflect.DeepEqual(m.Targets, c.targets) || content != c.content {
			t.Errorf("%s: %s", c.key, mustJSON(t, m))
		}
	}
}

func TestParseTextContentAndUndecryptable(t *testing.T) {
	m := parse(rawMessage{From: midA, ContentType: 0, Type: 1, Text: ""})
	if m.Content != nil || m.Undecryptable {
		t.Errorf("empty text: %s", mustJSON(t, m))
	}
	if s := mustJSON(t, m); s != `{"type":"text","sender":"Alice","sender_id":"`+midA+`"}` {
		t.Errorf("empty text JSON: %s", s)
	}
	m = parse(rawMessage{From: midA, ContentType: 0, Type: 1, HasChunks: true})
	if m.Content != nil || !m.Undecryptable || m.Type != "text" {
		t.Errorf("undecryptable: %s", mustJSON(t, m))
	}
	m = parse(rawMessage{From: midA, ContentType: 0, Type: 1, Text: "hi http://example.invalid/x", HasChunks: true})
	// A link stays in content only; no urls field repeats it.
	if m.Content == nil || m.Undecryptable || strings.Contains(mustJSON(t, m), `"urls"`) {
		t.Errorf("decrypted text: %s", mustJSON(t, m))
	}
}

func TestParseSenderFallsBackToFromName(t *testing.T) {
	const unknown = "v00000000000000000000000000000009"
	m := parse(rawMessage{From: unknown, ContentType: 0, Type: 1, Text: "x", Info: `{"fromName":"Bot"}`})
	if m.Sender != "Bot" || m.SenderID != unknown {
		t.Errorf("fromName: %s", mustJSON(t, m))
	}
	m = parse(rawMessage{From: midA, ContentType: 0, Type: 1, Text: "x", Info: `{"fromName":"Old name"}`})
	if m.Sender != "Alice" {
		t.Errorf("known name must win: %s", mustJSON(t, m))
	}
	m = parse(rawMessage{From: unknown, ContentType: 0, Type: 1, Text: "x"})
	if m.Sender != unknown {
		t.Errorf("no name: %s", mustJSON(t, m))
	}
}

func TestParseEventSenderFromDisplayNameMap(t *testing.T) {
	const gone = "p00000000000000000000000000000009"
	for _, typ := range []int{13, 14} {
		info := `{"type":` + mustJSON(t, typ) + `,"midList":[],"displayNameMap":{"` + gone + `":"Dave"}}`
		m := parse(rawMessage{From: gone, ContentType: 0, Type: 3, EventInfo: info})
		if m.Type != "event" || m.Sender != "Dave" || m.SenderID != gone || len(m.Targets) != 0 {
			t.Errorf("type %d: %s", typ, mustJSON(t, m))
		}
	}
	m := parse(rawMessage{From: midA, ContentType: 0, Type: 3, EventInfo: `{"type":14,"displayNameMap":{"` + midA + `":"Old"}}`})
	if m.Sender != "Alice" {
		t.Errorf("known name must win: %s", mustJSON(t, m))
	}
	m = parse(rawMessage{From: gone, ContentType: 0, Type: 3, EventInfo: `{"type":14,"fromName":"Eve","displayNameMap":{"` + gone + `":"Dave"}}`})
	if m.Sender != "Eve" {
		t.Errorf("fromName first: %s", mustJSON(t, m))
	}
}

func TestParseRelations(t *testing.T) {
	m := parse(rawMessage{From: midA, ContentType: 0, Type: 1, Text: "x", RelatedID: "100", RelationType: 3})
	if m.ReplyTo != "100" || m.Forwarded {
		t.Errorf("reply: %s", mustJSON(t, m))
	}
	m = parse(rawMessage{From: midA, ContentType: 0, Type: 1, Text: "x", RelatedID: "100", RelationType: 0})
	if m.ReplyTo != "" || !m.Forwarded {
		t.Errorf("forward: %s", mustJSON(t, m))
	}
	m = parse(rawMessage{From: midA, ContentType: 0, Type: 1, Text: "x", RelationType: 0})
	if m.ReplyTo != "" || m.Forwarded {
		t.Errorf("relation 0 without id is not a forward: %s", mustJSON(t, m))
	}
}

func TestParseMentions(t *testing.T) {
	// "😀 @Bob @Zed @All hi": the emoji is two UTF-16 units, so offsets are UTF-16.
	text := "😀 @Bob @Zed @All hi"
	mention := `{"MENTIONEES":[{"S":"3","E":"7","M":"` + midB + `"},{"S":"8","E":"12","M":"` + midC + `"},{"S":"13","E":"17","A":"1"},{"S":"3","E":"7","M":"` + midB + `"}]}`
	meta := mustJSON(t, map[string]string{"MENTION": mention})
	m := parse(rawMessage{From: midA, ContentType: 0, Type: 1, Text: text, Meta: meta})
	want := []Person{{ID: midB, Name: "Bob"}, {ID: midC, Name: "Zed"}}
	if !reflect.DeepEqual(m.Mentions, want) || !m.MentionsAll {
		t.Errorf("mentions: %s", mustJSON(t, m))
	}
	m = parse(rawMessage{From: midA, ContentType: 0, Type: 1, Text: "x", Meta: `{"MENTION":"not json"}`})
	if m.Mentions != nil || m.MentionsAll {
		t.Errorf("bad mention: %s", mustJSON(t, m))
	}
}

func TestParseReactions(t *testing.T) {
	for _, c := range []struct {
		status, legacy string
		want           *Reactions
	}{
		{`{"countByType":{"2":3,"4":1,"6":0,"9":2},"totalCount":6}`, "", &Reactions{Total: 6, ByType: map[string]int64{"nice": 3, "fun": 1, "type_9": 2}}},
		{`{"countByType":{},"totalCount":0}`, "", nil},
		{`{"reactionList":[{"reactor":"` + midA + `","type":1,"PredefinedType":3},{"reactor":"` + midB + `","type":2,"PredefinedType":0,"PaidType":{"productId":"x"}},{"type":1,"PredefinedType":7,"cancel":1},{"type":1,"PredefinedType":5,"cancel":0}],"totalCount":0}`, "",
			&Reactions{Total: 3, ByType: map[string]int64{"love": 1, "paid": 1, "amazing": 1}}},
		{"", `{"_lkp|_001":{"fromUserMid":"` + midA + `","reactionType":{"predefinedReactionType":2}},"_lkp|_002":{"reactionType":{"paidReactionType":{"productId":"x"}}}}`,
			&Reactions{Total: 2, ByType: map[string]int64{"nice": 1, "paid": 1}}},
		{"garbage", "", nil},
	} {
		got := parseReactions(c.status, c.legacy)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q %q: got %s", c.status, c.legacy, mustJSON(t, got))
		}
	}
}

func TestParseTypeDetails(t *testing.T) {
	m := parse(rawMessage{From: midA, ContentType: 15, Type: 1, Text: "Place", Location: `{"title":"Place","address":"1 Road","latitude":25.5,"longitude":121.25,"categoryId":"x"}`})
	if m.Type != "location" || m.Location == nil || *m.Location != (GeoLocation{Latitude: 25.5, Longitude: 121.25, Address: "1 Road", Title: "Place"}) {
		t.Errorf("location: %s", mustJSON(t, m))
	}
	m = parse(rawMessage{From: midA, ContentType: 6, Type: 1, Text: "Ca internal Cause: 16", Meta: `{"TYPE":"V","RESULT":"NORMAL","DURATION":"61500","CAUSE":"16"}`})
	if m.Content == nil || *m.Content != "Ca internal Cause: 16" || m.Call == nil || *m.Call != (Call{Kind: "video", Result: "normal", DurationSec: 61.5}) {
		t.Errorf("call keeps its content and adds call: %s", mustJSON(t, m))
	}
	m = parse(rawMessage{From: midA, ContentType: 6, Type: 1, Meta: `{"TYPE":"G","RESULT":"INFO","GC_EVT_TYPE":"S","DURATION":"0"}`})
	if m.Call == nil || *m.Call != (Call{Kind: "group", Result: "started"}) {
		t.Errorf("group call: %s", mustJSON(t, m))
	}
	m = parse(rawMessage{From: midA, ContentType: 0, Type: 1, Text: "see https://example.invalid", URLPreview: `{"urlPreview":[{"url":"https://example.invalid","title":"T","desc":"D"},{"url":"https://example.invalid/2","title":"","desc":""}]}`})
	if !reflect.DeepEqual(m.LinkPreviews, []LinkPreview{{URL: "https://example.invalid", Title: "T", Description: "D"}}) {
		t.Errorf("preview: %s", mustJSON(t, m))
	}
	m = parse(rawMessage{From: midA, ContentType: 1, Type: 1, Meta: `{"GID":"42","GSEQ":"2","GTOTAL":"3"}`})
	if m.Album == nil || *m.Album != (Album{ID: "42", Index: 2, Total: 3}) || m.Content != nil {
		t.Errorf("album: %s", mustJSON(t, m))
	}
	m = parse(rawMessage{From: midA, ContentType: 2, Type: 1, Meta: `{"DURATION":"15842"}`})
	if m.DurationSec != 15.842 {
		t.Errorf("video duration: %s", mustJSON(t, m))
	}
	m = parse(rawMessage{From: midA, ContentType: 3, Type: 1, Info: `{"duration":2000}`})
	if m.DurationSec != 2 {
		t.Errorf("audio duration: %s", mustJSON(t, m))
	}
	m = parse(rawMessage{From: midA, ContentType: 0, Type: 1, Text: "fixed", Meta: `{"EDITED":"true","UPDATED_TIME":"1"}`})
	if !m.Edited {
		t.Errorf("edited: %s", mustJSON(t, m))
	}
	m = parse(rawMessage{From: midA, ContentType: 16, Type: 1, Meta: `{"albumName":"Trip","postEndUrl":"line://x","serviceType":"AB"}`})
	if m.Type != "post" || m.Title != "Trip" || m.URL != "line://x" {
		t.Errorf("post: %s", mustJSON(t, m))
	}
	m = parse(rawMessage{From: midA, ContentType: 99, Type: 1, Text: "x"})
	if m.Type != "type_99" || m.Content == nil {
		t.Errorf("unknown type: %s", mustJSON(t, m))
	}
}

// File names come from the same three sources find-file uses.
func TestParseFileNameSources(t *testing.T) {
	for _, c := range []struct {
		name          string
		r             rawMessage
		file, content string
	}{
		{"contentInfo only", rawMessage{Info: `{"fileName":"a.pdf"}`, Meta: `{"FILE_SIZE":"5"}`}, "a.pdf", ""},
		{"text equals name", rawMessage{Text: "b.docx", Info: `{"fileName":"b.docx"}`}, "b.docx", ""},
		{"metadata", rawMessage{Meta: `{"FILE_NAME":"c.xlsx","FILE_SIZE":"7"}`, Text: "c.xlsx"}, "c.xlsx", ""},
		{"text only", rawMessage{Text: "d.txt"}, "d.txt", ""},
		{"caption", rawMessage{Text: "see this", Meta: `{"FILE_NAME":"e.zip"}`}, "e.zip", "see this"},
	} {
		c.r.From, c.r.ContentType, c.r.Type = midA, 14, 1
		m := parse(c.r)
		content := ""
		if m.Content != nil {
			content = *m.Content
		}
		if m.Type != "file" || m.Filename != c.file || content != c.content || m.Filename != fileNameOf(c.r.Info, c.r.Meta, c.r.Text) {
			t.Errorf("%s: %s", c.name, mustJSON(t, m))
		}
	}
}

func TestContentTypeNamesCoverOutputs(t *testing.T) {
	names := map[string]bool{}
	for _, n := range ContentTypeNames() {
		names[n] = true
	}
	for _, n := range []string{"text", "image", "video", "audio", "call", "sticker", "contact", "file", "location", "post", "template", "flex", "event", "unsent"} {
		if !names[n] {
			t.Errorf("missing %s", n)
		}
	}
	if names["link"] || names["notice"] {
		t.Error("link and notice are filters, not output types")
	}
}
