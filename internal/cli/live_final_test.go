package cli

import (
	"strings"
	"testing"
)

// Live checks for the final review fixes. Like live_test.go they report only
// codes, counts, flags and field names.

// stats counts only stored messages, so it prints the same stale note as read
// does for the whole chat: on every chat status or unread reports as stale,
// and on none where read prints none.
func TestLiveStatsStaleNote(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	const note = "has activity up to"
	stale := map[string]bool{}
	sc, _ := liveStatus(t)["stale_chats"].(map[string]any)
	ids, _ := sc["chat_ids"].([]any)
	for _, id := range ids {
		stale[id.(string)] = true
	}
	for _, u := range mustRows(t, "unread", "--include-hidden", "--include-official", "--limit-chats", "5000", "--per-chat-limit", "1") {
		if u["stale"] == true {
			stale[str(u, "chat_id")] = true
		}
	}
	for _, c := range mustRows(t, "chats", "--limit", "10") {
		stale[str(c, "chat_id")] = stale[str(c, "chat_id")]
	}
	n := 0
	for id, want := range stale {
		rc, _, stderr := runCLIErr("stats", id)
		_, _, readErr := runCLIErr("read", id, "--limit", "1")
		if rc != 0 || strings.Contains(stderr, note) != strings.Contains(readErr, note) || want && !strings.Contains(stderr, note) {
			t.Errorf("stats rc=%d: stale note %v, read's %v, reported stale %v", rc, strings.Contains(stderr, note), strings.Contains(readErr, note), want)
		}
		if want {
			n++
		}
	}
	t.Logf("%d chats, %d reported stale", len(stale), n)
}

func TestLiveFinalArgumentChecks(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	cid := str(mustRows(t, "chats", "--limit", "1")[0], "chat_id")
	for _, args := range [][]string{
		{"unread", cid},
		{"version", "extra"},
		{"search", "x", "--chat", cid, "--chat-type", "group"},
		{"find-file", "--chat", cid, "--chat-type", "open"},
	} {
		rc, out := runCLI(args...)
		if m, _ := decode(out).(map[string]any); rc != 1 || m["error"] != "INVALID_ARGUMENT" {
			t.Errorf("%s: rc=%d error=%v", args[0], rc, m["error"])
		}
	}
}

// send --to <your own display name> (dry run only) either resolves to your own
// chat or lists it among the CHAT_AMBIGUOUS candidates; it never silently picks
// another chat that shares the name.
func TestLiveSendOwnNameDryRun(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	var name string
	for _, c := range mustRows(t, "contacts", "--limit", "1") {
		if c["is_me"] == true {
			name = str(c, "display_name")
		}
	}
	if name == "" {
		t.Skip("no profile name")
	}
	before := sendLogStat()
	rc, m, _ := dryRun(t, "--to", name, "--message", liveProbe)
	switch {
	case rc == 0:
		if str(m, "chat_type") != "self" || m["sent"] != false {
			t.Errorf("own name resolved to chat_type %q", str(m, "chat_type"))
		}
	case str(m, "error") == "CHAT_AMBIGUOUS":
		self := false
		cands, _ := m["candidates"].([]any)
		for _, c := range cands {
			self = self || str(c.(map[string]any), "type") == "self"
		}
		if !self {
			t.Errorf("own chat missing from %d candidates", len(cands))
		}
	default:
		t.Errorf("own name: rc=%d error=%s", rc, str(m, "error"))
	}
	if sendLogStat() != before {
		t.Error("a dry run wrote the send log")
	}
}

// find-file, stats and download name senders like read and give sender_id.
func TestLiveSenderMatchesRead(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	files := mustRows(t, "find-file", "--limit", "5000", "--no-local")
	checked := 0
	for _, f := range files {
		if str(f, "sender_id") == "" {
			t.Fatal("find-file row without sender_id")
		}
		if str(f, "sender") != str(f, "sender_id") && checked >= 10 {
			continue
		}
		checked++
		for _, m := range mustRows(t, "read", "--around", str(f, "id"), "--limit", "3") {
			if str(m, "id") == str(f, "id") && (str(m, "sender") != str(f, "sender") || str(m, "sender_id") != str(f, "sender_id")) {
				t.Errorf("find-file sender differs from read's (raw id: %v)", str(f, "sender") == str(f, "sender_id"))
			}
		}
		if checked >= 40 {
			break
		}
	}
	for _, f := range files {
		if f["expired"] == true {
			rows := mustRows(t, "download", "--id", str(f, "id"), "--out", t.TempDir())
			if len(rows) != 1 || str(rows[0], "status") != "expired" || str(rows[0], "sender_id") != str(f, "sender_id") || str(rows[0], "sender") != str(f, "sender") {
				t.Errorf("download --id of an expired file: %d rows", len(rows))
			}
			break
		}
	}
	t.Logf("%d of %d files compared with read", checked, len(files))
}
