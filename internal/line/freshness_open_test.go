package line

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ncruces/go-sqlite3"
)

func cbcEncrypt(buf []byte, k key, iv [16]byte) {
	blk, _ := aes.NewCipher(k[:])
	cipher.NewCBCEncrypter(blk, iv[:]).CryptBlocks(buf, buf)
}

// writeEncryptedFakeDB builds a synthetic SQLite file and encrypts it the way
// LINE does, so the real openers can read it back with master.
func writeEncryptedFakeDB(t *testing.T) (string, key) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake.edb")
	conn, err := sqlite3.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Exec("PRAGMA page_size=4096; CREATE TABLE fake(x INTEGER); INSERT INTO fake VALUES (7)"); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	plain, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var master key
	rand.Read(master[:])
	const ps = 4096
	enc := make([]byte, len(plain))
	for p := 1; p*ps <= len(plain); p++ {
		pg := append([]byte(nil), plain[(p-1)*ps:p*ps]...)
		out := enc[(p-1)*ps : p*ps]
		if p > 1 {
			cbcEncrypt(pg, pageKey(master, uint32(p)), pageIV(uint32(p)))
			copy(out, pg)
			continue
		}
		// Page 1 keeps header bytes 16-24 in the clear and moves their
		// ciphertext to bytes 8-16.
		ct := pg[16:]
		cbcEncrypt(ct, pageKey(master, 1), pageIV(1))
		copy(out[8:16], ct[:8])
		copy(out[16:24], plain[16:24])
		copy(out[24:], ct[8:])
	}
	if err := os.WriteFile(path, enc, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, master
}

// Every opener must record where it read from and the on-disk state, or a
// checkpoint during a command would go unnoticed.
func TestOpenWithRecordsStamp(t *testing.T) {
	for _, mode := range []string{"", "image"} {
		t.Run("open="+mode, func(t *testing.T) {
			t.Setenv("LINECLI_OPEN", mode)
			path, master := writeEncryptedFakeDB(t)
			db, err := openWith(path, master)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if db.path != path || db.master != master || db.stamp != readStamp(path) || db.stamp.size == 0 {
				t.Fatalf("opener left path=%q stamp=%+v", db.path, db.stamp)
			}
			st, _, err := db.conn.Prepare("SELECT x FROM fake")
			if err != nil || !st.Step() || st.ColumnInt64(0) != 7 {
				t.Fatalf("decrypted read failed: %v", err)
			}
			st.Close()
			if db.Changed() {
				t.Fatal("untouched database reported as changed")
			}
			later := time.Now().Add(time.Minute)
			if err := os.Chtimes(path, later, later); err != nil {
				t.Fatal(err)
			}
			if !db.Changed() {
				t.Fatal("main file rewritten after open but Changed() is false")
			}
		})
	}
}
