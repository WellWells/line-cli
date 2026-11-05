package line

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// launchLine starts LINE. explorer.exe starts it, so LINE is not a child of
// line-cli and keeps running after the command, or the agent or job around the
// command, ends. It starts LINE.exe itself, not LineLauncher.exe: the launcher
// first deploys any update LINE has downloaded, and the updater then ends every
// process named LINE.exe. Windows ignores case, so that includes the line.exe
// this command runs under, which would end without output or an error.
func launchLine() error {
	program := lineProgram()
	if program == "" {
		return errors.New("LINE's program was not found")
	}
	win, err := windows.GetWindowsDirectory()
	if err != nil {
		return err
	}
	cmd := exec.Command(filepath.Join(win, "explorer.exe"), program)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// lineProgram returns the LINE.exe this user has installed, or "" when there is
// none. The folder is writable by the user, so only a program whose version
// information says it is LINE counts.
func lineProgram() string {
	for _, data := range dataDirCandidates() {
		p := filepath.Join(filepath.Dir(data), "bin", "current", "LINE.exe")
		if checkLineProgram(p) == procLine {
			return p
		}
	}
	return ""
}

// findLineProcess returns the process id of LINE: a process named LINE.exe
// whose program file says it is LINE. It never opens LINE's memory.
func findLineProcess() (uint32, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, &Error{Code: ErrMemoryReadFailed, Message: "could not snapshot processes"}
	}
	defer windows.CloseHandle(snap)
	self := uint32(os.Getpid())
	var pids []uint32
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for e := windows.Process32First(snap, &pe); e == nil; e = windows.Process32Next(snap, &pe) {
		if pe.ProcessID != self && strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), "LINE.exe") {
			pids = append(pids, pe.ProcessID)
		}
	}
	if pid, ok := pickLineProcess(pids, checkLineProcess); ok {
		return pid, nil
	}
	return 0, &Error{Code: ErrLineNotRunning, Message: "LINE is not running. Start LINE, sign in, and try again."}
}

// checkLineProcess checks the program file a process runs.
func checkLineProcess(pid uint32) procVerdict {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return procUnknown
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return procUnknown
	}
	return checkLineProgram(windows.UTF16ToString(buf[:n]))
}

// checkLineProgram reads a program file's version information: procLine when
// it names the product LINE, procNotLine when it names another product or the
// file has none, procUnknown when the file cannot be read.
func checkLineProgram(path string) procVerdict {
	size, err := windows.GetFileVersionInfoSize(path, nil)
	if err != nil || size == 0 {
		f, oerr := os.Open(path)
		if oerr != nil {
			return procUnknown
		}
		f.Close()
		return procNotLine // readable, but no version information
	}
	data := make([]byte, size)
	if err := windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&data[0])); err != nil {
		return procUnknown
	}
	for _, lang := range versionLanguages(data) {
		if product, ok := versionString(data, `\StringFileInfo\`+lang+`\ProductName`); ok {
			if isLineProduct(product) {
				return procLine
			}
			return procNotLine
		}
	}
	return procNotLine
}

// versionLanguages lists the language and code page blocks of version
// information, as the "%04x%04x" names StringFileInfo uses, the declared ones
// first and then the usual US English ones.
func versionLanguages(data []byte) []string {
	var out []string
	var p unsafe.Pointer
	var n uint32
	if windows.VerQueryValue(unsafe.Pointer(&data[0]), `\VarFileInfo\Translation`, unsafe.Pointer(&p), &n) == nil && p != nil {
		for _, t := range unsafe.Slice((*[2]uint16)(p), n/4) {
			out = append(out, fmt.Sprintf("%04x%04x", t[0], t[1]))
		}
	}
	return append(out, "040904b0", "040904e4")
}

func versionString(data []byte, sub string) (string, bool) {
	var p unsafe.Pointer
	var n uint32
	if windows.VerQueryValue(unsafe.Pointer(&data[0]), sub, unsafe.Pointer(&p), &n) != nil || p == nil || n == 0 {
		return "", false
	}
	return windows.UTF16ToString(unsafe.Slice((*uint16)(p), n)), true
}
