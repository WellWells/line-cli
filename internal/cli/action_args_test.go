package cli

import (
	"strings"
	"testing"
)

// These fail before the DB is opened, so they run without LINE.
func TestActionArgErrorsBeforeOpeningDB(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	const to = "u0000000000000000000000000000000f"
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"react", "--id", "1", "--reaction", "love", "--dry-run"}, "requires --to"},
		{[]string{"react", "--to", to, "--reaction", "love", "--dry-run"}, "requires --id"},
		{[]string{"react", "--to", to, "--id", "x", "--reaction", "love", "--dry-run"}, "message id"},
		{[]string{"react", "--to", to, "--id", "1", "--dry-run"}, "--reaction <name> or --remove"},
		{[]string{"react", "--to", to, "--id", "1", "--reaction", "love", "--remove", "--dry-run"}, "--reaction <name> or --remove"},
		{[]string{"react", "--to", to, "--id", "1", "--reaction", "love"}, "refusing to react without confirmation"},
		{[]string{"react", "--to", to, "--id", "1", "--remove"}, "refusing to react without confirmation"},
		{[]string{"react", "--to", to, "--id", "1", "--remove=true", "--dry-run"}, "takes no value"},
		{[]string{"react", "--to", to, "--id", "1", "--reaction", "love", "extra", "--dry-run"}, "unexpected argument"},
		{[]string{"unsend", "--id", "1", "--dry-run"}, "requires --to"},
		{[]string{"unsend", "--to", to, "--dry-run"}, "requires --id"},
		{[]string{"unsend", "--to", to, "--id", "1"}, "refusing to unsend without confirmation"},
		{[]string{"unsend", "--to", to, "--id", "1", "--id", "2", "--dry-run"}, "more than once"},
		{[]string{"unsend", "--to", to, "--id", "1", "--dry-run", "--", "-y"}, "unexpected argument"},
	} {
		rc, out, errs := runCLIErr(c.args...)
		if rc == 0 || !strings.Contains(string(out)+errs, c.msg) {
			t.Errorf("%v: exit %d, %s %s", c.args, rc, out, errs)
		}
	}
}

func TestActionsAreNotRerun(t *testing.T) {
	for _, cmd := range []string{"react", "unsend"} {
		if rerunnable[cmd] {
			t.Errorf("%s must not be rerun", cmd)
		}
	}
}
