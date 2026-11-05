package cli

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Live checks for message context and search against the real database.
// Failures report only counts, codes and field names.

func visibleText(r map[string]any) string {
	parts := []string{str(r, "content"), str(r, "filename"), str(r, "title"), str(r, "description")}
	if lp, _ := r["link_previews"].([]any); lp != nil {
		for _, x := range lp {
			p, _ := x.(map[string]any)
			parts = append(parts, str(p, "title"), str(p, "description"))
		}
	}
	if loc, _ := r["location"].(map[string]any); loc != nil {
		parts = append(parts, str(loc, "address"), str(loc, "title"))
	}
	return foldForTest(strings.Join(parts, "\n"))
}

func foldForTest(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			r -= 0xFEE0
		case r == 0x3000:
			r = ' '
		}
		b.WriteString(strings.ToLower(string(r)))
	}
	return b.String()
}

func TestLiveContextFields(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	for _, c := range []struct {
		typ   string
		check func(map[string]any) string
	}{
		{"unsent", func(r map[string]any) string {
			if str(r, "original_type") == "" || r["content"] != nil || r["filename"] != nil {
				return "unsent row with content or without original_type"
			}
			return ""
		}},
		{"event", func(r map[string]any) string {
			if str(r, "event") == "" {
				return "event row without event"
			}
			if ts, ok := r["targets"]; ok {
				for _, x := range ts.([]any) {
					if str(x.(map[string]any), "id") == "" {
						return "target without id"
					}
				}
			}
			return ""
		}},
		{"text", func(r map[string]any) string {
			if c, ok := r["content"]; ok && c == "" {
				return `text row with content ""`
			}
			if _, ok := r["content"]; ok && r["undecryptable"] == true {
				return "undecryptable text row with content"
			}
			return ""
		}},
		{"call", func(r map[string]any) string {
			if c, ok := r["content"]; ok && c == "" {
				return `call row with content ""`
			}
			if call, ok := r["call"].(map[string]any); ok && str(call, "kind") == "" {
				return "call without kind"
			}
			return ""
		}},
	} {
		rows := mustRows(t, "search", "--type", c.typ, "--limit", "5000")
		if len(rows) == 0 {
			t.Logf("no %s rows", c.typ)
			continue
		}
		for i, r := range rows {
			if str(r, "type") != c.typ {
				t.Fatalf("%s row %d has type %s", c.typ, i, str(r, "type"))
			}
			if msg := c.check(r); msg != "" {
				t.Fatalf("%s row %d: %s", c.typ, i, msg)
			}
		}
	}

	rows := mustRows(t, "search", "--type", "text", "--limit", "5000")
	seen := map[string]int{}
	for _, r := range rows {
		if str(r, "sender_id") != "" {
			seen["sender_id"]++
		}
		for _, k := range []string{"reply_to", "mentions", "reactions", "forwarded", "link_previews"} {
			if _, ok := r[k]; ok {
				seen[k]++
			}
		}
		if m, ok := r["reactions"].(map[string]any); ok {
			by, _ := m["by_type"].(map[string]any)
			var sum float64
			for _, v := range by {
				sum += v.(float64)
			}
			if total, _ := m["total"].(float64); total <= 0 || sum > total {
				t.Fatalf("reactions total %v, by_type sum %v", total, sum)
			}
		}
	}
	t.Logf("newest %d text rows: %v", len(rows), seen)
	if seen["sender_id"] == 0 {
		t.Fatal("no sender_id in text rows")
	}
}

func TestLiveEventsCallsAndRenames(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	events := mustRows(t, "search", "--type", "event", "--limit", "5000")
	member, rawID := 0, 0
	var renamed map[string]any
	for _, r := range events {
		switch str(r, "event") {
		case "joined", "left":
			member++
			if str(r, "sender") == str(r, "sender_id") {
				rawID++
			}
		case "renamed":
			if c := str(r, "content"); renamed == nil && c != "" && !strings.ContainsAny(c, "\"“”") {
				renamed = r
			}
		}
	}
	t.Logf("%d events: %d joined/left, %d of them without a sender name", len(events), member, rawID)
	if member > 0 && rawID*2 > member {
		t.Fatalf("%d of %d joined/left events show a raw id as sender", rawID, member)
	}
	if renamed != nil {
		hits := mustRows(t, "search", `"`+str(renamed, "content")+`"`, "--type", "event", "--limit", "5000")
		if !ids(hits)[str(renamed, "id")] {
			t.Fatal("a renamed event is not found by its new name")
		}
	}
	calls := mustRows(t, "search", "--type", "call", "--limit", "5000")
	both := 0
	for _, r := range calls {
		if r["call"] != nil && str(r, "content") != "" {
			both++
		}
	}
	t.Logf("%d calls, %d with call and content", len(calls), both)
	if len(calls) > 0 && both == 0 {
		t.Fatal("call rows lost their content")
	}
	runCase(t, liveCase{name: "text with --type unsent", args: []string{"search", "x", "--type", "unsent"}, code: "INVALID_ARGUMENT"})
	if a, b := len(mustRows(t, "search", "--type", "notice", "--limit", "5000")), len(mustRows(t, "search", "--type", "type_18", "--limit", "5000")); a != b {
		t.Fatalf("--type notice %d rows, type_18 %d", a, b)
	}
}

func TestLiveSearchSemantics(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	for _, q := range [][]string{{"size"}, {"type"}, {"http", "com"}, {"ok"}} {
		rows := mustRows(t, append(append([]string{"search"}, q...), "--limit", "5000")...)
		for i, r := range rows {
			v := visibleText(r)
			for _, w := range q {
				if !strings.Contains(v, w) {
					t.Fatalf("search %v: hit %d (type %s) does not show %q", q, i, str(r, "type"), w)
				}
			}
		}
		t.Logf("search %v: %d hits, all visibly matching", q, len(rows))
	}
	if a, b := len(mustRows(t, "search", "ok", "--limit", "5000")), len(mustRows(t, "search", "ＯＫ", "--limit", "5000")); a != b {
		t.Fatalf("ok %d hits, full-width ＯＫ %d", a, b)
	}
	start := time.Now()
	mustRows(t, "search", "zzzqqq_nomatch_123", "--limit", "5000")
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("whole-database search took %v", d)
	} else {
		t.Logf("whole-database search: %v", d.Round(time.Millisecond))
	}
	for _, typ := range []string{"url", "link", "notice", "type_15"} {
		runCase(t, liveCase{name: "search type " + typ, args: []string{"search", "--type", typ, "--limit", "20"}, check: maxLen(20)})
	}
	for _, r := range mustRows(t, "search", "--type", "url", "--limit", "200") {
		if str(r, "type") != "text" || (!strings.Contains(str(r, "content"), "http") && r["link_previews"] == nil) {
			t.Fatal("--type url hit without a link in content or link_previews")
		}
	}
	for _, r := range mustRows(t, "search", "--type", "link", "--limit", "200") {
		if str(r, "type") != "post" {
			t.Fatal("--type link (older name of post) hit of another type")
		}
	}
}

func TestLiveReadAround(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	hits := mustRows(t, "search", "--type", "text", "--limit", "50")
	if len(hits) == 0 {
		t.Skip("no text messages")
	}
	h := hits[len(hits)/2]
	id, cid := str(h, "id"), str(h, "chat_id")
	rows := mustRows(t, "read", "--around", id)
	if !ids(rows)[id] || len(rows) > 20 {
		t.Fatalf("read --around: %d rows, target present %v", len(rows), ids(rows)[id])
	}
	if msg := sortedBy("sent_at", false)(decodeRows(rows)); msg != "" {
		t.Fatal(msg)
	}
	window := ids(mustRows(t, "read", cid, "--since", str(rows[0], "sent_at"), "--until", str(rows[len(rows)-1], "sent_at"), "--limit", "5000"))
	for _, r := range rows {
		if !window[str(r, "id")] {
			t.Fatal("read --around returned a message outside the chat's window")
		}
	}
	if got := mustRows(t, "read", "--around", id, "--limit", "5", cid); len(got) > 5 || !ids(got)[id] {
		t.Fatalf("read --around --limit 5 with chat: %d rows", len(got))
	}
	runCase(t, liveCase{name: "around not found", args: []string{"read", "--around", "zzzqqq_nomatch_123"}, code: "INVALID_ARGUMENT"})
	for _, c := range mustRows(t, "chats", "--limit", "50") {
		if other := str(c, "chat_id"); other != cid {
			runCase(t, liveCase{name: "around wrong chat", args: []string{"read", "--around", id, other}, code: "INVALID_ARGUMENT"})
			break
		}
	}
	for _, r := range mustRows(t, "search", "--type", "text", "--limit", "500") {
		if rt := str(r, "reply_to"); rt != "" {
			if rc, _ := runCLI("read", "--around", rt, "--limit", "1"); rc != 0 {
				t.Logf("reply target not stored locally (rc=%d)", rc)
			}
			break
		}
	}
}

func decodeRows(rows []map[string]any) any {
	out := make([]any, len(rows))
	for i, r := range rows {
		out[i] = r
	}
	return out
}

func TestLiveTextColumns(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	cid := str(mustRows(t, "chats", "--limit", "1")[0], "chat_id")
	for _, c := range []struct {
		args []string
		cols int
	}{
		{[]string{"read", cid, "--limit", "300"}, 6},
		{[]string{"search", "http", "--limit", "300"}, 7},
		{[]string{"search", "--type", "text", "--limit", "5000"}, 7},
		{[]string{"unread"}, 7},
	} {
		rc, out := runCLI(append(c.args, "--text")...)
		if rc != 0 {
			t.Fatalf("%v: rc=%d", c.args[:1], rc)
		}
		if i := strings.IndexFunc(string(out), func(r rune) bool {
			return r < 0x20 && r != '\t' && r != '\n' || r == 0x7f || r == 0x85 || r == 0x2028 || r == 0x2029
		}); i >= 0 {
			t.Fatalf("%v: unescaped control or line separator at byte %d", c.args[:1], i)
		}
		lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
		headers := 0
		for i, l := range lines {
			if l == "" {
				continue
			}
			want := c.cols
			if c.args[0] == "unread" && strings.HasPrefix(l, "#\t") {
				want = 6
				headers++
			}
			if n := len(strings.Split(l, "\t")); n != want {
				t.Fatalf("%s line %d has %d columns, want %d", c.args[0], i, n, want)
			}
		}
		rows := mustRows(t, c.args...)
		records := len(rows)
		if c.args[0] == "unread" {
			for _, u := range rows {
				msgs, _ := u["messages"].([]any)
				records += len(msgs)
			}
			if headers != len(rows) {
				t.Fatalf("unread: %d chats but %d header lines", len(rows), headers)
			}
		}
		if records != len(lines) && !(records == 0 && lines[0] == "") {
			t.Fatalf("%s: %d records but %d lines", c.args[0], records, len(lines))
		}
	}
}

// Without the time zone init in package line, read --around on a large chat
// crashed the runtime in a fresh process: in most runs started by python,
// PowerShell 7 or cmd, never in runs started by a Go program or Git Bash. So
// the binary is run through python when it is installed.
func TestLiveAroundStress(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	exe := filepath.Join(t.TempDir(), "linecli.exe")
	if out, err := exec.Command("go", "build", "-o", exe, "github.com/WellWells/line-cli/cmd/linecli").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	hits := mustRows(t, "search", "--type", "text", "--limit", "5000")
	if len(hits) == 0 {
		t.Skip("no text messages")
	}
	byChat := map[string][]string{}
	top := ""
	for _, h := range hits {
		c := str(h, "chat_id")
		byChat[c] = append(byChat[c], str(h, "id"))
		if len(byChat[c]) > len(byChat[top]) {
			top = c
		}
	}
	id := byChat[top][len(byChat[top])/2]
	args := []string{exe, "read", "--around", id}
	if py, err := exec.LookPath("python"); err == nil {
		const relay = "import subprocess, sys; p = subprocess.run(sys.argv[1:], capture_output=True); " +
			"sys.stdout.buffer.write(p.stdout); sys.stderr.buffer.write(p.stderr); sys.exit(p.returncode)"
		args = append([]string{py, "-c", relay}, args...)
	} else {
		t.Log("python not found; a Go parent process rarely shows the crash")
	}
	for i := 0; i < 10; i++ {
		cmd := exec.Command(args[0], args[1:]...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if err != nil || len(mustDecodeRows(t, stdout.Bytes())) == 0 {
			first, _, _ := strings.Cut(stderr.String(), "\n")
			t.Fatalf("run %d: %v; %s", i, err, first)
		}
	}
}

func mustDecodeRows(t *testing.T, b []byte) []map[string]any {
	t.Helper()
	rows, msg := rowsOf(decode(b))
	if msg != "" {
		t.Fatal(msg)
	}
	return rows
}
