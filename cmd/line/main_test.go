package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// The test binary also plays linecli.exe: copied to bin\linecli.exe and run by
// line.exe with this variable set, it reports what it was given.
const fakeEnv = "LINE_TEST_FAKE_LINECLI"

func TestMain(m *testing.M) {
	if os.Getenv(fakeEnv) == "1" {
		os.Exit(fakeLinecli(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeLinecli prints its arguments as JSON; "stdin" then copies its input,
// "exit N" exits with N, and "sleep FILE" writes its process id to FILE and
// waits.
func fakeLinecli(args []string) int {
	b, _ := json.Marshal(args)
	os.Stdout.Write(append(b, '\n'))
	switch {
	case len(args) > 0 && args[0] == "stdin":
		io.Copy(os.Stdout, os.Stdin)
	case len(args) > 1 && args[0] == "exit":
		n, _ := strconv.Atoi(args[1])
		return n
	case len(args) > 1 && args[0] == "sleep":
		os.WriteFile(args[1], []byte(strconv.Itoa(os.Getpid())), 0o644)
		time.Sleep(time.Minute)
	}
	return 0
}

func TestAfterProgramName(t *testing.T) {
	for _, c := range []struct{ raw, want string }{
		{`"C:\Program Files\x\line.exe" read abc`, ` read abc`},
		{`line read "a b"`, ` read "a b"`},
		{`line`, ``},
		{`"C:\x\line.exe"`, ``},
		{`line	--json`, "\t--json"},
		{`"C:\x\line.exe"read`, ` read`},
		{`"unterminated`, ``},
		{"line send --message \"one\ntwo\" -y", " send --message \"one\ntwo\" -y"},
		{`line search "\"exact phrase\""`, ` search "\"exact phrase\""`},
	} {
		if got := afterProgramName(c.raw); got != c.want {
			t.Errorf("afterProgramName(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
	if got := commandLine(`C:\a b\bin\linecli.exe`, `line chats x`); got != `"C:\a b\bin\linecli.exe" chats x` {
		t.Errorf("commandLine = %q", got)
	}
}

func TestFindProgram(t *testing.T) {
	root := `C:\Users\u\AppData\Local\linecli`
	installed := filepath.Join(root, "bin", "linecli.exe")
	sameDir := filepath.Join(root, "cmd", "linecli.exe")
	for _, c := range []struct {
		name  string
		files []string
		want  string
	}{
		{"installed layout", []string{installed}, installed},
		{"bin wins over same folder", []string{installed, sameDir}, installed},
		{"same folder", []string{sameDir}, sameDir},
		{"missing", nil, ""},
	} {
		exists := func(p string) bool {
			for _, f := range c.files {
				if strings.EqualFold(f, p) {
					return true
				}
			}
			return false
		}
		if got := findProgram(filepath.Join(root, "cmd"), exists); got != c.want {
			t.Errorf("%s: findProgram = %q, want %q", c.name, got, c.want)
		}
	}
}

// installedLayout builds line.exe into root\cmd and, unless noProgram, copies
// the test binary to root\bin\linecli.exe, and returns line.exe's path.
func installedLayout(t *testing.T, noProgram bool) string {
	t.Helper()
	root := t.TempDir()
	line := filepath.Join(root, "cmd", "line.exe")
	build := exec.Command("go", "build", "-o", line, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	if noProgram {
		return line
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", program), data, 0o755); err != nil {
		t.Fatal(err)
	}
	return line
}

func runLine(line string, stdin string, args ...string) (stdout string, code int, err error) {
	cmd := exec.Command(line, args...)
	cmd.Env = append(os.Environ(), fakeEnv+"=1")
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		return out.String(), ee.ExitCode(), nil
	}
	return out.String(), 0, err
}

func TestLauncher(t *testing.T) {
	if testing.Short() {
		t.Skip("builds line.exe")
	}
	line := installedLayout(t, false)

	t.Run("arguments arrive unchanged", func(t *testing.T) {
		args := []string{"send", "a b", `"quoted"`, `back\slash\`, `tail\\"`, "multi\nline\r\nCRLF", "中文 ü 😀", "", "%PATH%", "^&|<>!", `C:\dir\`, "--message=x y", "\ttab"}
		out, code, err := runLine(line, "", args...)
		if err != nil || code != 0 {
			t.Fatalf("code %d, err %v, out %s", code, err, out)
		}
		var got []string
		if err := json.Unmarshal([]byte(strings.SplitN(out, "\n", 2)[0]), &got); err != nil {
			t.Fatalf("%v: %q", err, out)
		}
		if !reflect.DeepEqual(got, args) {
			t.Errorf("linecli.exe got %q\nwant %q", got, args)
		}
	})

	t.Run("no arguments", func(t *testing.T) {
		out, code, err := runLine(line, "")
		if err != nil || code != 0 || strings.TrimSpace(out) != "[]" {
			t.Errorf("code %d, err %v, out %q", code, err, out)
		}
	})

	t.Run("stdin passes through", func(t *testing.T) {
		in := "第一行\nsecond line\n"
		out, code, err := runLine(line, in, "stdin")
		if err != nil || code != 0 || !strings.HasSuffix(out, in) {
			t.Errorf("code %d, err %v, out %q", code, err, out)
		}
	})

	t.Run("exit code passes through", func(t *testing.T) {
		for _, want := range []int{1, 2, 7} {
			_, code, err := runLine(line, "", "exit", strconv.Itoa(want))
			if err != nil || code != want {
				t.Errorf("exit %d: got code %d, err %v", want, code, err)
			}
		}
	})

	t.Run("killing line ends linecli", func(t *testing.T) {
		pidFile := filepath.Join(t.TempDir(), "pid")
		cmd := exec.Command(line, "sleep", pidFile)
		cmd.Env = append(os.Environ(), fakeEnv+"=1")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		var pid int
		for deadline := time.Now().Add(15 * time.Second); pid == 0; {
			if b, err := os.ReadFile(pidFile); err == nil {
				pid, _ = strconv.Atoi(string(b))
			}
			if pid == 0 {
				if time.Now().After(deadline) {
					cmd.Process.Kill()
					t.Fatal("linecli.exe never started")
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
		h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
		if err != nil {
			t.Fatalf("open linecli.exe: %v", err)
		}
		defer windows.CloseHandle(h)
		if err := cmd.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		cmd.Wait()
		if ev, _ := windows.WaitForSingleObject(h, 10000); ev != windows.WAIT_OBJECT_0 {
			windows.TerminateProcess(h, 1)
			t.Fatal("linecli.exe kept running after line.exe was killed")
		}
	})
}

func TestLauncherWithoutProgram(t *testing.T) {
	if testing.Short() {
		t.Skip("builds line.exe")
	}
	line := installedLayout(t, true)
	out, code, err := runLine(line, "", "status")
	if err != nil || code != 1 {
		t.Fatalf("code %d, err %v, out %s", code, err, out)
	}
	var e struct{ Error, Message string }
	if err := json.Unmarshal([]byte(out), &e); err != nil || e.Error != "ERROR" || !strings.Contains(e.Message, "linecli.exe") {
		t.Errorf("want a JSON error naming linecli.exe, got %q", out)
	}
}
