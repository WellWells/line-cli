package line

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckLineProgram(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// A Go program has no version information, like line-cli's line.exe.
	if got := checkLineProgram(self); got != procNotLine {
		t.Errorf("test binary: got %v, want procNotLine", got)
	}
	if got := checkLineProgram(filepath.Join(os.Getenv("SystemRoot"), "System32", "notepad.exe")); got != procNotLine {
		t.Errorf("notepad.exe: got %v, want procNotLine", got)
	}
	if got := checkLineProgram(filepath.Join(t.TempDir(), "missing.exe")); got != procUnknown {
		t.Errorf("missing file: got %v, want procUnknown", got)
	}
	line := filepath.Join(os.Getenv("LOCALAPPDATA"), "LINE", "bin", "current", "LINE.exe")
	if _, err := os.Stat(line); err != nil {
		t.Logf("LINE is not installed at %s; skipping the LINE.exe check", line)
		return
	}
	if got := checkLineProgram(line); got != procLine {
		t.Errorf("%s: got %v, want procLine", line, got)
	}
}

const fakeLineEnv = "LINE_TEST_FAKE_LINE_EXE"

// TestFakeLineExe is the process TestFindLineProcessSkipsOtherLineExe runs as
// LINE.exe; it only waits.
func TestFakeLineExe(t *testing.T) {
	if os.Getenv(fakeLineEnv) != "1" {
		t.Skip("helper process for TestFindLineProcessSkipsOtherLineExe")
	}
	time.Sleep(30 * time.Second)
}

// A program merely named LINE.exe, as line-cli's line.exe is to Windows, must
// not be taken for LINE.
func TestFindLineProcessSkipsOtherLineExe(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a process")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(t.TempDir(), "LINE.exe")
	if err := os.WriteFile(fake, data, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(fake, "-test.run=^TestFakeLineExe$")
	cmd.Env = append(os.Environ(), fakeLineEnv+"=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()

	pid, err := findLineProcess()
	if err == nil && pid == uint32(cmd.Process.Pid) {
		t.Fatalf("findLineProcess picked the fake LINE.exe (pid %d)", pid)
	}
	if err != nil {
		if le, ok := AsError(err); !ok || le.Code != ErrLineNotRunning {
			t.Fatalf("findLineProcess: %v", err)
		}
	}
}
