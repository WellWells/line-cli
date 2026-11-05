package line

import (
	"encoding/binary"
	"os"
	"strconv"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/vfs/memdb"
)

type DB struct {
	conn    *sqlite3.Conn
	cleanup func()
	path    string
	master  key
	stamp   dbStamp
}

func (db *DB) Close() error {
	err := db.conn.Close()
	if db.cleanup != nil {
		db.cleanup()
	}
	return err
}

func readPageSize(f interface {
	ReadAt([]byte, int64) (int, error)
}) (int, error) {
	hdr := make([]byte, 24)
	if _, err := f.ReadAt(hdr, 0); err != nil {
		return 0, &Error{Code: ErrDBAccess, Message: "cannot read database header"}
	}
	ps := int(binary.BigEndian.Uint16(hdr[16:18]))
	if ps == 1 {
		ps = 65536
	}
	if ps < 512 || ps > 65536 || (ps-1)&ps != 0 {
		ps = 4096
	}
	return ps, nil
}

// Open opens LINE's database. Without a usable cached key it gets the key from
// LINE's memory, starting LINE first if it does not run and mayStartLine is set.
func Open(notify func(string), mayStartLine bool) (*DB, error) {
	loc, err := Locate()
	if err != nil {
		return nil, err
	}
	master, err := resolveKey(loc.MainEdb, false, mayStartLine, notify)
	if err != nil {
		return nil, err
	}
	return openWith(loc.MainEdb, master)
}

// openWith is the one place a DB is opened: it records the on-disk state
// before reading so Changed can detect a checkpoint during the command.
func openWith(mainEdb string, master key) (*DB, error) {
	stamp := readStamp(mainEdb)
	open := openVFS
	if os.Getenv("LINECLI_OPEN") == "image" {
		open = openImage
	}
	db, err := open(mainEdb, master)
	if err != nil {
		return nil, err
	}
	db.path, db.master, db.stamp = mainEdb, master, stamp
	return db, nil
}

// Reopen opens a fresh connection to the same database. A connection sees the
// WAL as it was when opened, so rows LINE writes later need a new one.
func (db *DB) Reopen() (*DB, error) {
	if db.path == "" {
		return nil, &Error{Code: ErrDBUnreadable, Message: "cannot reopen database"}
	}
	return openWith(db.path, db.master)
}

func openImage(mainEdb string, master key) (*DB, error) {
	f, err := openShared(mainEdb)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ps, err := readPageSize(f)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		return nil, &Error{Code: ErrDBAccess, Message: "cannot stat database"}
	}
	npages := int(fi.Size()) / ps
	ov, err := readWalOverlay(mainEdb+"-wal", ps, master)
	if err != nil {
		return nil, &Error{Code: ErrDBUnreadable, Message: "cannot read write-ahead log"}
	}
	img := buildImage(f, ps, npages, master, ov)
	name := "linecli-img" + strconv.FormatUint(decCounter.Add(1), 10) + ".db"
	memdb.Create(name, img)
	conn, err := sqlite3.Open("file:/" + name + "?vfs=memdb&mode=ro&immutable=1")
	if err != nil {
		memdb.Delete(name)
		return nil, &Error{Code: ErrDBUnreadable, Message: "cannot open decrypted database"}
	}
	if err := prepareConn(conn); err != nil {
		conn.Close()
		memdb.Delete(name)
		return nil, err
	}
	return &DB{conn: conn, cleanup: func() { memdb.Delete(name) }}, nil
}

func prepareConn(conn *sqlite3.Conn) error {
	if err := conn.Exec("PRAGMA temp_store=MEMORY"); err != nil {
		return &Error{Code: ErrDBUnreadable, Message: "cannot configure database connection"}
	}
	return nil
}

// OpenCached opens the database with the cached key only: it never scans
// LINE's memory. It returns a nil DB when no cached key decrypts the database.
func OpenCached() (*DB, error) {
	loc, err := Locate()
	if err != nil {
		return nil, err
	}
	m, ok, err := cachedKey(loc.MainEdb)
	if err != nil || !ok {
		return nil, err
	}
	return openWith(loc.MainEdb, m)
}

func cachedKey(mainEdb string) (key, bool, error) {
	page1, _, err := readFirstPage(mainEdb)
	if err != nil {
		return key{}, false, err
	}
	stored, ok := loadStoredKey()
	if !ok {
		return key{}, false, nil
	}
	m, ok := verifyPassphrase(page1, stored)
	return m, ok, nil
}
