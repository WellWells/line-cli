package cli

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/WellWells/line-cli/internal/line"
)

func strp(s string) *string { return &s }

func TestMsgLineFixedColumns(t *testing.T) {
	for _, c := range []struct {
		m    line.Message
		want string
	}{
		{line.Message{ID: "1", Type: "text", Sender: "A\tB", SentAt: "T", Content: strp("two\nlines\r\nand\ttab"), FromMe: true},
			"T\t1\tA\\tB\tme\ttext\ttwo\\nlines\\r\\nand\\ttab"},
		{line.Message{ID: "2", Type: "image", Sender: "A", SentAt: "T"}, "T\t2\tA\t\timage\t"},
		{line.Message{ID: "3", Type: "unsent", OriginalType: "sticker", Sender: "A", SentAt: "T"}, "T\t3\tA\t\tunsent\t[unsent sticker]"},
		{line.Message{ID: "4", Type: "event", Event: "kicked", Targets: []line.Person{{ID: "u1", Name: "B"}, {ID: "u2"}}, Sender: "A", SentAt: "T"},
			"T\t4\tA\t\tevent\tkicked B, u2"},
		{line.Message{ID: "5", Type: "event", Event: "renamed", Content: strp("New"), Sender: "A", SentAt: "T"}, "T\t5\tA\t\tevent\trenamed: New"},
		{line.Message{ID: "6", Type: "call", Call: &line.Call{Kind: "audio", Result: "normal", DurationSec: 61.5}, Sender: "A", SentAt: "T"},
			"T\t6\tA\t\tcall\t[audio call normal 62s]"},
		{line.Message{ID: "7", Type: "file", Filename: "a.pdf", Sender: "A", SentAt: "T"}, "T\t7\tA\t\tfile\ta.pdf"},
		{line.Message{ID: "8", Type: "text", Undecryptable: true, Sender: "A", SentAt: "T"}, "T\t8\tA\t\ttext\t[undecryptable]"},
		{line.Message{ID: "9", Type: "location", Content: strp("Station"), Location: &line.GeoLocation{Latitude: 1.5, Longitude: 2.25, Address: "Road"}, Sender: "A", SentAt: "T"},
			"T\t9\tA\t\tlocation\tStation Road (1.5,2.25)"},
		{line.Message{ID: "10", Type: "post", Title: "Trip", URL: "line://x", Sender: "A", SentAt: "T"}, "T\t10\tA\t\tpost\tTrip line://x"},
	} {
		got := msgLine(c.m)
		if got != c.want {
			t.Errorf("msgLine = %q, want %q", got, c.want)
		}
		if n := len(strings.Split(got, "\t")); n != 6 || strings.ContainsAny(got, "\n\r") {
			t.Errorf("%q: %d columns", got, n)
		}
	}
}

func TestTextOutputOneLinePerRecord(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, err: &bytes.Buffer{}, text: true}
	rc.emitSearch([]line.SearchHit{{ChatID: "c1", ChatName: "N", Message: line.Message{ID: "1", Type: "text", Sender: "A", SentAt: "T", Content: strp("a\nb")}}})
	if got := out.String(); got != "c1\tT\t1\tA\t\ttext\ta\\nb\n" {
		t.Errorf("search text = %q", got)
	}
	out.Reset()
	rc.emitFiles([]line.FileHit{{ID: "9", SentAt: "T", ChatName: "N\nX", Filename: "f.pdf", Expired: true}})
	if got := out.String(); got != "T\t9\tN\\nX\tf.pdf EXPIRED\n" {
		t.Errorf("find-file text = %q", got)
	}
	out.Reset()
	rc.emitPreview([]line.PreviewResult{{ID: "5", SentAt: "T", Sender: "A", Type: "image", Status: "not_cached", Note: "n"}})
	if got := out.String(); got != "T\t5\tA\timage\tnot_cached\tn\n" {
		t.Errorf("preview text = %q", got)
	}
}

// unescapeText reverses escapeText.
func unescapeText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'x':
			n, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			b.WriteByte(byte(n))
			i += 2
		case 'u':
			n, _ := strconv.ParseUint(s[i+1:i+5], 16, 32)
			b.WriteRune(rune(n))
			i += 4
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func TestEscapeTextOneLineReversible(t *testing.T) {
	for _, s := range []string{
		"plain", "", `C:\path\n literal`, "a\nb\r\nc\td", "vt\vff\f", "fs\x1cgs\x1drs\x1eus\x1f", "nul\x00del\x7f",
		"nel\u0085ls\u2028ps\u2029", "全形\u3000空白", `\\n`, "\\\n",
	} {
		got := escapeText(s)
		if strings.ContainsFunc(got, func(r rune) bool { return r < 0x20 || r == 0x7f || r == 0x85 || r == 0x2028 || r == 0x2029 }) {
			t.Errorf("escapeText(%q) = %q keeps a control or line separator", s, got)
		}
		if back := unescapeText(got); back != s {
			t.Errorf("escapeText(%q) = %q, unescapes to %q", s, got, back)
		}
	}
	if escapeText(`a\nb`) == escapeText("a\nb") {
		t.Error("a literal backslash-n must differ from a newline")
	}
}

func TestUnreadTextColumns(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, err: &bytes.Buffer{}, text: true}
	msg := line.Message{ID: "1", Type: "text", Sender: "A", SentAt: "T", Content: strp("x\vy")}
	rc.emitUnread([]line.UnreadChat{
		{ChatID: "c1", Name: "N\tM", Type: "group", UnreadCount: 3, AvailableCount: 2, Messages: []line.Message{msg, msg}},
		{ChatID: "c2", Name: "P", Type: "personal", UnreadCount: 1},
	})
	want := "#\tc1\tgroup\tN\\tM\tunread=3\tavailable=2\n" +
		"c1\tT\t1\tA\t\ttext\tx\\x0by\n" +
		"c1\tT\t1\tA\t\ttext\tx\\x0by\n" +
		"#\tc2\tpersonal\tP\tunread=1\tavailable=0\n"
	if got := out.String(); got != want {
		t.Errorf("unread text =\n%q\nwant\n%q", got, want)
	}
}

func TestReadAroundArgErrors(t *testing.T) {
	for _, args := range [][]string{
		{"read", "--around", "1", "--since", "2026-01-01"},
		{"read", "--around", "1", "--until", "2026-01-01"},
		{"read", "--around", " "},
		{"read", "--around", "1", "--limit", "-1"},
	} {
		rc, out := runCLI(args...)
		var e errorOutput
		if rc != 1 || json.Unmarshal(out, &e) != nil || e.Error != "INVALID_ARGUMENT" {
			t.Errorf("%v: rc=%d out=%s", args, rc, out)
		}
	}
}

func TestHelpDocumentsContext(t *testing.T) {
	doc := map[string]cmdDoc{}
	for _, c := range helpDoc().Commands {
		doc[c.Name] = c
	}
	for _, f := range []string{"sender_id", "original_type", "event", "targets", "undecryptable", "reply_to", "forwarded",
		"mentions", "mentions_all", "reactions", "edited", "location", "call", "link_previews", "album", "duration_sec"} {
		if !strings.Contains(messageFields, f) {
			t.Errorf("messageFields lacks %s", f)
		}
	}
	if !strings.Contains(doc["read"].Usage, "--around") || !strings.Contains(strings.Join(doc["read"].Flags, " "), "--around") {
		t.Errorf("read help lacks --around: %+v", doc["read"])
	}
	flags := strings.Join(doc["search"].Flags, " ")
	for _, typ := range append(line.ContentTypeNames(), "url", "link", "notice", "type_N") {
		if !strings.Contains(flags, typ) {
			t.Errorf("search --type lacks %s", typ)
		}
	}
	for _, s := range []string{"double quotes", "case", "full-width", "read --around"} {
		if !strings.Contains(doc["search"].Summary, s) {
			t.Errorf("search summary lacks %q", s)
		}
	}
	if !strings.Contains(doc["read"].Summary, "sent_at, id, sender, me") {
		t.Error("read help lacks the --text columns")
	}
}
