package line

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func init() {
	// No test starts the real LINE or waits for it unless it says so itself.
	startLine = func() error { return errors.New("tests do not start LINE") }
	lineLoadWait, lineStartWait, lineRetryPause = 0, 0, 0
}

// fakeLine replaces how LINE starts and how long a command waits for it.
func fakeLine(t *testing.T, start func() error, load, begin time.Duration) {
	t.Helper()
	s, l, b, p := startLine, lineLoadWait, lineStartWait, lineRetryPause
	startLine, lineLoadWait, lineStartWait, lineRetryPause = start, load, begin, time.Millisecond
	t.Cleanup(func() { startLine, lineLoadWait, lineStartWait, lineRetryPause = s, l, b, p })
}

// scanResults returns a scan that gives these errors in turn and then "found".
func scanResults(errs ...error) func() (string, error) {
	i := 0
	return func() (string, error) {
		if i < len(errs) {
			i++
			return "", errs[i-1]
		}
		return "found", nil
	}
}

var (
	notRunning = &Error{Code: ErrLineNotRunning, Message: "LINE is not running."}
	scanEmpty  = &Error{Code: ErrKeyScanEmpty, Message: "no candidates"}
)

func TestWhenLineReadyDoesNothingElseWhenTheScanWorks(t *testing.T) {
	fakeLine(t, func() error { t.Error("started LINE although the scan worked"); return nil }, 0, 0)
	got, err := whenLineReady(scanResults(), true, nil)
	if err != nil || got != "found" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestWhenLineReadyStartsLineOnceAndWaitsForIt(t *testing.T) {
	starts := 0
	fakeLine(t, func() error { starts++; return nil }, 0, time.Minute)
	var notes []string
	got, err := whenLineReady(scanResults(notRunning, notRunning, scanEmpty), true, func(m string) { notes = append(notes, m) })
	if err != nil || got != "found" {
		t.Fatalf("got %q, %v", got, err)
	}
	if starts != 1 {
		t.Errorf("started LINE %d times", starts)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "starting") {
		t.Errorf("notes = %q", notes)
	}
}

func TestWhenLineReadyWaitsForARunningLineWithoutStartingIt(t *testing.T) {
	fakeLine(t, func() error { t.Error("started LINE although it runs"); return nil }, time.Minute, 0)
	for _, code := range []ErrorCode{ErrKeyScanEmpty, ErrNoKeyDecrypted, ErrTokenNotFound} {
		got, err := whenLineReady(scanResults(&Error{Code: code}, &Error{Code: code}), false, nil)
		if err != nil || got != "found" {
			t.Errorf("%s: got %q, %v", code, got, err)
		}
	}
}

func TestWhenLineReadyDoesNotStartLineWhenItMayNot(t *testing.T) {
	fakeLine(t, func() error { t.Error("started LINE although it may not"); return nil }, time.Minute, time.Minute)
	if _, err := whenLineReady(scanResults(notRunning), false, nil); err != notRunning {
		t.Fatalf("err = %v", err)
	}
	t.Setenv(noAutostartEnv, "1")
	if _, err := whenLineReady(scanResults(notRunning), true, nil); err != notRunning {
		t.Fatalf("%s: err = %v", noAutostartEnv, err)
	}
}

func TestWhenLineReadyKeepsTheErrorWhenLineCannotStart(t *testing.T) {
	fakeLine(t, func() error { return errors.New("no launcher") }, 0, time.Minute)
	if _, err := whenLineReady(scanResults(notRunning), true, nil); err != notRunning {
		t.Fatalf("err = %v", err)
	}
}

func TestWhenLineReadyStopsAtOtherErrors(t *testing.T) {
	fakeLine(t, func() error { t.Error("started LINE after another error"); return nil }, time.Minute, time.Minute)
	denied := &Error{Code: ErrMemoryReadFailed, Message: "denied"}
	if _, err := whenLineReady(scanResults(denied), true, nil); err != denied {
		t.Fatalf("err = %v", err)
	}
	plain := errors.New("plain")
	if _, err := whenLineReady(scanResults(plain), true, nil); err != plain {
		t.Fatalf("err = %v", err)
	}
}

func TestWhenLineReadyGivesUpSayingWhatToDo(t *testing.T) {
	fakeLine(t, func() error { return nil }, 0, 5*time.Millisecond)
	_, err := whenLineReady(func() (string, error) { return "", notRunning }, true, nil)
	le, ok := AsError(err)
	if !ok || le.Code != ErrLineNotRunning || !strings.Contains(le.Message, "Sign in") {
		t.Fatalf("err = %v", err)
	}
}

func TestLineProgramAcceptsOnlyLINEsOwnProgram(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	la := filepath.Join(home, "AppData", "Local")
	t.Setenv("LOCALAPPDATA", la)
	current := filepath.Join(la, "LINE", "bin", "current")
	if err := os.MkdirAll(current, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := lineProgram(); got != "" {
		t.Errorf("no program: got %q", got)
	}
	if err := os.WriteFile(filepath.Join(current, "LINE.exe"), []byte("not LINE"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := lineProgram(); got != "" {
		t.Errorf("a file without LINE's version information: got %q", got)
	}
}

func TestLineProgramFindsTheInstalledLINE(t *testing.T) {
	want := filepath.Join(os.Getenv("LOCALAPPDATA"), "LINE", "bin", "current", "LINE.exe")
	if _, err := os.Stat(want); err != nil {
		t.Skip("LINE is not installed here")
	}
	if got := lineProgram(); !strings.EqualFold(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
