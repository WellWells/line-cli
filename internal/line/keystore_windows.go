package line

import (
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	crypt32           = windows.NewLazySystemDLL("crypt32.dll")
	procCryptProtect  = crypt32.NewProc("CryptProtectData")
	procCryptUnprotct = crypt32.NewProc("CryptUnprotectData")
	kernel32Free      = windows.NewLazySystemDLL("kernel32.dll").NewProc("LocalFree")
)

func newBlob(b []byte) dataBlob {
	if len(b) == 0 {
		return dataBlob{}
	}
	return dataBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

func (b dataBlob) bytes() []byte {
	if b.cbData == 0 || b.pbData == nil {
		return nil
	}
	return append([]byte(nil), unsafe.Slice(b.pbData, b.cbData)...)
}

func dpapiProtect(plain []byte) ([]byte, error) {
	in := newBlob(plain)
	var out dataBlob
	r, _, err := procCryptProtect.Call(
		uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, err
	}
	defer kernel32Free.Call(uintptr(unsafe.Pointer(out.pbData)))
	return out.bytes(), nil
}

func dpapiUnprotect(enc []byte) ([]byte, error) {
	in := newBlob(enc)
	var out dataBlob
	r, _, err := procCryptUnprotct.Call(
		uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, err
	}
	defer kernel32Free.Call(uintptr(unsafe.Pointer(out.pbData)))
	return out.bytes(), nil
}

func storeDir() string {
	la := os.Getenv("LOCALAPPDATA")
	if la == "" {
		la, _ = os.UserConfigDir()
	}
	return filepath.Join(la, "linecli")
}

// StoreDir is the folder line-cli keeps its own files in:
// %LOCALAPPDATA%\linecli.
func StoreDir() string { return storeDir() }

func storedKeyPath() string { return filepath.Join(storeDir(), "key.dat") }

// DefaultMediaDir and DefaultDownloadDir are where preview and download write
// decrypted media and files when no --out directory is given.
func DefaultMediaDir() string    { return filepath.Join(storeDir(), "media") }
func DefaultDownloadDir() string { return filepath.Join(storeDir(), "downloads") }

func loadStoredKey() ([]byte, bool) {
	enc, err := os.ReadFile(storedKeyPath())
	if err != nil || len(enc) == 0 {
		return nil, false
	}
	plain, err := dpapiUnprotect(enc)
	if err != nil || len(plain) == 0 {
		return nil, false
	}
	return plain, true
}

func saveKey(passphrase []byte) error {
	enc, err := dpapiProtect(passphrase)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(storeDir(), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(storedKeyPath(), enc)
}

// writeFileAtomic writes b to a temporary file next to path and renames it into
// place, so a concurrent reader sees either the old file or the complete new one.
func writeFileAtomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

func forgetKey() error {
	err := os.Remove(storedKeyPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
