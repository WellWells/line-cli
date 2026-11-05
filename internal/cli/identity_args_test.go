package cli

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/WellWells/line-cli/internal/line"
)

// These run without LINE: argument errors, help and error output only.

func TestIdentityArgErrors(t *testing.T) {
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"members"}, "chat_id"},
		{[]string{"members", "x", "--limit", "-1"}, "--limit"},
		{[]string{"contacts", "--limit", "-1"}, "--limit"},
		{[]string{"contacts", "--nope"}, "nope"},
		{[]string{"chats", "--chat-type", "bogus"}, "thread"},
		{[]string{"search", "--chat-type", "bogus"}, "self"},
	} {
		rc, out := runCLI(c.args...)
		var e map[string]any
		if rc != 1 || json.Unmarshal(out, &e) != nil || e["error"] != "INVALID_ARGUMENT" || !strings.Contains(e["message"].(string), c.msg) {
			t.Errorf("%v: rc=%d out=%s", c.args, rc, out)
		}
	}
}

func TestHelpIdentity(t *testing.T) {
	var names []string
	doc := map[string]cmdDoc{}
	for _, c := range helpDoc().Commands {
		names = append(names, c.Name)
		doc[c.Name] = c
	}
	if i := slices.Index(names, "members"); i < 0 || names[i-1] != "contacts" {
		t.Fatalf("members entry missing or not right after contacts: %v", names)
	}
	for _, typ := range line.ChatTypeNames() {
		for _, cmd := range []string{"chats", "search"} {
			if !strings.Contains(strings.Join(doc[cmd].Flags, " "), typ) {
				t.Errorf("%s --chat-type does not list %s", cmd, typ)
			}
		}
	}
	for _, cmd := range []string{"search", "find-file"} {
		if f := strings.Join(doc[cmd].Flags, " "); !strings.Contains(f, "--from me|id|name") || !strings.Contains(f, "SENDER_AMBIGUOUS") {
			t.Errorf("%s --from is not documented: %s", cmd, f)
		}
	}
	for cmd, fields := range map[string][]string{
		"chats":    {"parent_chat_id", "member_count", "hidden", "muted", "pinned"},
		"contacts": {"contact_id", "display_name", "is_me", "nickname", "chat_id", "type", "source", "community"},
		"members":  {"id", "name", "is_me", "role", "state", "invited", "joined_at", "chat_id", "message_count", "last_sent_at"},
	} {
		for _, f := range fields {
			if !strings.Contains(doc[cmd].Output, f) {
				t.Errorf("%s output does not list %s", cmd, f)
			}
		}
	}
	if !strings.Contains(doc["contacts"].Usage, "--include-members") || !strings.Contains(doc["read"].Summary, "note: resolved") {
		t.Error("contacts --include-members or the read resolve note is undocumented")
	}
}

func TestFailPrintsSenderCandidates(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, err: &bytes.Buffer{}}
	rc.fail(&line.Error{Code: line.ErrSenderAmbiguous, Message: "2 people match", Senders: []line.SenderCandidate{
		{ID: "u1", Name: "A", Source: "contact"}, {ID: "p2", Name: "A", Source: "open_chat_member", Community: "C"},
	}})
	var e struct {
		Error      string           `json:"error"`
		Candidates []map[string]any `json:"candidates"`
	}
	if json.Unmarshal(out.Bytes(), &e) != nil || e.Error != "SENDER_AMBIGUOUS" || len(e.Candidates) != 2 || e.Candidates[1]["community"] != "C" || e.Candidates[0]["source"] != "contact" {
		t.Fatalf("got %s", out.String())
	}
	out.Reset()
	rc.fail(&line.Error{Code: line.ErrChatAmbiguous, Message: "x", Candidates: []line.ChatSummary{{ChatID: "c1", Name: "n", Type: "group"}}})
	if s := out.String(); !strings.Contains(s, `"chat_id":"c1"`) || strings.Contains(s, "source") {
		t.Fatalf("chat candidates changed shape: %s", s)
	}
	out.Reset()
	rc.fail(&line.Error{Code: line.ErrChatNotFound, Message: "x"})
	if strings.Contains(out.String(), "candidates") {
		t.Fatalf("empty candidates printed: %s", out.String())
	}
}

func TestNoteResolved(t *testing.T) {
	var errb bytes.Buffer
	rc := &runCtx{out: &bytes.Buffer{}, err: &errb}
	c := line.ChatSummary{ChatID: "c1", Name: "Team", Type: "group"}
	rc.noteResolved("c1", c)
	if errb.Len() != 0 {
		t.Fatalf("exact chat_id printed %q", errb.String())
	}
	rc.noteResolved(" team ", c)
	if want := "line: note: resolved \"team\" -> c1 (group, Team)\n"; errb.String() != want {
		t.Fatalf("got %q, want %q", errb.String(), want)
	}
}
