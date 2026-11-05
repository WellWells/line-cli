package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WellWells/line-cli/internal/line"
)

// These run without LINE: what JSON output leaves out to save an agent tokens.

func senderIDs(t *testing.T, b []byte) []string {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(b, &rows); err != nil {
		t.Fatalf("not a JSON list: %v: %s", err, b)
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i], _ = r["sender_id"].(string)
	}
	return ids
}

// sender_id stays only on rows whose name two different senders share.
func TestSenderIDOnlyWhenNameShared(t *testing.T) {
	msgs := func() []line.Message {
		return []line.Message{
			{ID: "1", Sender: "Ann", SenderID: "u1"},
			{ID: "2", Sender: "Bob", SenderID: "u2"},
			{ID: "3", Sender: "Ann", SenderID: "u1"},
			{ID: "4", Sender: "Amy", SenderID: "u3"},
			{ID: "5", Sender: "Amy", SenderID: "u4"},
		}
	}
	var out bytes.Buffer
	rc := &runCtx{out: &out, err: &bytes.Buffer{}}
	if rc.emitMessages(msgs(), false) != 0 {
		t.Fatal("emitMessages failed")
	}
	if got, want := strings.Join(senderIDs(t, out.Bytes()), ","), ",,,u3,u4"; got != want {
		t.Errorf("sender_ids = %q, want %q", got, want)
	}

	out.Reset()
	rc = &runCtx{out: &out, err: &bytes.Buffer{}, ids: true}
	rc.emitMessages(msgs(), false)
	if got, want := strings.Join(senderIDs(t, out.Bytes()), ","), "u1,u2,u1,u3,u4"; got != want {
		t.Errorf("--ids sender_ids = %q, want %q", got, want)
	}
}

// The rule spans the whole output: the same name in two chats of a search or
// unread result keeps its ids when they are different people.
func TestSenderIDSharedAcrossChats(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, err: &bytes.Buffer{}}
	rc.emitSearch([]line.SearchHit{
		{ChatID: "c1", Message: line.Message{ID: "1", Sender: "Ann", SenderID: "u1"}},
		{ChatID: "c2", Message: line.Message{ID: "2", Sender: "Ann", SenderID: "u9"}},
		{ChatID: "c2", Message: line.Message{ID: "3", Sender: "Bob", SenderID: "u2"}},
	})
	if got, want := strings.Join(senderIDs(t, out.Bytes()), ","), "u1,u9,"; got != want {
		t.Errorf("search sender_ids = %q, want %q", got, want)
	}

	out.Reset()
	rc.emitUnread([]line.UnreadChat{
		{ChatID: "c1", Messages: []line.Message{{ID: "1", Sender: "Ann", SenderID: "u1"}}},
		{ChatID: "c2", Messages: []line.Message{{ID: "2", Sender: "Ann", SenderID: "u9"}, {ID: "3", Sender: "Bob", SenderID: "u2"}}},
	})
	var chats []struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(out.Bytes(), &chats); err != nil || len(chats) != 2 {
		t.Fatalf("unread: %v: %s", err, out.Bytes())
	}
	if chats[0].Messages[0]["sender_id"] != "u1" || chats[1].Messages[0]["sender_id"] != "u9" || chats[1].Messages[1]["sender_id"] != nil {
		t.Errorf("unread sender_ids: %s", out.Bytes())
	}
}

// read and unread keep a message's id only when another message in the output
// replies to it; read --around (withIDs) and --ids keep every id.
func TestMessageIDOnlyWhenRepliedTo(t *testing.T) {
	msgs := func() []line.Message {
		return []line.Message{
			{ID: "100", Sender: "Ann", SenderID: "u1"},
			{ID: "101", Sender: "Bob", SenderID: "u2", ReplyTo: "100"},
			{ID: "102", Sender: "Ann", SenderID: "u1", ReplyTo: "99"},
		}
	}
	ids := func(b []byte) string {
		var rows []map[string]any
		if err := json.Unmarshal(b, &rows); err != nil {
			t.Fatalf("not a JSON list: %v: %s", err, b)
		}
		s := make([]string, len(rows))
		for i, r := range rows {
			s[i], _ = r["id"].(string)
		}
		return strings.Join(s, ",")
	}
	var out bytes.Buffer
	rc := &runCtx{out: &out, err: &bytes.Buffer{}}
	rc.emitMessages(msgs(), false)
	if got := ids(out.Bytes()); got != "100,," {
		t.Errorf("read ids = %q, want %q", got, "100,,")
	}
	if !strings.Contains(out.String(), `"reply_to":"99"`) {
		t.Errorf("reply_to outside the output dropped: %s", out.String())
	}
	out.Reset()
	rc.emitMessages(msgs(), true)
	if got := ids(out.Bytes()); got != "100,101,102" {
		t.Errorf("--around ids = %q", got)
	}
	out.Reset()
	(&runCtx{out: &out, err: &bytes.Buffer{}, ids: true}).emitMessages(msgs(), false)
	if got := ids(out.Bytes()); got != "100,101,102" {
		t.Errorf("--ids ids = %q", got)
	}
	out.Reset()
	rc.emitUnread([]line.UnreadChat{{ChatID: "c1", Messages: msgs()}})
	if strings.Count(out.String(), `"id":`) != 1 {
		t.Errorf("unread ids: %s", out.String())
	}
	// search hits keep their ids: the next step is read --around <id>.
	out.Reset()
	rc.emitSearch([]line.SearchHit{{ChatID: "c1", Message: line.Message{ID: "7", Sender: "Ann", SenderID: "u1"}}})
	if !strings.Contains(out.String(), `"id":"7"`) {
		t.Errorf("search hit lost its id: %s", out.String())
	}
}

// find-file, preview and download rows follow the same rule.
func TestSenderIDInFileRows(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, err: &bytes.Buffer{}}
	rc.emitFiles([]line.FileHit{{ID: "1", Sender: "Ann", SenderID: "u1"}, {ID: "2", Sender: "Ann", SenderID: "u1"}})
	rc.emitPreview([]line.PreviewResult{{ID: "1", Sender: "Ann", SenderID: "u1"}})
	rc.emitDownload([]line.DownloadResult{{ID: "1", Sender: "Ann", SenderID: "u1"}})
	if strings.Contains(out.String(), "sender_id") {
		t.Errorf("unshared name kept sender_id: %s", out.String())
	}
}

// JSON is indented only for a terminal; pipes, files and --out get one line.
func TestJSONIndentOnlyOnTerminal(t *testing.T) {
	var out bytes.Buffer
	if code := Run([]string{"help", "--json"}, &out, &bytes.Buffer{}); code != 0 || bytes.Count(out.Bytes(), []byte("\n")) != 1 {
		t.Errorf("piped help: code=%d out=%q", code, out.String())
	}

	// Stand in for a console: out plays stdout on a terminal.
	defer func(f func(io.Writer) bool) { isTerminal = f }(isTerminal)
	isTerminal = func(w io.Writer) bool { return w == io.Writer(&out) }
	out.Reset()
	Run([]string{"help", "--json"}, &out, &bytes.Buffer{})
	if !strings.Contains(out.String(), "\n  \"version\"") {
		t.Errorf("terminal help not indented: %q", out.String())
	}
	out.Reset()
	Run([]string{"help", "--json", "--compact"}, &out, &bytes.Buffer{})
	if bytes.Count(out.Bytes(), []byte("\n")) != 1 {
		t.Errorf("--compact on a terminal: %q", out.String())
	}

	// --out FILE is a file, so it is compact even when stdout is a terminal;
	// the summary on stdout is for whoever runs the command.
	file := filepath.Join(t.TempDir(), "v.json")
	out.Reset()
	if code := Run([]string{"help", "--json", "--out", file}, &out, &bytes.Buffer{}); code != 0 {
		t.Fatalf("--out: code=%d out=%q", code, out.String())
	}
	b, err := os.ReadFile(file)
	if err != nil || bytes.Count(b, []byte("\n")) != 1 {
		t.Errorf("--out file: %v %q", err, b)
	}
	if !strings.Contains(out.String(), "\n  \"saved_as\"") {
		t.Errorf("--out summary on a terminal not indented: %q", out.String())
	}
}

// A real console counts as a terminal; a buffer and a regular file do not.
func TestIsTerminal(t *testing.T) {
	if isTerminal(&bytes.Buffer{}) {
		t.Error("buffer is a terminal")
	}
	f, err := os.Create(filepath.Join(t.TempDir(), "f"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isTerminal(f) {
		t.Error("regular file is a terminal")
	}
}
