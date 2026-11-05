package line

import (
	"encoding/binary"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/vfs"
)

const decVFSName = "linecli-dec"

var (
	decRegisterOnce sync.Once
	decSources      sync.Map
	decCounter      atomic.Uint64
)

type decSource struct {
	mu       sync.Mutex
	f        *os.File
	fileSize int64
	master   key
	pageSize int
	total    int
	overlay  map[uint32][]byte
}

func (s *decSource) page(pgno uint32) []byte {
	if p, ok := s.overlay[pgno]; ok {
		return s.patch(p, pgno)
	}
	start := int64(pgno-1) * int64(s.pageSize)
	if start+int64(s.pageSize) > s.fileSize {
		return nil
	}
	buf := make([]byte, s.pageSize)
	s.mu.Lock()
	_, err := s.f.ReadAt(buf, start)
	s.mu.Unlock()
	if err != nil {
		return nil
	}
	decryptPage(buf, s.master, pgno)
	return s.patch(buf, pgno)
}

func (s *decSource) patch(buf []byte, pgno uint32) []byte {
	if pgno == 1 && s.total > 0 {
		binary.BigEndian.PutUint32(buf[28:32], uint32(s.total))
	}
	return buf
}

func newDecSource(mainEdb string, master key) (*decSource, error) {
	f, err := openShared(mainEdb)
	if err != nil {
		return nil, err
	}
	ps, err := readPageSize(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, &Error{Code: ErrDBAccess, Message: "cannot stat database"}
	}
	npages := int(fi.Size()) / ps
	ov, err := readWalOverlay(mainEdb+"-wal", ps, master)
	if err != nil {
		f.Close()
		return nil, &Error{Code: ErrDBUnreadable, Message: "cannot read write-ahead log"}
	}
	total := npages
	if ov.commitSize != 0 {
		total = int(ov.commitSize)
	}
	pages := ov.pages
	if pages == nil {
		pages = map[uint32][]byte{}
	}
	return &decSource{
		f: f, fileSize: fi.Size(), master: master,
		pageSize: ps, total: total, overlay: pages,
	}, nil
}

func (s *decSource) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f != nil {
		s.f.Close()
		s.f = nil
	}
}

type decVFS struct{}

func (decVFS) Open(name string, flags vfs.OpenFlag) (vfs.File, vfs.OpenFlag, error) {
	const databases = vfs.OPEN_MAIN_DB | vfs.OPEN_TEMP_DB | vfs.OPEN_TRANSIENT_DB
	if flags&databases == 0 {
		return nil, flags, sqlite3.CANTOPEN
	}
	token := strings.TrimPrefix(name, "/")
	v, ok := decSources.Load(token)
	if !ok {
		return nil, flags, sqlite3.CANTOPEN
	}
	return &decFile{src: v.(*decSource)}, flags | vfs.OPEN_READONLY, nil
}

func (decVFS) Delete(string, bool) error                   { return sqlite3.IOERR_DELETE_NOENT }
func (decVFS) Access(string, vfs.AccessFlag) (bool, error) { return false, nil }
func (decVFS) FullPathname(name string) (string, error)    { return name, nil }

type decFile struct {
	src  *decSource
	lock vfs.LockLevel
}

func (f *decFile) Close() error { return nil }

func (f *decFile) ReadAt(b []byte, off int64) (int, error) {
	ps := int64(f.src.pageSize)
	limit := int64(f.src.total) * ps
	if off >= limit {
		return 0, io.EOF
	}
	pgno := uint32(off/ps) + 1
	within := off % ps
	page := f.src.page(pgno)
	if page == nil {
		return 0, io.EOF
	}
	n := copy(b, page[within:])
	if n < len(b) {
		return n, io.ErrNoProgress
	}
	return n, nil
}

func (f *decFile) WriteAt([]byte, int64) (int, error) { return 0, sqlite3.READONLY }
func (f *decFile) Truncate(int64) error               { return sqlite3.READONLY }
func (f *decFile) Sync(vfs.SyncFlag) error            { return nil }

func (f *decFile) Size() (int64, error) {
	return int64(f.src.total) * int64(f.src.pageSize), nil
}

func (f *decFile) Lock(lock vfs.LockLevel) error {
	if lock >= vfs.LOCK_RESERVED {
		return sqlite3.IOERR_LOCK
	}
	f.lock = lock
	return nil
}

func (f *decFile) Unlock(lock vfs.LockLevel) error {
	f.lock = lock
	return nil
}

func (f *decFile) CheckReservedLock() (bool, error) { return false, nil }
func (f *decFile) SectorSize() int                  { return f.src.pageSize }

func (f *decFile) DeviceCharacteristics() vfs.DeviceCharacteristic {
	return vfs.IOCAP_IMMUTABLE | vfs.IOCAP_ATOMIC |
		vfs.IOCAP_SAFE_APPEND | vfs.IOCAP_POWERSAFE_OVERWRITE
}

func openVFS(mainEdb string, master key) (*DB, error) {
	decRegisterOnce.Do(func() { vfs.Register(decVFSName, decVFS{}) })
	src, err := newDecSource(mainEdb, master)
	if err != nil {
		return nil, err
	}
	token := "linecli" + strconv.FormatUint(decCounter.Add(1), 10) + ".db"
	decSources.Store(token, src)
	conn, err := sqlite3.Open("file:/" + token + "?vfs=" + decVFSName + "&mode=ro&immutable=1")
	if err != nil {
		decSources.Delete(token)
		src.close()
		return nil, &Error{Code: ErrDBUnreadable, Message: "cannot open decrypted database"}
	}
	if err := prepareConn(conn); err != nil {
		conn.Close()
		decSources.Delete(token)
		src.close()
		return nil, err
	}
	return &DB{conn: conn, cleanup: func() {
		decSources.Delete(token)
		src.close()
	}}, nil
}
