package line

import (
	"encoding/binary"
	"io"
)

// dbStamp identifies the on-disk state a connection's WAL snapshot was taken
// from. Pages missing from the overlay are read from the main file lazily, so
// a LINE checkpoint during a command can mix newer pages into the snapshot.
// A checkpoint writes the main file (size, mtime), advances the -shm backfill
// counters and, on the next write, restarts the WAL with new salts.
type dbStamp struct {
	size     int64
	modTime  int64
	walHdr   [12]byte  // checkpoint sequence and salts
	backfill [2]uint32 // -shm nBackfill, nBackfillAttempted
}

func readStamp(mainEdb string) dbStamp {
	var s dbStamp
	if f, err := openShared(mainEdb); err == nil {
		if fi, err := f.Stat(); err == nil {
			s.size, s.modTime = fi.Size(), fi.ModTime().UnixNano()
		}
		f.Close()
	}
	if b := readHead(mainEdb+"-wal", 24); len(b) == 24 {
		copy(s.walHdr[:], b[12:24])
	}
	if b := readHead(mainEdb+"-shm", 132); len(b) == 132 {
		s.backfill[0] = binary.LittleEndian.Uint32(b[96:100])
		s.backfill[1] = binary.LittleEndian.Uint32(b[128:132])
	}
	return s
}

func readHead(path string, n int) []byte {
	f, err := openShared(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	b := make([]byte, n)
	m, _ := io.ReadFull(f, b)
	return b[:m]
}

// Changed reports whether LINE checkpointed the database since it was opened,
// in which case results read through it may mix two versions and the command
// should be rerun on a fresh connection.
func (db *DB) Changed() bool {
	return db.path != "" && readStamp(db.path) != db.stamp
}
