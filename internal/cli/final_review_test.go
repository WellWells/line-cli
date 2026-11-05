package cli

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/WellWells/line-cli/internal/line"
)

// Synthetic rows only: fake chat ids, mids and texts.
const (
	finalChatStale = "c00000000000000000000000000000f01"
	finalChatFresh = "c00000000000000000000000000000f02"
	finalMidA      = "u00000000000000000000000000000f11"
	finalMidMe     = "u00000000000000000000000000000f12"
)

// finalScript: the stale chat's last activity (a message that is not stored)
// is an hour after its newest stored message.
const finalScript = `
INSERT INTO _chat (_id, _midType, _lastUpdatedTime, _unreadCount, _status, _lastMessage) VALUES
 ('` + finalChatStale + `', 2, 1700003600000, 0, 0, '{"id":"gone","createdTime":1700003600000}'),
 ('` + finalChatFresh + `', 2, 1700000002000, 0, 0, '{"id":"f2","createdTime":1700000002000}');
INSERT INTO _groupChat (_type, _chatMid, _chatName, _status) VALUES (0, '` + finalChatStale + `', 'Stale Group', 0), (0, '` + finalChatFresh + `', 'Fresh Group', 0);
INSERT INTO _contact (_mid, _type, _status, _displayName, _displayNameOverridden) VALUES ('` + finalMidA + `', 0, 1, 'Alice', '');
INSERT INTO _profile (_mid, _displayName) VALUES ('` + finalMidMe + `', 'Me');
INSERT INTO _message (_id, _chatId, _from, _toType, _createdTime, _text, _contentType, _type, _status, _attribute) VALUES
 ('s1', '` + finalChatStale + `', '` + finalMidA + `', 2, 1700000000000, 'one', 0, 1, 1, 0),
 ('s2', '` + finalChatStale + `', '` + finalMidMe + `', 2, 1700000001000, 'two', 0, 1, 1, 0),
 ('f1', '` + finalChatFresh + `', '` + finalMidA + `', 2, 1700000001000, 'three', 0, 1, 1, 0),
 ('f2', '` + finalChatFresh + `', '` + finalMidA + `', 2, 1700000002000, 'four', 0, 1, 1, 0);
`

// stats counts only stored messages, so like read it flags a chat LINE has
// not finished syncing, and says when the range holds nothing stored.
func TestStatsFreshnessNotes(t *testing.T) {
	useSyntheticDB(t, finalScript)
	for _, c := range []struct {
		args []string
		want string // "" = no note at all
	}{
		{[]string{"stats", finalChatStale}, "open the chat in LINE to sync"},
		{[]string{"stats", finalChatStale, "--by", "day"}, "open the chat in LINE to sync"},
		{[]string{"stats", finalChatStale, "--until", "1700000001"}, ""},
		{[]string{"stats", finalChatFresh}, ""},
		{[]string{"stats", finalChatFresh, "--until", "1600000000"}, "has no stored messages in this range"},
	} {
		rc, out, errs := runCLIErr(c.args...)
		var res struct{ Total int }
		if rc != 0 || json.Unmarshal(out, &res) != nil {
			t.Fatalf("%v: rc=%d %s", c.args, rc, out)
		}
		if c.want == "" && strings.Contains(errs, "note:") || c.want != "" && !strings.Contains(errs, c.want) {
			t.Errorf("%v: stderr %q, want %q", c.args, errs, c.want)
		}
	}
}

// Commands without a chat argument refuse stray words instead of ignoring
// them, and --chat-type is not silently dropped next to --chat.
func TestFinalArgumentChecks(t *testing.T) {
	useSyntheticDB(t, finalScript)
	oldInit, oldForget := initKey, forgetKey
	t.Cleanup(func() { initKey, forgetKey = oldInit, oldForget })
	initKey = func(func(string)) error { t.Fatal("init ran"); return nil }
	forgetKey = func() error { t.Fatal("init --forget ran"); return nil }
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"unread", finalChatStale}, "unread takes no chat"},
		{[]string{"version", "extra"}, "version takes no arguments"},
		{[]string{"init", "extra"}, "init takes no arguments"},
		{[]string{"init", "--forget", "extra"}, "init takes no arguments"},
		{[]string{"search", "x", "--chat", finalChatStale, "--chat-type", "open"}, "--chat and --chat-type cannot be combined"},
		{[]string{"search", "--chat-type", "group", "--chat", finalChatStale}, "--chat and --chat-type cannot be combined"},
		{[]string{"find-file", "--chat", finalChatStale, "--chat-type", "group"}, "--chat and --chat-type cannot be combined"},
	} {
		rc, out, _ := runCLIErr(c.args...)
		var e errorOutput
		if rc != 1 || json.Unmarshal(out, &e) != nil || e.Error != "INVALID_ARGUMENT" || !strings.Contains(e.Message, c.want) {
			t.Errorf("%v: rc=%d %s", c.args, rc, out)
		}
	}
	for _, args := range [][]string{{"unread"}, {"version"}, {"search", "--chat-type", "group"}, {"search", "--chat", finalChatStale}, {"find-file", "--chat-type", "group"}} {
		if rc, out, _ := runCLIErr(args...); rc != 0 {
			t.Errorf("%v: rc=%d %s", args, rc, out)
		}
	}
}

// Git Bash rewrites a --message starting with '/' into a Windows path before
// line-cli sees it: help says so, and send warns when the text looks rewritten.
// Only dry runs on a synthetic database; nothing is sent.
func TestSendGitBashPathWarning(t *testing.T) {
	useSyntheticDB(t, finalScript)
	const warn = "Git Bash (MSYS) rewrites"
	for _, c := range []struct {
		msystem, text string
		want          bool
	}{
		{"MINGW64", "C:/Program Files/Git/hello there", true},
		{"MINGW64", `d:\x`, true},
		{"MINGW64", "/hello", false},
		{"MINGW64", "C: not a path", false},
		{"", "C:/Program Files/Git/hello there", false},
	} {
		t.Setenv("MSYSTEM", c.msystem)
		_, _, errs := runCLIErr("send", "--to", finalMidA, "--message", c.text, "--dry-run")
		if strings.Contains(errs, warn) != c.want {
			t.Errorf("MSYSTEM=%q %q: stderr %q", c.msystem, c.text, errs)
		}
	}
	for _, args := range [][]string{{"help", "semantics"}, {"help", "send", "--json"}} {
		_, out, _ := runCLIErr(args...)
		if !strings.Contains(string(out), "MSYS_NO_PATHCONV=1") || !strings.Contains(string(out), "starting with '/'") {
			t.Errorf("%v does not explain Git Bash path rewriting", args)
		}
	}
	rc, out, _ := runCLIErr("send", "--to", finalMidA, "--message", "a\x1b[2Jb", "--dry-run")
	var e errorOutput
	if rc != 1 || json.Unmarshal(out, &e) != nil || e.Error != "INVALID_ARGUMENT" || !strings.Contains(e.Message, "U+001B") {
		t.Errorf("control character: rc=%d %s", rc, out)
	}
}

// withReader reads again after DB_UNREADABLE, but not when the schema changed.
func TestWithReaderSkipsRetryOnSchemaChange(t *testing.T) {
	useSyntheticDB(t, finalScript+"DROP TABLE _contact;")
	rc := &runCtx{cmd: "contacts", err: io.Discard}
	calls := 0
	err := rc.withReader(func(r *line.Reader) error {
		calls++
		_, err := r.ListContacts(line.ContactsOptions{})
		return err
	})
	if le, ok := line.AsError(err); !ok || !le.SchemaChanged() || calls != 1 {
		t.Errorf("schema change: %v after %d reads", err, calls)
	}
	calls = 0
	err = rc.withReader(func(*line.Reader) error {
		calls++
		return &line.Error{Code: line.ErrDBUnreadable, Message: "synthetic"}
	})
	if calls != 2 || err == nil {
		t.Errorf("other DB_UNREADABLE: %v after %d reads", err, calls)
	}
}

// --type link keeps its old meaning (post) with a note; url is the URL filter.
func TestSearchTypeLinkIsPost(t *testing.T) {
	useSyntheticDB(t, finalScript)
	_, _, errs := runCLIErr("search", "--type", "link")
	if !strings.Contains(errs, "--type link is the older name of --type post") {
		t.Errorf("--type link: stderr %q", errs)
	}
	for _, typ := range []string{"post", "url"} {
		if rc, out, errs := runCLIErr("search", "--type", typ); rc != 0 || strings.Contains(errs, "older name") {
			t.Errorf("--type %s: rc=%d %s %q", typ, rc, out, errs)
		}
	}
}
