package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/WellWells/line-cli/internal/line"
)

// Regression tests against the real LINE database on this machine (LINE must be running).
//
//	LINECLI_LIVE=1 go test ./internal/cli -run Live -v
//
// They only read: they never run init, scan LINE's memory or write key.dat.
// LINECLI_LIVE_COLD=1 additionally deletes the cached key and re-scans LINE's memory (init).
// Failures report only exit codes, error codes, counts and field names, never chat names or message content.

type check func(v any) string

type liveCase struct {
	name  string
	args  []string
	code  string // expected error code; "" means success
	check check
}

func liveOnly(t *testing.T, env string) {
	t.Helper()
	if os.Getenv(env) != "1" {
		t.Skip("set " + env + "=1 to run against the real LINE database")
	}
	// read and unread would fill open chats in from LINE's server; live tests
	// check this PC's data, and a test binary is not a program the firewall
	// allows, so the server is always unreachable here.
	t.Cleanup(line.UseFakeSquare(func([]byte) ([]byte, error) {
		return nil, errors.New("live tests do not contact LINE's server")
	}))
}

func runCLI(args ...string) (int, []byte) {
	rc, out, _ := runCLIErr(args...)
	return rc, out
}

// runCLIErr runs line-cli with --ids, so tests see every message and sender id
// (paging, dedupe and cross-checks need them); TestLiveLeanDefault checks what
// the output leaves out without it.
func runCLIErr(args ...string) (int, []byte, string) {
	var out, errb bytes.Buffer
	rc := Run(append([]string{"--ids"}, args...), &out, &errb)
	return rc, out.Bytes(), errb.String()
}

func decode(b []byte) any {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	return v
}

func rowsOf(v any) ([]map[string]any, string) {
	a, ok := v.([]any)
	if !ok {
		return nil, fmt.Sprintf("want array, got %T", v)
	}
	rows := make([]map[string]any, 0, len(a))
	for i, x := range a {
		m, ok := x.(map[string]any)
		if !ok {
			return nil, fmt.Sprintf("row %d: want object, got %T", i, x)
		}
		rows = append(rows, m)
	}
	return rows, ""
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

func all(checks ...check) check {
	return func(v any) string {
		for _, c := range checks {
			if msg := c(v); msg != "" {
				return msg
			}
		}
		return ""
	}
}

func hasKeys(keys ...string) check {
	return func(v any) string {
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Sprintf("want object, got %T", v)
		}
		for _, k := range keys {
			if _, ok := m[k]; !ok {
				return "missing key " + k
			}
		}
		return ""
	}
}

func maxLen(n int) check {
	return func(v any) string {
		rows, msg := rowsOf(v)
		if msg == "" && len(rows) > n {
			msg = fmt.Sprintf("got %d rows, want <= %d", len(rows), n)
		}
		return msg
	}
}

func isEmpty(v any) string {
	rows, msg := rowsOf(v)
	if msg == "" && len(rows) != 0 {
		msg = fmt.Sprintf("got %d rows, want 0", len(rows))
	}
	return msg
}

// sortedBy checks rows are ordered by a string field; rows missing the field are ignored.
func sortedBy(field string, desc bool) check {
	return func(v any) string {
		rows, msg := rowsOf(v)
		if msg != "" {
			return msg
		}
		var vals []string
		for _, r := range rows {
			if s := str(r, field); s != "" {
				vals = append(vals, s)
			}
		}
		ok := sort.SliceIsSorted(vals, func(i, j int) bool {
			if desc {
				return vals[i] > vals[j]
			}
			return vals[i] < vals[j]
		})
		if !ok {
			return fmt.Sprintf("not sorted by %s (desc=%v)", field, desc)
		}
		return ""
	}
}

func every(desc string, pred func(map[string]any) bool) check {
	return func(v any) string {
		rows, msg := rowsOf(v)
		if msg != "" {
			return msg
		}
		for i, r := range rows {
			if !pred(r) {
				return fmt.Sprintf("row %d: %s", i, desc)
			}
		}
		return ""
	}
}

func fieldIs(field, want string) check {
	return every(field+" != "+want, func(r map[string]any) bool { return str(r, field) == want })
}

var hasID = every("missing id", func(r map[string]any) bool { return str(r, "id") != "" })

func ids(rows []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, r := range rows {
		out[str(r, "id")] = true
	}
	return out
}

func runCase(t *testing.T, c liveCase) {
	t.Helper()
	t.Run(c.name, func(t *testing.T) {
		rc, out := runCLI(c.args...)
		v := decode(out)
		if c.code != "" {
			m, _ := v.(map[string]any)
			if rc != 1 || m == nil || m["error"] != c.code {
				t.Fatalf("want rc=1 error=%s, got rc=%d error=%v", c.code, rc, m["error"])
			}
		} else if rc != 0 {
			m, _ := v.(map[string]any)
			t.Fatalf("want rc=0, got rc=%d error=%v", rc, m["error"])
		}
		if c.check != nil {
			if msg := c.check(v); msg != "" {
				t.Fatal(msg)
			}
		}
	})
}

func mustRows(t *testing.T, args ...string) []map[string]any {
	t.Helper()
	rc, out := runCLI(args...)
	rows, msg := rowsOf(decode(out))
	if rc != 0 || msg != "" {
		t.Fatalf("%s: rc=%d %s", args[0], rc, msg)
	}
	return rows
}

func TestLiveCommands(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")

	chats := mustRows(t, "chats", "--limit", "5000")
	if len(chats) == 0 {
		t.Fatal("no chats")
	}
	cid := chats[0]["chat_id"].(string)
	for _, c := range chats {
		if typ := str(c, "type"); typ == "personal" || typ == "group" {
			cid = str(c, "chat_id")
			break
		}
	}
	names := map[string]int{}
	for _, c := range chats {
		names[str(c, "name")]++
	}

	now := time.Now()
	day := func(d time.Time) string { return d.Format("2006-01-02") }
	rangeFrom, rangeTo := now.AddDate(-1, 0, 0), now.AddDate(0, -6, 0)
	recent := now.AddDate(0, 0, -30)

	cases := []liveCase{
		{name: "help", args: []string{"help", "--json"}, check: hasKeys("tool", "version", "commands", "error_codes")},
		{name: "init help", args: []string{"init", "--help", "--json"}, check: hasKeys("name", "usage", "summary")},
		{name: "unknown command", args: []string{"bogus"}, code: "INVALID_ARGUMENT"},
		{name: "unknown flag", args: []string{"chats", "--nope"}, code: "INVALID_ARGUMENT"},

		{name: "chats", args: []string{"chats"}, check: all(maxLen(50), sortedBy("last_message_at", true))},
		{name: "chats limit", args: []string{"chats", "--limit", "5"}, check: maxLen(5)},
		{name: "chats limit 0 is default", args: []string{"chats", "--limit", "0"}, check: maxLen(50)},
		{name: "chats limit clamped", args: []string{"chats", "--limit", "99999"}, check: maxLen(5000)},
		{name: "chats limit negative", args: []string{"chats", "--limit", "-1"}, code: "INVALID_ARGUMENT"},
		{name: "chats limit not number", args: []string{"chats", "--limit", "abc"}, code: "INVALID_ARGUMENT"},
		{name: "chats chat-type bogus", args: []string{"chats", "--chat-type", "bogus"}, code: "INVALID_ARGUMENT"},
		{name: "chats type alias", args: []string{"chats", "--type", "group", "--limit", "5000"}, check: fieldIs("type", "group")},
		{name: "chats no match", args: []string{"chats", "zzzqqq_nomatch_123"}, check: isEmpty},

		{name: "contacts", args: []string{"contacts"}, check: func(v any) string {
			rows, msg := rowsOf(v)
			if msg == "" && (len(rows) == 0 || rows[0]["is_me"] != true) {
				msg = "first contact is not is_me"
			}
			return msg
		}},
		{name: "contacts no match", args: []string{"contacts", "zzzqqq_nomatch_123"}, check: isEmpty},

		{name: "read", args: []string{"read", cid}, check: all(maxLen(200), sortedBy("sent_at", false), hasID)},
		{name: "read until only", args: []string{"read", cid, "--until", day(rangeTo), "--limit", "20"},
			check: all(maxLen(20), sortedBy("sent_at", false), every("sent_at after --until", func(r map[string]any) bool {
				return str(r, "sent_at") < day(rangeTo.AddDate(0, 0, 1))
			}))},
		{name: "read limit", args: []string{"read", cid, "--limit", "3"}, check: maxLen(3)},
		{name: "read range", args: []string{"read", cid, "--since", day(rangeFrom), "--until", day(rangeTo), "--limit", "20"},
			check: all(maxLen(20), sortedBy("sent_at", false), every("sent_at outside range", func(r map[string]any) bool {
				s := str(r, "sent_at")
				return s >= day(rangeFrom) && s < day(rangeTo.AddDate(0, 0, 1))
			}))},
		{name: "read since after until", args: []string{"read", cid, "--since", day(rangeTo), "--until", day(rangeFrom)}, code: "INVALID_ARGUMENT"},
		{name: "read bad time", args: []string{"read", cid, "--since", "yesterday"}, code: "INVALID_ARGUMENT"},
		{name: "read no arg", args: []string{"read"}, code: "INVALID_ARGUMENT"},
		{name: "read not found", args: []string{"read", "zzzqqq_nomatch_123"}, code: "CHAT_NOT_FOUND"},

		{name: "search no args", args: []string{"search"}, code: "INVALID_ARGUMENT"},
		{name: "search text desc", args: []string{"search", "http", "--limit", "10"}, check: all(maxLen(10), sortedBy("sent_at", true), hasID)},
		{name: "search text asc", args: []string{"search", "http", "--limit", "10", "--order", "asc"}, check: all(maxLen(10), sortedBy("sent_at", false))},
		{name: "search order bogus", args: []string{"search", "http", "--order", "up"}, code: "INVALID_ARGUMENT"},
		{name: "search type bogus", args: []string{"search", "--type", "bogus"}, code: "INVALID_ARGUMENT"},
		{name: "search chat", args: []string{"search", "--chat", cid, "--limit", "5"}, check: all(maxLen(5), fieldIs("chat_id", cid))},
		{name: "search chat-type", args: []string{"search", "--chat-type", "group", "--limit", "5"}, check: all(maxLen(5), fieldIs("chat_type", "group"))},
		{name: "search from not found", args: []string{"search", "--from", "zzzqqq_nomatch_123"}, code: "SENDER_NOT_FOUND"},
		{name: "search since", args: []string{"search", "--since", day(recent), "--limit", "5"},
			check: every("sent_at before --since", func(r map[string]any) bool { return str(r, "sent_at") >= day(recent) })},

		{name: "find-file", args: []string{"find-file", "--limit", "20"}, check: maxLen(20)},
		{name: "find-file ext", args: []string{"find-file", "--ext", ".PDF", "--limit", "20"},
			check: every("filename not .pdf", func(r map[string]any) bool { return strings.HasSuffix(strings.ToLower(str(r, "filename")), ".pdf") })},
		{name: "find-file no-local", args: []string{"find-file", "--limit", "20", "--no-local"},
			check: every("probable_local_path present", func(r map[string]any) bool { return r["probable_local_path"] == nil })},

		{name: "unread", args: []string{"unread"}, check: all(maxLen(50), every("message missing id", func(r map[string]any) bool {
			m, _ := r["messages"].([]any)
			for _, x := range m {
				if mm, _ := x.(map[string]any); str(mm, "id") == "" {
					return false
				}
			}
			return true
		}))},

		{name: "download no args", args: []string{"download"}, code: "INVALID_ARGUMENT"},
		{name: "download id not found", args: []string{"download", "--id", "zzzqqq_nomatch_123", "--out", t.TempDir()}, code: "INVALID_ARGUMENT"},
		{name: "unread limits", args: []string{"unread", "--include-official", "--limit-chats", "5", "--per-chat-limit", "3"},
			check: all(maxLen(5), every("--per-chat-limit 3 not honoured", perChatCapped(3)))},
	}

	for _, typ := range []string{"personal", "group", "multi", "official", "open"} {
		cases = append(cases, liveCase{name: "chats chat-type " + typ, args: []string{"chats", "--chat-type", typ, "--limit", "5000"}, check: fieldIs("type", typ)})
	}
	for _, k := range line.ContentTypeNames() {
		cases = append(cases, liveCase{name: "search type " + k, args: []string{"search", "--type", k, "--limit", "5"}, check: all(maxLen(5), fieldIs("type", k))})
	}
	for name, n := range names {
		if n == 1 && name != "" && name != cid {
			cases = append(cases, liveCase{name: "read by exact name", args: []string{"read", name, "--limit", "3"}, check: maxLen(3)})
			break
		}
	}
	if names["a"] == 0 && len(mustRows(t, "chats", "a", "--limit", "5000")) >= 2 {
		cases = append(cases, liveCase{name: "read ambiguous", args: []string{"read", "a"}, code: "CHAT_AMBIGUOUS", check: func(v any) string {
			m, _ := v.(map[string]any)
			if c, _ := m["candidates"].([]any); len(c) < 2 {
				return "want >= 2 candidates"
			}
			return ""
		}})
	}

	for _, c := range cases {
		runCase(t, c)
	}
}

// TestLivePaging checks that the paging hints in help actually work: a sent_at
// passed back as --since/--until covers its whole second (no gap), each read mode
// anchors where help says, following the stderr notes reaches every message, and
// the notes say what each command's ordering needs.
func TestLivePaging(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	var cid string
	var recent []map[string]any // newest 40 messages, oldest first
	for _, c := range mustRows(t, "chats", "--limit", "50") {
		if rows := mustRows(t, "read", str(c, "chat_id"), "--limit", "40"); len(rows) == 40 {
			cid, recent = str(c, "chat_id"), rows
			break
		}
	}
	if cid == "" {
		t.Skip("no chat with 40+ messages among the 50 most recent")
	}
	idList := func(rows []map[string]any) []string {
		var out []string
		for _, r := range rows {
			out = append(out, str(r, "id"))
		}
		return out
	}
	sameSet := func(a, b []map[string]any) bool {
		if len(a) != len(b) {
			return false
		}
		in := ids(a)
		for _, r := range b {
			if !in[str(r, "id")] {
				return false
			}
		}
		return true
	}

	// quiet runs a command whose result is far below its limit: no note may print.
	quiet := func(args ...string) []map[string]any {
		t.Helper()
		rc, out, stderr := runCLIErr(args...)
		rows, msg := rowsOf(decode(out))
		if rc != 0 || msg != "" {
			t.Fatalf("%s: rc=%d %s", args[0], rc, msg)
		}
		if strings.Contains(stderr, "note:") {
			t.Fatalf("%s: note printed below the limit: %q", args[0], stderr)
		}
		return rows
	}
	shareSecond := func(rows []map[string]any) bool {
		return len(rows) > 0 && str(rows[0], "sent_at") == str(rows[len(rows)-1], "sent_at")
	}
	const stall = "paging by sent_at cannot pass this second, rerun with a larger --limit"

	// A one-second window at a message's own sent_at must contain it. Before the
	// +999 ms --until bound, messages with a non-zero millisecond part fell outside.
	for i := 0; i < len(recent); i += 8 {
		at, id := str(recent[i], "sent_at"), str(recent[i], "id")
		if !ids(quiet("read", cid, "--since", at, "--until", at, "--limit", "5000"))[id] {
			t.Fatalf("read: one-second window at sent_at misses message %d", i)
		}
		if !ids(quiet("search", "--chat", cid, "--since", at, "--until", at, "--limit", "5000"))[id] {
			t.Fatalf("search: one-second window at sent_at misses message %d", i)
		}
	}
	var img, expired map[string]any
	if imgs := mustRows(t, "search", "--type", "image", "--limit", "1"); len(imgs) == 1 {
		img = imgs[0]
		at := str(img, "sent_at")
		if !ids(quiet("preview", str(img, "chat_id"), "--since", at, "--until", at, "--out", t.TempDir()))[str(img, "id")] {
			t.Fatal("preview: one-second window at sent_at misses the image")
		}
	}
	// download over an expired file's second: nothing is fetched from the server,
	// as long as every file of that chat in that second has expired.
	for _, f := range mustRows(t, "find-file", "--limit", "200", "--no-local") {
		if f["expired"] == true {
			at := str(f, "sent_at")
			allExpired := true
			for _, g := range mustRows(t, "find-file", "--chat", str(f, "chat_id"), "--since", at, "--until", at, "--limit", "5000", "--no-local") {
				allExpired = allExpired && g["expired"] == true
			}
			if !allExpired {
				continue
			}
			expired = f
			if !ids(quiet("download", str(f, "chat_id"), "--since", at, "--until", at, "--out", t.TempDir()))[str(f, "id")] {
				t.Fatal("download: one-second window at sent_at misses the file")
			}
			break
		}
	}

	// Anchors: no range = newest N; only --until = newest N up to it; --since = first N from it.
	// The newest end moves when a message arrives, so compare with a fresh read and retry once.
	for try := 0; ; try++ {
		fresh := mustRows(t, "read", cid, "--limit", "40")
		if sameSet(mustRows(t, "read", cid, "--limit", "5"), fresh[35:]) {
			break
		}
		if try == 1 {
			t.Fatal("read without range is not the newest 5")
		}
	}
	u := str(recent[20], "sent_at")
	var upTo []map[string]any
	for _, m := range recent {
		if str(m, "sent_at") <= u {
			upTo = append(upTo, m)
		}
	}
	if got := mustRows(t, "read", cid, "--until", u, "--limit", "5"); !sameSet(got, upTo[len(upTo)-5:]) {
		t.Fatal("read --until is not the newest 5 up to --until")
	}
	if s := str(recent[10], "sent_at"); s > str(recent[0], "sent_at") {
		var from []map[string]any
		for _, m := range recent {
			if str(m, "sent_at") >= s {
				from = append(from, m)
			}
		}
		if got := mustRows(t, "read", cid, "--since", s, "--limit", "5"); !sameSet(got, from[:5]) {
			t.Fatal("read --since is not the first 5 from --since")
		}
	}

	// Following the notes from the newest page must make progress and leave no gap.
	seen := map[string]bool{}
	limit, until := 5, ""
	for page := 0; page < 8; page++ {
		args := []string{"read", cid, "--limit", fmt.Sprint(limit)}
		if until != "" {
			args = append(args, "--until", until)
		}
		rc, out, stderr := runCLIErr(args...)
		rows, msg := rowsOf(decode(out))
		if rc != 0 || msg != "" {
			t.Fatalf("page %d: rc=%d %s", page, rc, msg)
		}
		fresh := 0
		for _, id := range idList(rows) {
			if !seen[id] {
				seen[id] = true
				fresh++
			}
		}
		if !strings.Contains(stderr, "note:") {
			if len(rows) == limit {
				t.Fatalf("page %d: full page without a note", page)
			}
			break // reached the start of the chat
		}
		if strings.Contains(stderr, stall) {
			if len(rows) != limit || !shareSecond(rows) {
				t.Fatalf("page %d: stall note on a page that does not sit in one second", page)
			}
			limit *= 2
			continue
		}
		if !strings.Contains(stderr, "older: rerun with --until <first sent_at>") {
			t.Fatalf("page %d: unexpected note %q", page, stderr)
		}
		if fresh == 0 {
			t.Fatalf("page %d: following the --until hint returned no new message", page)
		}
		until = str(rows[0], "sent_at")
	}
	oldest := len(recent)
	for i, m := range recent {
		if seen[str(m, "id")] && i < oldest {
			oldest = i
		}
	}
	for i, m := range recent[oldest:] {
		if !seen[str(m, "id")] {
			t.Fatalf("paging skipped message %d of the newest 40", oldest+i)
		}
	}

	// The note matches each command's ordering. A full page inside one second must
	// get the stall note, and any other full page must not.
	mid := str(recent[20], "sent_at")
	repeat := boundaryRepeats
	type noteCase struct {
		args []string
		want string
	}
	notes := []noteCase{
		{[]string{"read", cid, "--since", mid, "--limit", "3"}, "newer: rerun with --since <last sent_at>" + repeat},
		{[]string{"read", cid, "--until", mid, "--limit", "3"}, "older: rerun with --until <first sent_at>" + repeat},
		{[]string{"search", "--chat", cid, "--limit", "3"}, "older: rerun with --until <last sent_at>" + repeat},
		{[]string{"search", "--chat", cid, "--order", "asc", "--limit", "3"}, "newer: rerun with --since <last sent_at>" + repeat},
		{[]string{"read", cid, "--limit", "1"}, stall},
		{[]string{"search", "--chat", cid, "--limit", "1"}, stall},
		{[]string{"search", "--chat", cid, "--order", "asc", "--limit", "1"}, stall},
		{[]string{"chats", "--limit", "1"}, "raise --limit (max 5000) or add a query"},
	}
	if img != nil {
		at := str(img, "sent_at")
		notes = append(notes,
			noteCase{[]string{"preview", str(img, "chat_id"), "--since", at, "--until", at, "--limit", "1", "--out", t.TempDir()}, stall},
			noteCase{[]string{"preview", str(img, "chat_id"), "--limit", "2", "--out", t.TempDir()}, "older: rerun with --until <first sent_at>" + repeat})
	}
	if expired != nil {
		at := str(expired, "sent_at")
		notes = append(notes, noteCase{[]string{"download", str(expired, "chat_id"), "--since", at, "--until", at, "--limit", "1", "--out", t.TempDir()}, stall})
	}
	for _, c := range notes {
		n := 0
		for i, a := range c.args {
			if a == "--limit" {
				fmt.Sscan(c.args[i+1], &n)
			}
		}
		rc, out, stderr := runCLIErr(c.args...)
		rows, msg := rowsOf(decode(out))
		if rc != 0 || msg != "" {
			t.Errorf("%v: rc=%d %s", c.args[:2], rc, msg)
			continue
		}
		if len(rows) < n {
			if strings.Contains(stderr, "note:") {
				t.Errorf("%v: note on a short page: %q", c.args[:2], stderr)
			}
			continue
		}
		want := c.want
		if c.args[0] != "chats" && shareSecond(rows) {
			want = stall
		}
		if !strings.Contains(stderr, "note: returned") || !strings.Contains(stderr, want) {
			t.Errorf("%v: want note %q, stderr=%q", c.args[:2], want, stderr)
		}
	}
	rc, _, stderr := runCLIErr("chats", "zzzqqq_nomatch_123")
	if rc != 0 || strings.Contains(stderr, "note:") {
		t.Fatalf("empty result should not print a limit note, stderr=%q", stderr)
	}
}

// TestLiveDownloadByMessageID checks that download --id finds an id from
// search and refuses a text message before anything is fetched.
func TestLiveDownloadByMessageID(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	hits := mustRows(t, "search", "--type", "text", "--limit", "1")
	if len(hits) == 0 {
		t.Skip("no text messages")
	}
	rc, out := runCLI("download", "--id", str(hits[0], "id"), "--out", t.TempDir())
	if m, _ := decode(out).(map[string]any); rc != 1 || m["error"] != "INVALID_ARGUMENT" || !strings.Contains(str(m, "message"), "(type text)") {
		t.Fatalf("download --id text message: rc=%d error=%v", rc, m["error"])
	}
}

func TestLiveTextOutput(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	cid := str(mustRows(t, "chats", "--limit", "1")[0], "chat_id")
	for _, args := range [][]string{
		{"help"}, {"chats", "--limit", "3"}, {"contacts", "zzzqqq_nomatch_123"}, {"read", cid, "--limit", "3"},
		{"search", "--type", "file", "--limit", "3"}, {"find-file", "--limit", "3"}, {"unread", "--limit-chats", "2", "--per-chat-limit", "1"},
	} {
		t.Run(args[0], func(t *testing.T) {
			rc, out := runCLI(append(args, "--text")...)
			if rc != 0 {
				t.Fatalf("rc=%d", rc)
			}
			if len(out) > 0 && (out[0] == '[' || out[0] == '{') {
				t.Fatal("--text produced JSON")
			}
		})
	}
	rc, out := runCLI("chats", "--type", "bogus", "--text")
	if m, _ := decode(out).(map[string]any); rc != 1 || m == nil || m["error"] != "INVALID_ARGUMENT" {
		t.Fatalf("--text error: want rc=1 JSON INVALID_ARGUMENT, got rc=%d", rc)
	}
}

func TestLivePreview(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	// Find a chat that has image/video messages so preview has something to try.
	hits := mustRows(t, "search", "--type", "image", "--limit", "1")
	if len(hits) == 0 {
		t.Skip("no image messages in this database")
	}
	cid := str(hits[0], "chat_id")
	dir := t.TempDir()
	rc, out := runCLI("preview", cid, "--out", dir, "--limit", "20")
	if rc != 0 {
		t.Fatalf("rc=%d out=%s", rc, out)
	}
	rows, msg := rowsOf(decode(out))
	if msg != "" {
		t.Fatal(msg)
	}
	validStatus := map[string]bool{"saved": true, "not_cached": true, "server_only": true, "unsupported": true, "write_failed": true}
	for _, r := range rows {
		if c := hasKeys("id", "type", "status")(r); c != "" {
			t.Fatal(c)
		}
		if !validStatus[str(r, "status")] {
			t.Fatalf("invalid status %q", str(r, "status"))
		}
		if str(r, "status") == "saved" {
			p := str(r, "saved_as")
			if fi, err := os.Stat(p); err != nil || fi.Size() == 0 {
				t.Fatalf("saved_as %q not a non-empty file: %v", p, err)
			}
		}
	}
}

func TestLiveOpenPathsIdentical(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	cid := str(mustRows(t, "chats", "--limit", "1")[0], "chat_id")
	for _, args := range [][]string{{"read", cid, "--limit", "200"}, {"search", "http", "--limit", "50"}, {"unread"}} {
		t.Run(args[0], func(t *testing.T) {
			rc1, vfs := runCLI(args...)
			t.Setenv("LINECLI_OPEN", "image")
			rc2, img := runCLI(args...)
			if rc1 != 0 || rc2 != 0 {
				t.Fatalf("rc vfs=%d image=%d", rc1, rc2)
			}
			if !bytes.Equal(vfs, img) {
				t.Fatalf("output differs: vfs %d bytes, image %d bytes", len(vfs), len(img))
			}
		})
	}
}

func TestLiveColdKeyScan(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	liveOnly(t, "LINECLI_LIVE_COLD")
	keyFile := filepath.Join(os.Getenv("LOCALAPPDATA"), "linecli", "key.dat")

	rc, out := runCLI("init", "--forget")
	if m, _ := decode(out).(map[string]any); rc != 0 || m["forgotten"] != true {
		t.Fatalf("init --forget: rc=%d", rc)
	}
	if _, err := os.Stat(keyFile); !os.IsNotExist(err) {
		t.Fatalf("key.dat still present after --forget: %v", err)
	}
	start := time.Now()
	mustRows(t, "chats", "--limit", "1")
	cold := time.Since(start)
	if _, err := os.Stat(keyFile); err != nil {
		t.Fatalf("key.dat not recreated: %v", err)
	}
	start = time.Now()
	mustRows(t, "chats", "--limit", "1")
	t.Logf("cold %v, warm %v", cold.Round(time.Millisecond), time.Since(start).Round(time.Millisecond))
	if rc, out := runCLI("init"); rc != 0 || hasKeys("ok")(decode(out)) != "" {
		t.Fatalf("init: rc=%d", rc)
	}
}
