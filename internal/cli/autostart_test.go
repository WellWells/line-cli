package cli

import (
	"os"
	"testing"

	"github.com/WellWells/line-cli/internal/line"
)

func init() {
	// A test that reaches the real line.Open must not start the real LINE.
	os.Setenv("LINECLI_NO_AUTOSTART", "1")
}

func TestOfflineNeverStartsLINE(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	old := openDB
	t.Cleanup(func() { openDB = old })
	var mayStart []bool
	openDB = func(_ func(string), start bool) (*line.DB, error) {
		mayStart = append(mayStart, start)
		return nil, &line.Error{Code: line.ErrLineNotRunning, Message: "LINE is not running."}
	}
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"read", "someone", "--offline"}, false},
		{[]string{"unread", "--offline"}, false},
		{[]string{"read", "someone"}, true},
		{[]string{"unread"}, true},
		{[]string{"chats"}, true},
	}
	for _, c := range cases {
		mayStart = nil
		if code, out := runCLI(c.args...); code == 0 {
			t.Errorf("%v: rc=0 out=%s", c.args, out)
		}
		if len(mayStart) != 1 || mayStart[0] != c.want {
			t.Errorf("%v: mayStart = %v, want %v", c.args, mayStart, c.want)
		}
	}
}
