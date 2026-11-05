package cli

import (
	"strings"
	"testing"

	"github.com/WellWells/line-cli/internal/line"
)

// Live checks for status, unread boundaries and freshness notes. Like
// live_test.go they report only codes, counts, flags and field names.

func num(m map[string]any, k string) int64 { f, _ := m[k].(float64); return int64(f) }

// perChatCapped checks an unread chat against --per-chat-limit n: system
// events do not use it up, so at least min(n, available_count) messages, and
// at most n others. Messages without content count as possible events.
func perChatCapped(n int64) func(map[string]any) bool {
	return func(r map[string]any) bool {
		msgs, _ := r["messages"].([]any)
		var others int64
		for _, x := range msgs {
			m, _ := x.(map[string]any)
			if t := str(m, "type"); t != "event" && t != "notice" && str(m, "content") != "" {
				others++
			}
		}
		return int64(len(msgs)) >= min(n, num(r, "available_count")) && others <= n
	}
}

func liveStatus(t *testing.T) map[string]any {
	t.Helper()
	rc, out := runCLI("status")
	s, _ := decode(out).(map[string]any)
	if rc != 0 || s == nil {
		t.Fatalf("status: rc=%d", rc)
	}
	return s
}

func TestLiveStatus(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	s := liveStatus(t)
	if msg := hasKeys("line_running", "db_path", "db_size", "db_modified_at", "key_cached")(s); msg != "" {
		t.Fatal(msg)
	}
	if _, ok := s["line_running"].(bool); !ok || num(s, "db_size") <= 0 {
		t.Fatalf("line_running=%T db_size=%d", s["line_running"], num(s, "db_size"))
	}
	if s["key_cached"] != true {
		t.Skip("no cached key; database fields are not reported")
	}
	if msg := hasKeys("newest_message_at", "oldest_message_at", "stale_chats")(s); msg != "" {
		t.Fatal(msg)
	}
	if str(s, "oldest_message_at") > str(s, "newest_message_at") {
		t.Fatal("oldest_message_at is after newest_message_at")
	}
	sc, _ := s["stale_chats"].(map[string]any)
	ids, ok := sc["chat_ids"].([]any)
	if !ok || num(sc, "count") < int64(len(ids)) || len(ids) > 5 {
		t.Fatalf("stale_chats: count=%d ids=%d (array=%v)", num(sc, "count"), len(ids), ok)
	}
	newest := mustRows(t, "search", "--since", "2000-01-01", "--limit", "1")
	// A message may arrive between the two calls, so the hit may only be newer.
	if len(newest) == 1 && str(newest[0], "sent_at") < str(s, "newest_message_at") {
		t.Fatal("newest_message_at is after the newest search hit")
	}
	if s["line_running"] == true {
		if _, _, stderr := runCLIErr("chats", "--limit", "1"); strings.Contains(stderr, "LINE is not running") {
			t.Fatal("not-running note printed while LINE runs")
		}
	}
}

func TestLiveUnreadBoundary(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	running := liveStatus(t)["line_running"] == true
	all := mustRows(t, "unread", "--include-hidden", "--include-official", "--limit-chats", "5000")
	visible := mustRows(t, "unread", "--include-official", "--limit-chats", "5000")
	hidden := 0
	for i, u := range all {
		b := str(u, "boundary")
		unread, avail, missing := num(u, "unread_count"), num(u, "available_count"), num(u, "missing_count")
		stale, _ := u["stale"].(bool)
		if _, ok := u["muted"].(bool); !ok {
			t.Fatalf("chat %d: muted missing", i)
		}
		exact := b == "first_unread" || b == "read_marker"
		if !exact && b != "unread_count" && b != "not_stored" && b != "none" {
			t.Fatalf("chat %d: boundary %q", i, b)
		}
		// A boundary never offers more unread messages than LINE counts.
		if missing != max(0, unread-avail) || avail < 0 || avail > unread {
			t.Fatalf("chat %d: unread=%d available=%d missing=%d", i, unread, avail, missing)
		}
		if want := exact && missing == 0 && !stale && running; u["fully_synced"] != want {
			t.Fatalf("chat %d: fully_synced=%v, want %v (boundary %s, missing %d, stale %v, LINE running %v)", i, u["fully_synced"], want, b, missing, stale, running)
		}
		if (b == "none" || b == "not_stored") && avail != 0 || b == "unread_count" && avail != unread {
			t.Fatalf("chat %d: boundary %s with available_count %d of %d", i, b, avail, unread)
		}
		if msgs, _ := u["messages"].([]any); b == "not_stored" && len(msgs) != 0 {
			t.Fatalf("chat %d: boundary not_stored with %d messages", i, len(msgs))
		}
		// LINE counts exactly the non-event messages from its first unread one.
		if b == "first_unread" && !stale && avail != unread {
			t.Fatalf("chat %d: first_unread boundary gives %d stored unread, LINE says %d", i, avail, unread)
		}
		if !perChatCapped(200)(u) {
			t.Fatalf("chat %d: default --per-chat-limit 200 not honoured (%d available)", i, avail)
		}
		if stale && (str(u, "last_activity_at") == "" || str(u, "last_activity_at") <= str(u, "newest_stored_at")) {
			t.Fatalf("chat %d: stale without newer last_activity_at", i)
		}
		if u["hidden"] == true {
			hidden++
		}
	}
	if len(visible) != len(all)-hidden {
		t.Fatalf("without --include-hidden: %d chats, want %d", len(visible), len(all)-hidden)
	}
	for _, u := range visible {
		if u["hidden"] == true {
			t.Fatal("hidden chat listed without --include-hidden")
		}
	}
	for _, u := range mustRows(t, "unread", "--per-chat-limit", "1") {
		if msgs, _ := u["messages"].([]any); num(u, "available_count") > 0 && len(msgs) == 0 || !perChatCapped(1)(u) {
			t.Fatal("events crowded out the unread message, or --per-chat-limit 1 not honoured")
		}
	}
}

func TestLiveFreshnessNotes(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	var cid string
	for _, c := range mustRows(t, "chats", "--limit", "50") {
		if typ := str(c, "type"); typ == "group" || typ == "personal" {
			cid = str(c, "chat_id")
			break
		}
	}
	if cid == "" {
		t.Skip("no group or personal chat")
	}
	const older = "older history is not on this PC"
	rc, out, stderr := runCLIErr("read", cid, "--until", "2000-01-01")
	if rows, msg := rowsOf(decode(out)); rc != 0 || msg != "" || len(rows) != 0 || !strings.Contains(stderr, "no stored messages in this range") || !strings.Contains(stderr, older) {
		t.Fatalf("read before history: rc=%d rows=%d note=%v", rc, len(rows), strings.Contains(stderr, older))
	}
	if _, _, stderr := runCLIErr("read", cid, "--since", "2000-01-01", "--limit", "1"); !strings.Contains(stderr, "reached the oldest message stored for chat "+cid) {
		t.Fatal("read --since before history: no oldest-message note")
	}
	if _, _, stderr := runCLIErr("read", cid, "--limit", "1"); strings.Contains(stderr, older) && len(mustRows(t, "read", cid, "--limit", "2")) == 2 {
		t.Fatal("history note on a page that did not reach the oldest message")
	}

	s := liveStatus(t)
	sc, _ := s["stale_chats"].(map[string]any)
	ids, _ := sc["chat_ids"].([]any)
	if len(ids) == 0 {
		t.Skip("no stale chat right now")
	}
	stale := ids[0].(string)
	const sync = "open the chat in LINE to sync"
	for _, args := range [][]string{{"read", stale, "--limit", "1"}, {"search", "--chat", stale, "--limit", "1"}} {
		if rc, _, stderr := runCLIErr(args...); rc != 0 || !strings.Contains(stderr, "note: chat "+stale+" has activity up to") || !strings.Contains(stderr, sync) {
			t.Fatalf("%s on a stale chat: rc=%d, no stale note", args[0], rc)
		}
	}
	if _, _, stderr := runCLIErr("read", stale, "--until", "2000-01-01"); strings.Contains(stderr, sync) {
		t.Fatal("stale note for a range that ends before the newest stored message")
	}
	for _, u := range mustRows(t, "unread", "--include-hidden", "--include-official", "--limit-chats", "5000") {
		if str(u, "chat_id") == stale && (u["stale"] != true || u["fully_synced"] != false) {
			t.Fatal("stale chat reported as synced by unread")
		}
	}
}

func TestLiveThreadAndEmptyChatNotes(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	const sync = "open the chat in LINE to sync"
	chats := mustRows(t, "chats", "--limit", "5000")
	for _, c := range chats {
		id := str(c, "chat_id")
		if !line.IsThreadChat(id) {
			continue
		}
		rc, _, stderr := runCLIErr("read", id, "--limit", "1")
		if rc != 0 || strings.Contains(stderr, sync) || strings.Contains(stderr, "has activity") && !strings.Contains(stderr, "note: thread chat "+id) {
			t.Fatalf("read on a thread chat: rc=%d, sync advice %v", rc, strings.Contains(stderr, sync))
		}
		break
	}
	// A chat with nothing stored: a range ending long before its activity is
	// not reported as unsynced.
	for i := len(chats) - 1; i >= 0; i-- {
		id := str(chats[i], "chat_id")
		if !strings.HasPrefix(id, "u") || len(mustRows(t, "read", id, "--limit", "1")) != 0 {
			continue
		}
		rc, _, stderr := runCLIErr("read", id, "--until", "2000-01-01")
		if rc != 0 || strings.Contains(stderr, sync) || !strings.Contains(stderr, "has no messages stored on this PC") {
			t.Fatalf("read --until 2000-01-01 on a chat with nothing stored: rc=%d, sync advice %v", rc, strings.Contains(stderr, sync))
		}
		return
	}
	t.Log("no one-to-one chat without stored messages")
}
