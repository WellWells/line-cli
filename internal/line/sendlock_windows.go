package line

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

// lockSendLog takes an exclusive lock on send.lock, waiting up to 10 s.
// Windows releases it when the handle closes or the process dies, so an
// interrupted send never leaves it stuck.
func lockSendLog() (func(), error) {
	if err := os.MkdirAll(storeDir(), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(storeDir(), "send.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	h := windows.Handle(f.Fd())
	deadline := time.Now().Add(10 * time.Second)
	for {
		ol := new(windows.Overlapped)
		err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
		if err == nil {
			return func() {
				windows.UnlockFileEx(h, 0, 1, 0, ol)
				f.Close()
			}, nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) || time.Now().After(deadline) {
			f.Close()
			return nil, err
		}
		time.Sleep(20 * time.Millisecond)
	}
}
