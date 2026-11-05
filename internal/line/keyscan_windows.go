package line

import (
	"os"
	"sort"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	maxRegionBytes = 128 * 1024 * 1024
	memCommit      = 0x1000
	memPrivate     = 0x20000
	pageNoAccess   = 0x1
	pageGuard      = 0x100
)

type memInfo struct {
	BaseAddress       uintptr
	AllocationBase    uintptr
	AllocationProtect uint32
	_                 uint32
	RegionSize        uintptr
	State             uint32
	Protect           uint32
	Type              uint32
	_                 uint32
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isAlnum(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func lowerHex(b *[32]byte) [32]byte {
	var o [32]byte
	for i, c := range b {
		if c >= 'A' && c <= 'F' {
			c += 32
		}
		o[i] = c
	}
	return o
}

func scanStream(n int, charAt func(i int) byte, counts map[[32]byte]int) {
	runStart := -1
	flush := func(end int) {
		if runStart >= 0 && end-runStart == 32 {
			before := byte(0xFF)
			if runStart > 0 {
				before = charAt(runStart - 1)
			}
			after := byte(0xFF)
			if end < n {
				after = charAt(end)
			}
			if !isAlnum(before) && !isAlnum(after) {
				var tok [32]byte
				for j := 0; j < 32; j++ {
					tok[j] = charAt(runStart + j)
				}
				lt := lowerHex(&tok)
				counts[lt]++
			}
		}
		runStart = -1
	}
	for i := 0; i < n; i++ {
		if isHex(charAt(i)) {
			if runStart < 0 {
				runStart = i
			}
		} else {
			flush(i)
		}
	}
	flush(n)
}

func scanRegionASCII(buf []byte, counts map[[32]byte]int) {
	scanStream(len(buf), func(i int) byte { return buf[i] }, counts)
}

func scanRegionUTF16(buf []byte, counts map[[32]byte]int) {
	n := len(buf) / 2
	scanStream(n, func(i int) byte {
		if buf[2*i+1] != 0 {
			return 0xFF
		}
		return buf[2*i]
	}, counts)
}

func scanKeyCandidates() ([]string, error) {
	pid, err := findLineProcess()
	if err != nil {
		return nil, err
	}
	h, err := windows.OpenProcess(windows.PROCESS_VM_READ|windows.PROCESS_QUERY_INFORMATION, false, pid)
	if err != nil {
		return nil, &Error{Code: ErrMemoryReadFailed, Message: "Could not open LINE process memory. Try running as Administrator."}
	}
	defer windows.CloseHandle(h)

	vqe := windows.NewLazySystemDLL("kernel32.dll").NewProc("VirtualQueryEx")
	counts := map[[32]byte]int{}
	var addr uintptr
	var buf []byte
	for addr < 0x7FFFFFFF0000 {
		var m memInfo
		r1, _, _ := vqe.Call(uintptr(h), addr, uintptr(unsafe.Pointer(&m)), unsafe.Sizeof(m))
		if r1 == 0 {
			break
		}
		next := m.BaseAddress + m.RegionSize
		include := m.State == memCommit && m.Type == memPrivate &&
			(m.Protect&pageNoAccess) == 0 && (m.Protect&pageGuard) == 0 &&
			m.RegionSize > 0 && m.RegionSize <= maxRegionBytes
		if include {
			rs := int(m.RegionSize)
			if cap(buf) < rs {
				buf = make([]byte, rs)
			}
			b := buf[:rs]
			var nRead uintptr
			if e := windows.ReadProcessMemory(h, m.BaseAddress, &b[0], uintptr(rs), &nRead); e == nil && nRead > 0 {
				bb := b[:nRead]
				scanRegionASCII(bb, counts)
				scanRegionUTF16(bb, counts)
			}
		}
		if next <= addr {
			break
		}
		addr = next
	}
	if len(counts) == 0 {
		return nil, &Error{Code: ErrKeyScanEmpty, Message: "No key candidates were found in LINE process memory."}
	}
	type tc struct {
		tok [32]byte
		n   int
	}
	ranked := make([]tc, 0, len(counts))
	for k, v := range counts {
		ranked = append(ranked, tc{k, v})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].n > ranked[j].n })
	out := make([]string, len(ranked))
	for i, c := range ranked {
		out[i] = string(c.tok[:])
	}
	return out, nil
}

func openShared(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &Error{Code: ErrDBAccess, Message: "cannot open LINE database file"}
	}
	return os.NewFile(uintptr(h), path), nil
}
