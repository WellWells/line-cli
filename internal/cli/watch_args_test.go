package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/WellWells/line-cli/internal/line"
)

// Each message goes out alone, so "two senders share a name" can never show in
// one row: sender_id stays, and every row is one complete line.
func TestWatchRowsAreLinesWithSenderIDs(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, stdout: &out}
	rc.startWatch()
	hit := line.SearchHit{ChatID: "c1", ChatName: "Family", ChatType: "group", Message: line.Message{ID: "7", Type: "text", Sender: "Alex", SenderID: "uA"}}
	for range 2 {
		if rc.emitSearch([]line.SearchHit{hit}) != 0 {
			t.Fatal("emit failed")
		}
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("output %q", out.String())
	}
	var got line.SearchHit
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatal(err)
	}
	if got.ChatID != "c1" || got.ID != "7" || got.SenderID != "uA" {
		t.Errorf("row %+v", got)
	}
}

// These fail before the DB is opened, so they run without LINE.
func TestWatchArgErrorsBeforeOpeningDB(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	out := t.TempDir() + `\watch.jsonl`
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"watch", "--interval", "0"}, "--interval must be"},
		{[]string{"watch", "--interval", "3601"}, "--interval must be"},
		{[]string{"watch", "--count", "-1"}, "--count must not"},
		{[]string{"watch", "--timeout", "-1"}, "--timeout must not"},
		{[]string{"watch", "--include-official=true"}, "takes no value"},
		{[]string{"watch", "--out", out}, "cannot use --out"},
	} {
		rc, stdout, errs := runCLIErr(c.args...)
		if rc == 0 || !strings.Contains(string(stdout)+errs, c.msg) {
			t.Errorf("%v: exit %d, %s %s", c.args, rc, stdout, errs)
		}
	}
}
