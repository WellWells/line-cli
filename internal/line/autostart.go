package line

import (
	"os"
	"time"
)

// noAutostartEnv, set to anything, keeps line-cli from starting LINE by itself.
const noAutostartEnv = "LINECLI_NO_AUTOSTART"

// Test seams: how LINE is started and how long a command waits for it.
var (
	startLine      = launchLine
	lineStartWait  = 60 * time.Second // after starting LINE, which then loads and may ask to sign in
	lineLoadWait   = 15 * time.Second // when LINE already runs but may only just have started
	lineRetryPause = 2 * time.Second
)

// lineNotReady reports an error that only means LINE is not up yet: it does not
// run, or it runs but has not loaded what a scan of its memory looks for.
func lineNotReady(err error) bool {
	le, ok := AsError(err)
	if !ok {
		return false
	}
	switch le.Code {
	case ErrLineNotRunning, ErrKeyScanEmpty, ErrNoKeyDecrypted, ErrTokenNotFound:
		return true
	}
	return false
}

// whenLineReady runs scan, which reads LINE's memory, for a command that cannot
// work without what it finds, so that nobody has to prepare LINE first. When LINE
// does not run it starts LINE, if mayStart, and while LINE is not ready it tries
// again for a limited time. Any other error ends it at once. When it gives up, it
// returns the last error.
func whenLineReady[T any](scan func() (T, error), mayStart bool, notify func(string)) (T, error) {
	v, err := scan()
	if !lineNotReady(err) {
		return v, err
	}
	mayStart = mayStart && os.Getenv(noAutostartEnv) == ""
	started := false
	deadline := time.Now().Add(lineLoadWait)
	for {
		if le, _ := AsError(err); le.Code == ErrLineNotRunning && !started {
			if !mayStart || startLine() != nil {
				return v, err
			}
			started = true
			deadline = time.Now().Add(lineStartWait)
			if notify != nil {
				notify("LINE is not running; starting it (this can take up to a minute)…")
			}
		}
		if !time.Now().Before(deadline) {
			if le, _ := AsError(err); started {
				return v, &Error{Code: le.Code, Message: "LINE was started but is not ready. Sign in to LINE if it asks, then try again."}
			}
			return v, err
		}
		time.Sleep(lineRetryPause)
		if v, err = scan(); !lineNotReady(err) {
			return v, err
		}
	}
}
