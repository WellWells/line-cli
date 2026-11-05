package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// These run without LINE: they only exercise argument handling and help.

func TestArgErrorsBeforeOpeningDB(t *testing.T) {
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"chats", "--chat-type", "bogus"}, "--chat-type"},
		{[]string{"chats", "--type", "bogus"}, "--chat-type"},
		{[]string{"download"}, "--id"},
		{[]string{"read"}, "chat_id"},
	} {
		rc, out := runCLI(c.args...)
		var e errorOutput
		if rc != 1 || json.Unmarshal(out, &e) != nil || e.Error != "INVALID_ARGUMENT" || !strings.Contains(e.Message, c.msg) {
			t.Errorf("%v: rc=%d out=%s", c.args, rc, out)
		}
	}
}

func TestHelpDocumentsIDsAndPaging(t *testing.T) {
	doc := map[string]cmdDoc{}
	for _, c := range helpDoc().Commands {
		doc[c.Name] = c
	}
	if u := doc["download"].Usage; !strings.Contains(u, "(<chat_id|name> | --id MSGID)") {
		t.Errorf("download usage does not show chat or --id as alternatives: %s", u)
	}
	if !strings.Contains(strings.Join(doc["chats"].Flags, " "), "--chat-type") {
		t.Errorf("chats flags missing --chat-type: %v", doc["chats"].Flags)
	}
	for _, name := range []string{"read", "search", "unread"} {
		if !strings.Contains(doc[name].Output, "id, type") {
			t.Errorf("%s output does not list id: %s", name, doc[name].Output)
		}
	}
	if !strings.Contains(helpDoc().Output, "note:") {
		t.Error("help output does not mention the limit note")
	}
}

func TestNoteLimit(t *testing.T) {
	var errb bytes.Buffer
	rc := &runCtx{out: &bytes.Buffer{}, err: &errb}
	rc.noteLimit(4, 5, "x")
	if errb.Len() != 0 {
		t.Fatalf("below limit printed %q", errb.String())
	}
	rc.noteLimit(5, 5, "rerun with --since <last sent_at>")
	if s := errb.String(); !strings.HasPrefix(s, "line: note: returned 5 = limit 5, more may exist; rerun with --since") {
		t.Fatalf("at limit printed %q", s)
	}
}

func TestNotePage(t *testing.T) {
	for _, c := range []struct {
		n, limit    int
		first, last string
		want        string
	}{
		{2, 3, "a", "b", ""},
		{2, 3, "2026-01-01T00:00:05+08:00", "2026-01-01T00:00:05+08:00", ""},
		{0, 3, "", "", ""},
		{3, 3, "2026-01-01T00:00:00+08:00", "2026-01-01T00:00:05+08:00", "more may exist; older: x" + boundaryRepeats},
		{3, 3, "2026-01-01T00:00:05+08:00", "2026-01-01T00:00:05+08:00", "all sent at 2026-01-01T00:00:05+08:00; paging by sent_at cannot pass this second, rerun with a larger --limit"},
		{1, 1, "2026-01-01T00:00:05+08:00", "2026-01-01T00:00:05+08:00", "rerun with a larger --limit"},
	} {
		var errb bytes.Buffer
		rc := &runCtx{out: &bytes.Buffer{}, err: &errb}
		rc.notePage(c.n, c.limit, c.first, c.last, "older: x")
		got := errb.String()
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("notePage(%d, %d, %q, %q) printed %q, want %q", c.n, c.limit, c.first, c.last, got, c.want)
		}
	}
}
