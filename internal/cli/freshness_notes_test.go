package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WellWells/line-cli/internal/line"
)

const minute = int64(60_000)

var base = int64(1_700_000_000_000)

func TestStaleNote(t *testing.T) {
	useEdition(t, "full")
	f := &line.Freshness{ChatID: "c0000000001", LastActivity: base + 90*minute, NewestStored: base}
	var errb bytes.Buffer
	rc := &runCtx{out: &bytes.Buffer{}, err: &errb}
	rc.noteStale(f, 0, hintRead)
	want := "line: note: chat c0000000001 has activity up to " + line.TimeISO(base+90*minute) + " but the newest stored message is " + line.TimeISO(base) + "; open the chat in LINE to sync\n"
	if errb.String() != want {
		t.Fatalf("got %q\nwant %q", errb.String(), want)
	}
	for _, until := range []int64{base / 1000, (base - 10*minute) / 1000} {
		errb.Reset()
		rc.noteStale(f, until, hintRead)
		if errb.Len() != 0 {
			t.Errorf("--until %d ends before the unsynced span but printed %q", until, errb.String())
		}
	}
	errb.Reset()
	rc.noteStale(f, (base+30*minute)/1000, hintRead)
	if !strings.Contains(errb.String(), "open the chat in LINE to sync") {
		t.Errorf("--until inside the unsynced span printed %q", errb.String())
	}
	errb.Reset()
	rc.noteStale(&line.Freshness{ChatID: "c1", LastActivity: base, NewestStored: base}, 0, hintRead)
	rc.noteStale(nil, 0, hintRead)
	if errb.Len() != 0 {
		t.Errorf("fresh chat printed %q", errb.String())
	}
	if s := staleNote("u1", line.TimeISO(base), "", hintRead); !strings.HasSuffix(s, "but no stored messages; open the chat in LINE to sync") {
		t.Errorf("empty chat note %q", s)
	}
	// Opening a thread in LINE does not store its replies; read gets them from
	// LINE's server, and so does it an open chat's missing messages.
	for _, c := range []struct {
		chat string
		h    serverHint
		want string
	}{
		{"t1", hintRead, "thread replies are not stored on this PC; 'line read t1' reads them from LINE's server"},
		{"t1", hintOffline, "thread replies are not stored on this PC; without --offline, line-cli reads them from LINE's server"},
		{"t1", hintNone, "thread replies are not stored on this PC"},
		{"m1", hintRead, "open the chat in LINE to sync, or 'line read m1' reads the missing messages from LINE's server"},
		{"m1", hintOffline, "open the chat in LINE to sync, or without --offline, line-cli reads the missing messages from LINE's server"},
		{"m1", hintNone, "open the chat in LINE to sync"},
		{"c1", hintOffline, "open the chat in LINE to sync"},
	} {
		s := staleNote(c.chat, line.TimeISO(base), line.TimeISO(base-minute), c.h)
		if !strings.HasPrefix(s, "note: ") || !strings.HasSuffix(s, c.want) {
			t.Errorf("%s %d: %q", c.chat, c.h, s)
		}
		if line.IsThreadChat(c.chat) && (!strings.HasPrefix(s, "note: thread chat t1 has activity up to") || strings.Contains(s, "open the chat")) {
			t.Errorf("thread note %q", s)
		}
	}
	// With nothing stored, a range ending long before the last activity is
	// not flagged; one reaching it is.
	empty := &line.Freshness{ChatID: "u1", LastMissing: true, LastActivity: base}
	errb.Reset()
	rc.noteStale(empty, (base-99*minute)/1000, hintRead)
	if errb.Len() != 0 {
		t.Errorf("empty chat, old range: %q", errb.String())
	}
	rc.noteStale(empty, base/1000, hintRead)
	if !strings.Contains(errb.String(), "no stored messages") {
		t.Errorf("empty chat, range reaching the last activity: %q", errb.String())
	}
}

func TestHistoryNote(t *testing.T) {
	useEdition(t, "full")
	f := &line.Freshness{ChatID: "c0000000001", OldestStored: base, OldestID: "x1", NewestStored: base + 99*minute, LastActivity: base + 99*minute}
	msgs := []line.Message{{ID: "x1"}, {ID: "x2"}}
	later := []line.Message{{ID: "x5"}, {ID: "x6"}}
	for _, c := range []struct {
		name         string
		f            *line.Freshness
		msgs         []line.Message
		since, until int64
		want         string
	}{
		{"empty range before history", f, nil, 0, (base - minute) / 1000, "has no stored messages in this range; its stored messages span " + line.TimeISO(base) + " to " + line.TimeISO(base+99*minute) + "; older history is not on this PC"},
		{"empty range after history", f, nil, (base + 200*minute) / 1000, 0, "span " + line.TimeISO(base) + " to " + line.TimeISO(base+99*minute)},
		{"empty gap inside history", f, nil, (base + 10*minute) / 1000, (base + 20*minute) / 1000, "has no stored messages in this range"},
		{"since before history", f, msgs, (base - 99*minute) / 1000, 0, "reached the oldest message stored for chat c0000000001 (" + line.TimeISO(base) + "); older history is not on this PC"},
		{"no range reaching the start", f, msgs, 0, 0, "reached the oldest message stored"},
		{"no range, newer page", f, later, 0, 0, ""},
		{"since at the oldest second", f, msgs, base / 1000, 0, ""},
		{"nothing stored", &line.Freshness{ChatID: "u1"}, nil, 0, 0, "has no messages stored on this PC"},
		{"nothing stored but stale", &line.Freshness{ChatID: "u1", LastMissing: true, LastActivity: base}, nil, 0, 0, ""},
		{"nothing stored, range before the activity", &line.Freshness{ChatID: "u1", LastMissing: true, LastActivity: base}, nil, 0, (base - 99*minute) / 1000, "has no messages stored on this PC"},
		{"no state", nil, nil, 0, 0, ""},
		{"thread, nothing stored", &line.Freshness{ChatID: "t1"}, nil, 0, 0, "chat t1 has no messages stored on this PC; LINE for PC does not store thread replies: 'line read t1' reads them from LINE's server"},
	} {
		got := historyNote(c.f, c.msgs, c.since, c.until, hintRead)
		if strings.Contains(c.name, "after") || strings.Contains(c.name, "inside") {
			if strings.Contains(got, "older history") {
				t.Errorf("%s: %q claims missing older history", c.name, got)
			}
		}
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
		if got != "" && !strings.HasPrefix(got, "note: ") {
			t.Errorf("%s: %q lacks the note prefix", c.name, got)
		}
	}
}

func TestNoteLineStopped(t *testing.T) {
	defer func(f func() string) { stoppedDataAsOf = f }(stoppedDataAsOf)
	var errb bytes.Buffer
	rc := &runCtx{out: &bytes.Buffer{}, err: &errb}
	stoppedDataAsOf = func() string { return "" }
	rc.noteLineStopped()
	if errb.Len() != 0 {
		t.Fatalf("LINE running printed %q", errb.String())
	}
	if rc.dataAsOf != "" {
		t.Fatal("dataAsOf set while LINE runs")
	}
	stoppedDataAsOf = func() string { return "2026-01-02T03:04:05+08:00" }
	rc.noteLineStopped()
	if errb.String() != "line: note: LINE is not running; data is as of 2026-01-02T03:04:05+08:00\n" || rc.dataAsOf != "2026-01-02T03:04:05+08:00" {
		t.Fatalf("got %q, dataAsOf %q", errb.String(), rc.dataAsOf)
	}
}

func TestHelpFreshness(t *testing.T) {
	doc := helpDoc()
	if len(doc.Commands) < 3 || doc.Commands[1].Name != "version" || doc.Commands[2].Name != "status" {
		t.Fatal("status must follow version in help")
	}
	byName := map[string]cmdDoc{}
	for _, c := range doc.Commands {
		byName[c.Name] = c
	}
	for _, k := range []string{"line_running", "db_modified_at", "wal_modified_at", "key_cached", "newest_message_at", "oldest_message_at", "stale_chats"} {
		if !strings.Contains(byName["status"].Output, k) {
			t.Errorf("status output misses %s", k)
		}
	}
	u := byName["unread"]
	if !strings.Contains(u.Usage, "--include-hidden") || !strings.Contains(strings.Join(u.Flags, " "), "--include-hidden") {
		t.Error("unread does not document --include-hidden")
	}
	for _, k := range []string{"boundary", "stale", "last_activity_at", "newest_stored_at", "muted", "hidden", "first_unread", "read_marker", "unread_count", "not_stored", "LINE running"} {
		if !strings.Contains(u.Output+u.Summary, k) {
			t.Errorf("unread help misses %s", k)
		}
	}
	for _, k := range []string{"LINE is not running", "open the chat in LINE to sync", "thread replies are not stored on this PC", "older history is not on this PC", "line status"} {
		if !strings.Contains(doc.Fresh, k) {
			t.Errorf("freshness help misses %q", k)
		}
	}
	if strings.Contains(doc.Fresh, "any command") {
		t.Error("the not-running note is only printed by commands that read the database")
	}
}

func TestStatusArgsAndNoLINEData(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	for _, c := range []struct {
		args []string
		code string
	}{
		{[]string{"status", "extra"}, "INVALID_ARGUMENT"},
		{[]string{"status", "--bogus"}, "INVALID_ARGUMENT"},
		{[]string{"status"}, "DB_NOT_FOUND"},
		{[]string{"unread", "--include-hidden", "--limit-chats", "-1"}, "INVALID_ARGUMENT"},
	} {
		rc, out := runCLI(c.args...)
		var e errorOutput
		if rc != 1 || json.Unmarshal(out, &e) != nil || e.Error != c.code {
			t.Errorf("%v: rc=%d out=%s", c.args, rc, out)
		}
	}
}

// With no cached key, status must report key_cached:false and stop there
// rather than scan LINE's memory.
func TestStatusWithoutCachedKey(t *testing.T) {
	la := t.TempDir()
	t.Setenv("LOCALAPPDATA", la)
	t.Setenv("USERPROFILE", t.TempDir())
	dir := filepath.Join(la, "LINE", "Data", "db")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fake.edb"), bytes.Repeat([]byte{0x5a}, 8192), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, out := runCLI("status")
	var s map[string]any
	if rc != 0 || json.Unmarshal(out, &s) != nil {
		t.Fatalf("rc=%d out=%s", rc, out)
	}
	if s["key_cached"] != false || s["db_size"] != float64(8192) || s["db_modified_at"] == "" || s["note"] == "" {
		t.Errorf("status %v", s)
	}
	if _, ok := s["line_running"].(bool); !ok {
		t.Error("line_running missing")
	}
	for _, k := range []string{"newest_message_at", "oldest_message_at", "stale_chats", "wal_modified_at"} {
		if _, ok := s[k]; ok {
			t.Errorf("%s present without a cached key or WAL", k)
		}
	}
	if _, err := os.Stat(filepath.Join(la, "linecli")); !os.IsNotExist(err) {
		t.Error("status created the key store")
	}
}
