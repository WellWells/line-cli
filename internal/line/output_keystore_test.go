package line

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveKeyIsAtomicAndRoundTrips(t *testing.T) {
	la := t.TempDir()
	t.Setenv("LOCALAPPDATA", la)
	for _, pass := range [][]byte{[]byte("synthetic-passphrase-1"), []byte("synthetic-passphrase-2")} {
		if err := saveKey(pass); err != nil {
			t.Fatal(err)
		}
		got, ok := loadStoredKey()
		if !ok || !bytes.Equal(got, pass) {
			t.Fatalf("round trip: ok=%v", ok)
		}
	}
	entries, err := os.ReadDir(filepath.Join(la, "linecli"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "key.dat" {
		t.Fatalf("store dir holds %d entries, want only key.dat", len(entries))
	}
	if err := writeFileAtomic(filepath.Join(la, "missing", "x.dat"), []byte("x")); err == nil {
		t.Fatal("write into a missing directory succeeded")
	}
}

func TestDefaultOutputDirsAreUnderLocalAppData(t *testing.T) {
	la := t.TempDir()
	t.Setenv("LOCALAPPDATA", la)
	if got, want := DefaultMediaDir(), filepath.Join(la, "linecli", "media"); got != want {
		t.Errorf("DefaultMediaDir = %q, want %q", got, want)
	}
	if got, want := DefaultDownloadDir(), filepath.Join(la, "linecli", "downloads"); got != want {
		t.Errorf("DefaultDownloadDir = %q, want %q", got, want)
	}
}
