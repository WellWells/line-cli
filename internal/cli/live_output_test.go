package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Live checks for global output flags, stats, find-file ranges and preview
// ordering. Like live_test.go they assert only counts, codes, field names and
// types, never chat names or message content.

func outLiveObj(t *testing.T, args ...string) map[string]any {
	t.Helper()
	rc, out := runCLI(args...)
	m, _ := decode(out).(map[string]any)
	if rc != 0 || m == nil {
		t.Fatalf("%s: rc=%d, want an object", args[0], rc)
	}
	return m
}

func outNum(m map[string]any, k string) int { f, _ := m[k].(float64); return int(f) }

var outBiggest struct {
	id string
	n  int
}

// outBiggestChat returns the chat with the most messages among the 10 most recent.
func outBiggestChat(t *testing.T) (string, int) {
	t.Helper()
	if outBiggest.id != "" {
		return outBiggest.id, outBiggest.n
	}
	outBiggest.n = -1
	for _, c := range mustRows(t, "chats", "--limit", "10") {
		s := outLiveObj(t, "stats", str(c, "chat_id"), "--by", "type", "--include-events")
		if outNum(s, "total") > outBiggest.n {
			outBiggest.id, outBiggest.n = str(c, "chat_id"), outNum(s, "total")
		}
	}
	return outBiggest.id, outBiggest.n
}

func TestLiveStats(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	cid, total := outBiggestChat(t)
	if total <= 0 {
		t.Skip("no messages")
	}
	for _, by := range []string{"sender", "day", "hour", "type"} {
		for _, ev := range []bool{false, true} {
			args := []string{"stats", cid, "--by", by, "--limit", "5000"}
			if ev {
				args = append(args, "--include-events")
			}
			start := time.Now()
			s := outLiveObj(t, args...)
			// Normally well under a second; the bound only catches a lost index.
			if el := time.Since(start); el > 15*time.Second {
				t.Errorf("%v took %v", args[2:], el)
			} else if el > time.Second {
				t.Logf("%v took %v", args[2:], el)
			}
			if c := hasKeys("chat_id", "chat_name", "chat_type", "by", "include_events", "total", "group_count", "groups")(s); c != "" {
				t.Fatal(c)
			}
			groups, msg := rowsOf(s["groups"])
			if msg != "" || len(groups) != outNum(s, "group_count") {
				t.Fatalf("%v: %s, %d groups of %d", args[2:], msg, len(groups), outNum(s, "group_count"))
			}
			sum := 0
			for _, g := range groups {
				if c := hasKeys("count", "first_sent_at", "last_sent_at")(g); c != "" {
					t.Fatalf("%v: %s", args[2:], c)
				}
				sum += outNum(g, "count")
			}
			if sum != outNum(s, "total") {
				t.Errorf("%v: groups add up to %d, total %d", args[2:], sum, outNum(s, "total"))
			}
			if ev && outNum(s, "total") != total {
				t.Errorf("%v: total %d, want %d", args[2:], outNum(s, "total"), total)
			}
			if by == "hour" && len(groups) > 24 {
				t.Errorf("%d hour groups", len(groups))
			}
		}
	}
	// --limit keeps the most active senders; the default caps at 100.
	s := outLiveObj(t, "stats", cid, "--limit", "2")
	groups, _ := rowsOf(s["groups"])
	if len(groups) > 2 || len(groups) == 2 && outNum(groups[0], "count") < outNum(groups[1], "count") {
		t.Errorf("--limit 2: %d groups, not most active first", len(groups))
	}
	rc, out := runCLI("stats", "zzzqqq_nomatch_123")
	if m, _ := decode(out).(map[string]any); rc != 1 || m["error"] != "CHAT_NOT_FOUND" {
		t.Errorf("unknown chat: rc=%d", rc)
	}
}

func TestLiveStatsMatchesRead(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	// A chat quiet for a day, and a fixed --until, so no message arrives between the two calls.
	until := time.Now().Add(-time.Hour).Format(time.RFC3339)
	quiet := time.Now().AddDate(0, 0, -1).Format(time.RFC3339)
	for _, c := range mustRows(t, "chats", "--limit", "60") {
		if str(c, "last_message_at") == "" || str(c, "last_message_at") > quiet {
			continue
		}
		s := outLiveObj(t, "stats", str(c, "chat_id"), "--by", "type", "--include-events", "--since", "2000-01-01", "--until", until)
		n := outNum(s, "total")
		if n == 0 || n > 3000 {
			continue
		}
		rows := mustRows(t, "read", str(c, "chat_id"), "--since", "2000-01-01", "--until", until, "--limit", "5000")
		if len(rows) != n {
			t.Fatalf("stats total %d, read returned %d", n, len(rows))
		}
		return
	}
	t.Skip("no chat quiet for a day with 1..3000 messages")
}

func TestLiveOutFileAndJSONL(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	cid, _ := outBiggestChat(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "page.json")
	s := outLiveObj(t, "read", cid, "--limit", "40", "--out", path)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, msg := rowsOf(decode(b))
	if msg != "" || len(rows) != outNum(s, "count") || len(b) != outNum(s, "bytes") || str(s, "saved_as") != path {
		t.Fatalf("summary count=%d bytes=%d vs file rows=%d bytes=%d (%s)", outNum(s, "count"), outNum(s, "bytes"), len(rows), len(b), msg)
	}
	if len(rows) == 40 {
		if s["limit_reached"] != true || !strings.Contains(str(s, "next"), str(rows[0], "sent_at")) || str(s, "first_sent_at") != str(rows[0], "sent_at") {
			t.Fatalf("full page: limit_reached=%v, next does not carry the first sent_at", s["limit_reached"])
		}
	}

	rc, out := runCLI("read", cid, "--limit", "25", "--jsonl")
	if rc != 0 {
		t.Fatalf("--jsonl rc=%d", rc)
	}
	lines := bytes.Split(bytes.TrimRight(out, "\n"), []byte("\n"))
	for i, l := range lines {
		var m map[string]any
		if json.Unmarshal(l, &m) != nil || str(m, "id") == "" {
			t.Fatalf("--jsonl line %d is not a message object", i)
		}
	}
	// Off a terminal the default is already one line, the same as --compact.
	rc, compact := runCLI("read", cid, "--limit", "25", "--compact")
	_, piped := runCLI("read", cid, "--limit", "25")
	if rc != 0 || bytes.Count(compact, []byte("\n")) != 1 || !bytes.Equal(compact, piped) {
		t.Fatalf("--compact: rc=%d lines=%d, same as piped default: %v", rc, bytes.Count(compact, []byte("\n")), bytes.Equal(compact, piped))
	}
	var a, b2 any
	if json.Unmarshal(compact, &a) != nil || json.Unmarshal(piped, &b2) != nil || len(a.([]any)) != len(lines) {
		t.Fatal("--compact and --jsonl disagree with the default output")
	}
}

// Without --ids, read leaves out what reading a chat does not need: message
// ids (except replied-to ones) and sender_ids (except where a name is shared);
// search keeps message ids. Everything else matches the --ids output row by row.
func TestLiveLeanDefault(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	cid, _ := outBiggestChat(t)
	for _, args := range [][]string{{"read", cid, "--limit", "200"}, {"search", "--type", "text", "--limit", "200"}} {
		var lean, full bytes.Buffer
		if Run(args, &lean, &bytes.Buffer{}) != 0 || Run(append([]string{"--ids"}, args...), &full, &bytes.Buffer{}) != 0 {
			t.Fatalf("%s failed", args[0])
		}
		l, msg := rowsOf(decode(lean.Bytes()))
		f, msg2 := rowsOf(decode(full.Bytes()))
		if msg != "" || msg2 != "" || len(l) != len(f) || len(l) == 0 {
			t.Fatalf("%s: %s %s, %d vs %d rows", args[0], msg, msg2, len(l), len(f))
		}
		senders := map[string]map[string]bool{} // sender name -> its sender_ids
		replied := map[string]bool{}
		for _, r := range f {
			if senders[str(r, "sender")] == nil {
				senders[str(r, "sender")] = map[string]bool{}
			}
			senders[str(r, "sender")][str(r, "sender_id")] = true
			replied[str(r, "reply_to")] = true
		}
		kept := 0
		for i := range l {
			shared := len(senders[str(f[i], "sender")]) > 1
			if sid := str(l[i], "sender_id"); (sid != "") != shared || sid != "" && sid != str(f[i], "sender_id") {
				t.Fatalf("%s row %d: sender_id kept=%v, name shared=%v", args[0], i, sid != "", shared)
			}
			wantID := args[0] == "search" || replied[str(f[i], "id")]
			if id := str(l[i], "id"); (id != "") != wantID || id != "" && id != str(f[i], "id") {
				t.Fatalf("%s row %d: id kept=%v, want %v", args[0], i, id != "", wantID)
			}
			if str(l[i], "id") != "" {
				kept++
			}
			for _, r := range []map[string]any{l[i], f[i]} {
				delete(r, "id")
				delete(r, "sender_id")
			}
			a, _ := json.Marshal(l[i])
			b, _ := json.Marshal(f[i])
			if !bytes.Equal(a, b) {
				t.Fatalf("%s row %d differs beyond id and sender_id", args[0], i)
			}
		}
		t.Logf("%s: %d rows, %d message ids kept, %d -> %d bytes (%.0f%%)", args[0], len(l), kept, full.Len(), lean.Len(), 100*float64(lean.Len())/float64(full.Len()))
	}
}

func TestLiveTimeIntegers(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	cid, _ := outBiggestChat(t)
	day := time.Now().AddDate(0, 0, -30)
	iso, compact := day.Format("2006-01-02"), day.Format("20060102")
	a := mustRows(t, "read", cid, "--since", iso, "--limit", "50")
	b := mustRows(t, "read", cid, "--since", compact, "--limit", "50")
	if len(a) != len(b) || len(a) > 0 && str(a[0], "id") != str(b[0], "id") {
		t.Fatalf("--since %s and %s differ: %d vs %d rows", iso, compact, len(a), len(b))
	}
	ms := time.Now().Add(-48 * time.Hour)
	c := mustRows(t, "read", cid, "--since", ms.Format(time.RFC3339), "--limit", "50")
	d := mustRows(t, "read", cid, "--since", strconv.FormatInt(ms.Truncate(time.Second).UnixMilli(), 10), "--limit", "50")
	if len(c) != len(d) {
		t.Fatalf("RFC3339 and millisecond --since differ: %d vs %d rows", len(c), len(d))
	}
	rc, out := runCLI("read", cid, "--until", "2026")
	if m, _ := decode(out).(map[string]any); rc != 1 || m["error"] != "INVALID_ARGUMENT" {
		t.Fatalf("--until 2026: rc=%d", rc)
	}
}

func TestLiveFindFileRanges(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	all := mustRows(t, "find-file", "--limit", "5000", "--no-local")
	if len(all) < 2 {
		t.Skip("fewer than 2 files")
	}
	asc := mustRows(t, "find-file", "--limit", "5000", "--no-local", "--order", "asc")
	if len(asc) != len(all) || !sort.SliceIsSorted(asc, func(i, j int) bool { return str(asc[i], "sent_at") < str(asc[j], "sent_at") }) {
		t.Fatalf("--order asc: %d rows vs %d, or not sorted", len(asc), len(all))
	}
	mid := str(all[len(all)/2], "sent_at")
	older := mustRows(t, "find-file", "--limit", "5000", "--no-local", "--until", mid)
	for _, r := range older {
		if str(r, "sent_at") > mid {
			t.Fatal("--until returned a later file")
		}
	}
	if len(older) == 0 || len(older) >= len(all) && str(all[0], "sent_at") > mid {
		t.Fatalf("--until %d of %d rows", len(older), len(all))
	}
	sum := 0
	for _, ct := range []string{"personal", "group", "multi", "official", "open"} {
		rows := mustRows(t, "find-file", "--limit", "5000", "--no-local", "--chat-type", ct)
		if c := fieldIs("chat_type", ct)(outAnyRows(rows)); c != "" {
			t.Fatalf("--chat-type %s: %s", ct, c)
		}
		sum += len(rows)
	}
	if sum > len(all) {
		t.Fatalf("chat types add up to %d of %d", sum, len(all))
	}
	for _, r := range mustRows(t, "find-file", "--limit", "5000") {
		if p := str(r, "local_path"); p != "" {
			if fi, err := os.Stat(p); err != nil || fi.IsDir() {
				t.Fatal("local_path does not exist")
			}
		}
	}
}

func outAnyRows(rows []map[string]any) any {
	out := make([]any, len(rows))
	for i, r := range rows {
		out[i] = r
	}
	return out
}

func TestLivePreviewNewestAndOpenChat(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	for _, typ := range []string{"group", "personal", "open"} {
		hits := mustRows(t, "search", "--type", "image", "--chat-type", typ, "--limit", "1")
		if len(hits) == 0 {
			continue
		}
		dir := t.TempDir()
		rows := mustRows(t, "preview", str(hits[0], "chat_id"), "--out", dir)
		if len(rows) == 0 || len(rows) > 50 {
			t.Fatalf("%s: %d rows with the default limit", typ, len(rows))
		}
		if c := sortedBy("sent_at", false)(outAnyRows(rows)); c != "" {
			t.Fatalf("%s: %s", typ, c)
		}
		// The newest image of the chat is in the default (newest) page.
		if !ids(rows)[str(hits[0], "id")] {
			t.Fatalf("%s: the newest image is not in the default page", typ)
		}
		for _, r := range rows {
			if c := hasKeys("id", "chat_id", "chat_name", "type", "sender", "status")(r); c != "" {
				t.Fatal(c)
			}
			if p := str(r, "saved_as"); p != "" && (!filepath.IsAbs(p) || !strings.HasPrefix(p, dir)) {
				t.Fatalf("saved_as outside --out or not absolute")
			}
			if typ == "open" && (str(r, "status") != "unsupported" || str(r, "note") == "") {
				t.Fatalf("open chat media: status %q", str(r, "status"))
			}
		}
	}
}

// unread's per-chat cut reaches the --out summary too, not only stderr.
func TestLiveUnreadCutInOutSummary(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	path := filepath.Join(t.TempDir(), "unread.json")
	s := outLiveObj(t, "unread", "--per-chat-limit", "1", "--out", path)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, msg := rowsOf(decode(b))
	if msg != "" {
		t.Fatal(msg)
	}
	cut := false
	for _, r := range rows {
		ms, _ := r["messages"].([]any)
		cut = cut || outNum(r, "available_count") > len(ms)
	}
	if cut && (s["limit_reached"] != true || !strings.Contains(str(s, "next"), "--per-chat-limit")) {
		t.Fatalf("per-chat cut not in summary: limit_reached=%v", s["limit_reached"])
	}
	if !cut && len(rows) < 50 && s["limit_reached"] != false {
		t.Fatal("limit_reached without a cut")
	}
}
