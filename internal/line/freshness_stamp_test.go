package line

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFakeDB lays out a main file, a WAL header and a -shm WAL-index header
// with synthetic bytes; readStamp only looks at sizes, times and headers.
func writeFakeDB(t *testing.T) string {
	t.Helper()
	main := filepath.Join(t.TempDir(), "fake.edb")
	if err := os.WriteFile(main, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	wal := make([]byte, 32)
	binary.BigEndian.PutUint32(wal[0:4], 0x377f0682)
	binary.BigEndian.PutUint32(wal[12:16], 7)
	binary.BigEndian.PutUint32(wal[16:20], 0x1111)
	binary.BigEndian.PutUint32(wal[20:24], 0x2222)
	if err := os.WriteFile(main+"-wal", wal, 0o600); err != nil {
		t.Fatal(err)
	}
	shm := make([]byte, 32768)
	binary.LittleEndian.PutUint32(shm[96:100], 10)
	binary.LittleEndian.PutUint32(shm[128:132], 10)
	if err := os.WriteFile(main+"-shm", shm, 0o600); err != nil {
		t.Fatal(err)
	}
	return main
}

func patchFile(t *testing.T, path string, off int64, b []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteAt(b, off); err != nil {
		t.Fatal(err)
	}
}

func u32(order binary.ByteOrder, v uint32) []byte {
	b := make([]byte, 4)
	order.PutUint32(b, v)
	return b
}

func TestStampDetectsCheckpoint(t *testing.T) {
	for _, c := range []struct {
		name    string
		changed bool
		touch   func(t *testing.T, main string)
	}{
		{"nothing", false, func(*testing.T, string) {}},
		{"new WAL frames only", false, func(t *testing.T, main string) {
			f, err := os.OpenFile(main+"-wal", os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			f.Write(make([]byte, 4120))
			f.Close()
		}},
		{"read mark only", false, func(t *testing.T, main string) { patchFile(t, main+"-shm", 104, u32(binary.LittleEndian, 99)) }},
		{"WAL restarted with new salts", true, func(t *testing.T, main string) { patchFile(t, main+"-wal", 16, u32(binary.BigEndian, 0x3333)) }},
		{"checkpoint sequence advanced", true, func(t *testing.T, main string) { patchFile(t, main+"-wal", 12, u32(binary.BigEndian, 8)) }},
		{"backfill attempted", true, func(t *testing.T, main string) { patchFile(t, main+"-shm", 128, u32(binary.LittleEndian, 20)) }},
		{"backfill done", true, func(t *testing.T, main string) { patchFile(t, main+"-shm", 96, u32(binary.LittleEndian, 20)) }},
		{"main file grew", true, func(t *testing.T, main string) {
			f, err := os.OpenFile(main, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			f.Write(make([]byte, 4096))
			f.Close()
		}},
		{"main page rewritten in place", true, func(t *testing.T, main string) {
			patchFile(t, main, 4000, []byte{1})
			fi, _ := os.Stat(main)
			os.Chtimes(main, fi.ModTime(), fi.ModTime().Add(time.Second))
		}},
		{"WAL deleted", true, func(t *testing.T, main string) { os.Remove(main + "-wal") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			main := writeFakeDB(t)
			db := &DB{path: main, stamp: readStamp(main)}
			if db.stamp.size != 4096 || db.stamp.backfill != [2]uint32{10, 10} {
				t.Fatalf("stamp not read: %+v", db.stamp)
			}
			c.touch(t, main)
			if got := db.Changed(); got != c.changed {
				t.Fatalf("Changed() = %v, want %v", got, c.changed)
			}
		})
	}
}

func TestStampWithoutWALOrShm(t *testing.T) {
	main := filepath.Join(t.TempDir(), "fake.edb")
	if err := os.WriteFile(main, make([]byte, 1024), 0o600); err != nil {
		t.Fatal(err)
	}
	db := &DB{path: main, stamp: readStamp(main)}
	if db.Changed() {
		t.Fatal("unchanged files reported as changed")
	}
	if (&DB{}).Changed() {
		t.Fatal("a DB without a path must never ask for a rerun")
	}
}

func TestDataAsOfIsNewestWrite(t *testing.T) {
	main := writeFakeDB(t)
	old, recent := time.Now().Add(-2*time.Hour), time.Now().Add(-time.Minute)
	os.Chtimes(main, old, old)
	os.Chtimes(main+"-wal", recent, recent)
	if got := statDBFiles(main).asOf(); !got.Equal(recent.Truncate(100 * time.Nanosecond)) {
		t.Fatalf("asOf = %v, want the WAL time %v", got, recent)
	}
	os.Remove(main + "-wal")
	d := statDBFiles(main)
	if !d.asOf().Equal(old.Truncate(100*time.Nanosecond)) || !d.walMod.IsZero() || d.size != 4096 {
		t.Fatalf("without WAL: %+v", d)
	}
	if fileTimeISO(time.Time{}) != "" {
		t.Fatal("zero time must format as empty")
	}
}
