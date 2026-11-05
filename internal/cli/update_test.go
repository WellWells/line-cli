package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WellWells/line-cli/internal/line"
)

// fakeRelease replaces the GitHub lookup and counts the calls; tests never
// reach the network.
func fakeRelease(t *testing.T, version string, err error) *int {
	t.Helper()
	calls := 0
	saved := latestRelease
	latestRelease = func(string, time.Duration) (string, error) {
		calls++
		return version, err
	}
	t.Cleanup(func() { latestRelease = saved })
	return &calls
}

// updateSetup is a full release build on a clean machine.
func updateSetup(t *testing.T, current string) {
	t.Helper()
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("CI", "")
	t.Setenv("LINECLI_NO_UPDATE_NOTIFIER", "")
	useEdition(t, "full")
	saved := Version
	Version = current
	t.Cleanup(func() { Version = saved })
}

func day(n int) time.Time { return time.Date(2026, 11, n, 9, 0, 0, 0, time.Local) }

func TestNewerRelease(t *testing.T) {
	for _, c := range []struct {
		latest, current string
		want            bool
	}{
		{"1.1.0", "1.0.0", true},
		{"1.0.1", "1.0.0", true},
		{"2.0.0", "1.9.9", true},
		{"1.10.0", "1.9.0", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.1", "1.1.0", false},
		{"1.2.0", "dev", false},
		{"1.2.0", "0.20261031.1530", false},
		{"1.2.0-rc1", "1.0.0", false},
		{"1.2", "1.0.0", false},
		{"1.2.0.1", "1.0.0", false},
		{"", "1.0.0", false},
		{"1.2.x", "1.0.0", false},
	} {
		if got := newerRelease(c.latest, c.current); got != c.want {
			t.Errorf("newerRelease(%q, %q) = %v", c.latest, c.current, got)
		}
	}
}

func TestInstallCommand(t *testing.T) {
	plain := "irm https://github.com/" + updateRepo + "/releases/latest/download/install.ps1 | iex"
	store := `C:\Users\a\AppData\Local\linecli`
	for _, c := range []struct{ exe, want string }{
		{store + `\bin\linecli.exe`, plain},
		{`c:\users\a\appdata\local\LINECLI\BIN\linecli.exe`, plain},
		{`C:\repo\linecli.exe`, plain},
		{`D:\Tools\line\bin\linecli.exe`, `$env:LINECLI_INSTALL_DIR='D:\Tools\line'; ` + plain},
		{`D:\It's\bin\linecli.exe`, `$env:LINECLI_INSTALL_DIR='D:\It''s'; ` + plain},
	} {
		if got := installCommand(c.exe, store); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.exe, got, c.want)
		}
	}
}

func TestDailyNoteOncePerDay(t *testing.T) {
	updateSetup(t, "1.0.0")
	calls := fakeRelease(t, "1.1.0", nil)
	var errb bytes.Buffer
	dailyUpdateNote("read", false, &errb, day(5))
	note := errb.String()
	if *calls != 1 || !strings.HasPrefix(note, "line: note:") || strings.Count(note, "\n") != 1 ||
		!strings.Contains(note, "1.1.0") || !strings.Contains(note, "1.0.0") || !strings.Contains(note, "| iex") {
		t.Fatalf("calls %d, note %q", *calls, note)
	}
	errb.Reset()
	dailyUpdateNote("read", false, &errb, day(5).Add(3*time.Hour))
	if *calls != 1 || errb.Len() != 0 {
		t.Errorf("second run the same day: calls %d, note %q", *calls, errb.String())
	}
	dailyUpdateNote("read", false, &errb, day(6))
	if *calls != 2 || errb.Len() == 0 {
		t.Errorf("next day: calls %d, note %q", *calls, errb.String())
	}
}

func TestDailyNoteSilentWhenCurrent(t *testing.T) {
	updateSetup(t, "1.1.0")
	calls := fakeRelease(t, "1.1.0", nil)
	var errb bytes.Buffer
	dailyUpdateNote("read", false, &errb, day(5))
	if *calls != 1 || errb.Len() != 0 {
		t.Errorf("calls %d, note %q", *calls, errb.String())
	}
}

// A failed check says nothing and waits for tomorrow, so a blocked connection
// never costs two seconds on every command.
func TestDailyNoteSilentOnFailure(t *testing.T) {
	updateSetup(t, "1.0.0")
	calls := fakeRelease(t, "", errors.New("blocked"))
	var errb bytes.Buffer
	for range 3 {
		dailyUpdateNote("read", false, &errb, day(5))
	}
	if *calls != 1 || errb.Len() != 0 {
		t.Errorf("calls %d, note %q", *calls, errb.String())
	}
}

func TestDailyNoteSkips(t *testing.T) {
	for _, c := range []struct {
		name, cmd, version, edition string
		env                         string
		offline                     bool
	}{
		{name: "open build", cmd: "read", version: "1.0.0", edition: "open"},
		{name: "opted out", cmd: "read", version: "1.0.0", edition: "full", env: "LINECLI_NO_UPDATE_NOTIFIER"},
		{name: "CI", cmd: "read", version: "1.0.0", edition: "full", env: "CI"},
		{name: "offline", cmd: "read", version: "1.0.0", edition: "full", offline: true},
		{name: "development build", cmd: "read", version: "dev", edition: "full"},
		{name: "beta build", cmd: "read", version: "0.20261031.1530", edition: "full"},
		{name: "help", cmd: "help", version: "1.0.0", edition: "full"},
		{name: "version", cmd: "version", version: "1.0.0", edition: "full"},
		{name: "skill", cmd: "skill", version: "1.0.0", edition: "full"},
		{name: "uninstall", cmd: "uninstall", version: "1.0.0", edition: "full"},
		{name: "update", cmd: "update", version: "1.0.0", edition: "full"},
		{name: "watch", cmd: "watch", version: "1.0.0", edition: "full"},
		{name: "no command", cmd: "", version: "1.0.0", edition: "full"},
	} {
		t.Run(c.name, func(t *testing.T) {
			updateSetup(t, c.version)
			useEdition(t, c.edition)
			if c.env != "" {
				t.Setenv(c.env, "1")
			}
			calls := fakeRelease(t, "9.0.0", nil)
			var errb bytes.Buffer
			dailyUpdateNote(c.cmd, c.offline, &errb, day(5))
			if *calls != 0 || errb.Len() != 0 {
				t.Errorf("calls %d, note %q", *calls, errb.String())
			}
			if _, err := os.Stat(filepath.Join(line.StoreDir(), updateStampFile)); err == nil {
				t.Error("a skipped check left a stamp")
			}
		})
	}
}

func fakeCommand(t *testing.T, code int) {
	t.Helper()
	commands["update-test"] = func(*runCtx, []string) int { return code }
	t.Cleanup(func() { delete(commands, "update-test") })
}

func TestMainChecksOnlyAfterSuccess(t *testing.T) {
	updateSetup(t, "1.0.0")
	calls := fakeRelease(t, "1.1.0", nil)
	var out, errb bytes.Buffer
	fakeCommand(t, 1)
	if Main([]string{"update-test"}, &out, &errb) != 1 || *calls != 0 {
		t.Fatalf("after a failure: calls %d", *calls)
	}
	fakeCommand(t, 0)
	if Main([]string{"update-test"}, &out, &errb) != 0 || *calls != 1 {
		t.Fatalf("after a success: calls %d", *calls)
	}
	if !strings.Contains(errb.String(), "1.1.0") || out.Len() != 0 {
		t.Errorf("stdout %q, stderr %q", out.String(), errb.String())
	}
}

// Asking for a command's help is not using it.
func TestMainSkipsCommandHelp(t *testing.T) {
	updateSetup(t, "1.0.0")
	calls := fakeRelease(t, "1.1.0", nil)
	var out, errb bytes.Buffer
	if Main([]string{"read", "--help"}, &out, &errb) != 0 || *calls != 0 {
		t.Fatalf("calls %d", *calls)
	}
}

// However --offline was written, it counts.
func TestMainHonorsEveryOfflineSpelling(t *testing.T) {
	for _, flag := range []string{"--offline", "-offline"} {
		updateSetup(t, "1.0.0")
		calls := fakeRelease(t, "1.1.0", nil)
		commands["update-test"] = func(rc *runCtx, args []string) int {
			fs := newFlags("update-test")
			offline := fs.Bool("offline", false, "")
			if err := parseArgs(fs, args, "offline"); err != nil {
				return rc.fail(err)
			}
			rc.offline = *offline
			return 0
		}
		t.Cleanup(func() { delete(commands, "update-test") })
		var out, errb bytes.Buffer
		if Main([]string{"update-test", flag}, &out, &errb) != 0 || *calls != 0 {
			t.Errorf("%s: calls %d, %s %s", flag, *calls, out.String(), errb.String())
		}
	}
}

// Run is what tests and the C ABI call: it must never reach the network.
func TestRunNeverChecks(t *testing.T) {
	updateSetup(t, "1.0.0")
	calls := fakeRelease(t, "1.1.0", nil)
	fakeCommand(t, 0)
	var out, errb bytes.Buffer
	if Run([]string{"update-test"}, &out, &errb) != 0 || *calls != 0 {
		t.Fatalf("calls %d", *calls)
	}
}

func TestUpdateCommand(t *testing.T) {
	updateSetup(t, "1.0.0")
	fakeRelease(t, "1.1.0", nil)
	rc, out, _ := runCLIErr("update")
	if rc != 0 {
		t.Fatalf("exit %d: %s", rc, out)
	}
	var got updateResult
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Current != "1.0.0" || got.Latest != "1.1.0" || !got.UpdateAvailable || !strings.Contains(got.Command, "| iex") {
		t.Errorf("result %+v", got)
	}

	fakeRelease(t, "1.0.0", nil)
	rc, out, _ = runCLIErr("update")
	got = updateResult{}
	if err := json.Unmarshal(out, &got); err != nil || rc != 0 || got.UpdateAvailable || got.Command != "" {
		t.Errorf("up to date: exit %d %s", rc, out)
	}
}

func TestUpdateFailure(t *testing.T) {
	updateSetup(t, "1.0.0")
	fakeRelease(t, "", &line.Error{Code: line.ErrUpdateCheckFailed, Message: "no route"})
	rc, out, errb := runCLIErr("update")
	if rc == 0 || !strings.Contains(string(out)+errb, "UPDATE_CHECK_FAILED") {
		t.Errorf("exit %d: %s %s", rc, out, errb)
	}
}

func TestUpdateArgs(t *testing.T) {
	updateSetup(t, "1.0.0")
	calls := fakeRelease(t, "1.1.0", nil)
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"update", "x"}, "unexpected argument"},
		{[]string{"update", "--check"}, "-check"},
	} {
		rc, out, errb := runCLIErr(c.args...)
		if rc == 0 || !strings.Contains(string(out)+errb, c.msg) {
			t.Errorf("%v: exit %d: %s %s", c.args, rc, out, errb)
		}
	}
	if *calls != 0 {
		t.Errorf("reached the lookup %d times", *calls)
	}
}
