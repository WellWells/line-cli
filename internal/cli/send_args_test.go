package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These fail before the DB is opened, so they run without LINE.

func TestSendArgErrorsBeforeOpeningDB(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("LOCALAPPDATA", appData)
	dir := t.TempDir()
	badFile := filepath.Join(dir, "bad.txt")
	os.WriteFile(badFile, []byte("\xff\xfebad"), 0o600)
	emptyFile := filepath.Join(dir, "empty.txt")
	os.WriteFile(emptyFile, []byte("\xef\xbb\xbf\r\n"), 0o600)
	ansiFile := filepath.Join(dir, "ansi.txt")
	os.WriteFile(ansiFile, []byte("\xa4\xa4\xa4\xe5"), 0o600)
	loneSurrogate := filepath.Join(dir, "surrogate.txt")
	os.WriteFile(loneSurrogate, []byte("\xff\xfeh\x00\x00\xd8i\x00"), 0o600)
	longFile := filepath.Join(dir, "long.txt")
	os.WriteFile(longFile, []byte(strings.Repeat("測", 10001)), 0o600)
	const to = "u0000000000000000000000000000000f"
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"send", "--to", to, "--message", "hello", "world", "--dry-run"}, "unexpected argument"},
		{[]string{"send", "--to", "first", "last", "--message", "hi", "--dry-run"}, "quote multi-word values (the --to name"},
		{[]string{"send", "--to", to, "--message", "hi", "--dry-run", "--", "-y"}, "unexpected argument"},
		{[]string{"send", "--to", to, "--message", "hi", "--stdin", "--dry-run"}, "only one of"},
		{[]string{"send", "--to", to, "--message", "hi", "--message-file", badFile, "--dry-run"}, "only one of"},
		{[]string{"send", "--to", to, "--stdin", "--message-file", badFile, "--dry-run"}, "only one of"},
		{[]string{"send", "--to", to, "--dry-run"}, "requires --message"},
		{[]string{"send", "--message", "hi", "--dry-run"}, "requires --to"},
		{[]string{"send", "--to", to, "--to", "u00000000000000000000000000000001", "--message", "hi", "--dry-run"}, "more than once"},
		{[]string{"send", "--to", to, "--message", "a", "--message", "b", "--dry-run"}, "more than once"},
		{[]string{"send", "--to", to, "--message", "hi", "--dry-run", "--dry-run"}, "more than once"},
		{[]string{"send", "--dry-run", "--to", to, "--message", "hi", "--dry-run=false"}, "more than once"},
		{[]string{"send", "--to", to, "--message", "hi", "--dry-run=false"}, "takes no value"},
		{[]string{"send", "--to", to, "--message", "hi", "--dry-run=true"}, "takes no value"},
		{[]string{"send", "--to", to, "--message", "hi", "-y=true"}, "takes no value"},
		{[]string{"send", "--to", to, "--message", "hi", "--yes=1"}, "takes no value"},
		{[]string{"send", "--to", to, "--message", "hi", "--allow-duplicate=true", "--dry-run"}, "takes no value"},
		{[]string{"send", "--to", to, "--message", "hi"}, "refusing to send without confirmation"},
		{[]string{"send", "--to", to, "--message", "hi", "--reply-to", "abc", "--dry-run"}, "message id"},
		{[]string{"send", "--to", to, "--file", badFile, "--message", "hi", "--mention", "Alex", "--dry-run"}, "separate command"},
		{[]string{"send", "--to", to, "--file", badFile, "--message", "hi", "--mention-all", "--dry-run"}, "separate command"},
		{[]string{"send", "--to", to, "--message", "hi", "--mention", "", "--dry-run"}, "needs a member"},
		{[]string{"send", "--to", to, "--message", "hi", "--mention-all=true", "--dry-run"}, "takes no value"},
		{[]string{"send", "--to", to, "--message", "hi", "--reply-to", "", "--dry-run"}, "message id"},
		{[]string{"send", "--to", to, "--message", "hi", "--reply-to", "1", "--reply-to", "2", "--dry-run"}, "more than once"},
		{[]string{"send", "--to", to, "--message", "", "--dry-run"}, "empty"},
		{[]string{"send", "--to", to, "--message", "bad \xc3\x28", "--dry-run"}, "UTF-8"},
		{[]string{"send", "--to", to, "--message-file", badFile, "--dry-run"}, "not valid UTF-16"},
		{[]string{"send", "--to", to, "--message-file", loneSurrogate, "--dry-run"}, "not valid UTF-16"},
		{[]string{"send", "--to", to, "--message-file", ansiFile, "--dry-run"}, "Set-Content -Encoding UTF8"},
		{[]string{"send", "--to", to, "--message-file", emptyFile, "--dry-run"}, "empty"},
		{[]string{"send", "--to", to, "--message-file", longFile, "--dry-run"}, "10001 characters"},
		{[]string{"send", "--to", to, "--message-file", filepath.Join(dir, "missing.txt"), "--dry-run"}, "cannot read --message-file"},
		{[]string{"send", "--to", to, "--message"}, "needs an argument"},
		{[]string{"read", "--limit"}, "needs an argument"},
		{[]string{"chats", "--type", "group", "--chat-type", "open"}, "more than once"},
		{[]string{"search", "--limit", "5", "--limit", "6", "x"}, "more than once"},
	} {
		rc, out := runCLI(c.args...)
		var e errorOutput
		if rc != 1 || json.Unmarshal(out, &e) != nil || e.Error != "INVALID_ARGUMENT" || !strings.Contains(e.Message, c.msg) {
			t.Errorf("%q: rc=%d out=%s, want INVALID_ARGUMENT containing %q", c.args, rc, out, c.msg)
		}
	}
	if left, _ := os.ReadDir(appData); len(left) != 0 {
		t.Errorf("argument errors wrote %d entries under LOCALAPPDATA", len(left))
	}
}

func TestParseArgsDoubleDash(t *testing.T) {
	for _, c := range []struct {
		args []string
		pos  []string
		lim  int
		flag bool
	}{
		{[]string{"--", "-1"}, []string{"-1"}, 0, false},
		{[]string{"--limit", "3", "--", "--flag", "-x"}, []string{"--flag", "-x"}, 3, false},
		{[]string{"text", "--limit", "4", "more"}, []string{"text", "more"}, 4, false},
		{[]string{"--b", "--", "--b"}, []string{"--b"}, 0, true},
		{[]string{"--limit=7", "-"}, []string{"-"}, 7, false},
	} {
		fs := newFlags("t")
		lim := fs.Int("limit", 0, "")
		b := fs.Bool("b", false, "")
		if err := parseArgs(fs, c.args); err != nil {
			t.Errorf("%q: %v", c.args, err)
			continue
		}
		if strings.Join(fs.Args(), "|") != strings.Join(c.pos, "|") || *lim != c.lim || *b != c.flag {
			t.Errorf("%q: args=%q limit=%d b=%v", c.args, fs.Args(), *lim, *b)
		}
	}
	fs := newFlags("t")
	fs.Bool("b", false, "")
	if err := parseArgs(fs, []string{"--b=false"}); err != nil {
		t.Errorf("bool value refused on a non-strict flag: %v", err)
	}
	fs = newFlags("t")
	fs.Bool("b", false, "")
	if err := parseArgs(fs, []string{"--b=false"}, "b"); err == nil {
		t.Error("bool value accepted on a strict flag")
	}
	fs = newFlags("t")
	fs.Func("f", "", func(string) error { return nil })
	if err := parseArgs(fs, []string{"-f", "1", "-f", "2"}); err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Errorf("repeated func flag: %v", err)
	}
}

func TestHelpDocumentsSend(t *testing.T) {
	var send cmdDoc
	for _, c := range helpDoc().Commands {
		if c.Name == "send" {
			send = c
		}
	}
	all := send.Usage + send.Summary + strings.Join(send.Flags, " ") + send.Output
	for _, k := range []string{
		"--message-file", "--stdin", "--dry-run", "-y", "--allow-duplicate",
		"server_message_id", "verified", "chat_type", "text_length", "text_sha256", "normalized",
		"SEND_FAILED", "SEND_UNCERTAIN", "DUPLICATE_SEND", "E2EE_DISABLED", "E2EE_KEY_MISSING",
		"one-to-one", "chat_id", "untrusted", "10000 Unicode characters", "send-log.jsonl",
	} {
		if !strings.Contains(all, k) {
			t.Errorf("send help lacks %q", k)
		}
	}
}
