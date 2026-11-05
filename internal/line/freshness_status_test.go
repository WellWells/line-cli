package line

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ncruces/go-sqlite3"
)

// junkDB opens a file that is not a database, so every query fails with
// DB_UNREADABLE, as when the WAL is caught mid-write.
func junkDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "junk.edb")
	if err := os.WriteFile(path, bytes.Repeat([]byte{0x5a}, 8192), 0o600); err != nil {
		t.Fatal(err)
	}
	conn, err := sqlite3.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return &DB{conn: conn}
}

func TestStatusRetriesUnreadableOnce(t *testing.T) {
	var s Status
	calls := 0
	err := s.fromDB(func() (*DB, error) {
		calls++
		if calls == 1 {
			return junkDB(t), nil
		}
		return newFakeDB(t), nil
	})
	if err != nil || calls != 2 || !s.KeyCached || s.NewestMessageAt == "" {
		t.Fatalf("err=%v calls=%d status=%+v", err, calls, s)
	}

	calls = 0
	err = (&Status{}).fromDB(func() (*DB, error) { calls++; return junkDB(t), nil })
	if le, ok := AsError(err); !ok || le.Code != ErrDBUnreadable || calls != 2 {
		t.Fatalf("persistent failure: err=%v calls=%d", err, calls)
	}

	s = Status{}
	if err := s.fromDB(func() (*DB, error) { return nil, nil }); err != nil || s.KeyCached || s.Note == "" {
		t.Fatalf("no cached key: err=%v %+v", err, s)
	}
}
