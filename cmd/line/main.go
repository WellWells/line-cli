// Command line is the command people and agents type. It runs linecli.exe, the
// real program, with the same command line, input and output, and exits with
// its exit code. The installer puts only line.exe's folder (cmd) on PATH and
// keeps linecli.exe in the bin folder beside it, so 'linec' + Tab completes
// nothing, while linecli.exe keeps the one path the firewall allows.
//
// line.exe has no version information on purpose: finding LINE's process
// relies on LINE.exe naming its product LINE, and line.exe must not.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// program is the real program line.exe runs.
const program = "linecli.exe"

func main() { os.Exit(run()) }

func run() int {
	self, err := os.Executable()
	if err != nil {
		return fail("line.exe could not find its own folder: " + err.Error())
	}
	dir := filepath.Dir(self)
	target := findProgram(dir, isFile)
	if target == "" {
		return fail(fmt.Sprintf("line.exe could not find %s in %s or %s; reinstall line-cli", program, filepath.Join(filepath.Dir(dir), "bin"), dir))
	}
	joinKillOnCloseJob()

	cmd := exec.Command(target)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: commandLine(target, windows.UTF16PtrToString(windows.GetCommandLine()))}
	// Handing over the handles themselves, not pipes, keeps what linecli.exe
	// sees: a terminal stays a terminal and --stdin reads the same input.
	if os.Stdin != nil {
		cmd.Stdin = os.Stdin
	}
	if os.Stdout != nil {
		cmd.Stdout = os.Stdout
	}
	if os.Stderr != nil {
		cmd.Stderr = os.Stderr
	}
	// Ctrl+C reaches linecli.exe too, as it shares the console; line.exe keeps
	// running so it can pass on how linecli.exe ended.
	signal.Notify(make(chan os.Signal, 1), os.Interrupt)
	if err := cmd.Start(); err != nil {
		return fail("line.exe could not start " + target + ": " + err.Error())
	}
	err = cmd.Wait()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	if err != nil {
		return fail("line.exe lost track of " + target + ": " + err.Error())
	}
	return 0
}

// findProgram returns linecli.exe in the bin folder beside line.exe's folder
// (the installed layout), else in line.exe's own folder (a build or go install
// that puts both in one folder), else "".
func findProgram(dir string, exists func(string) bool) string {
	for _, p := range []string{
		filepath.Join(filepath.Dir(dir), "bin", program),
		filepath.Join(dir, program),
	} {
		if exists(p) {
			return p
		}
	}
	return ""
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// commandLine is line.exe's own command line with its program name replaced by
// target, so linecli.exe parses exactly the arguments line.exe was given: none
// is split, joined or quoted again on the way.
func commandLine(target, raw string) string {
	return `"` + target + `"` + afterProgramName(raw)
}

// afterProgramName returns what follows the program name in a Windows command
// line, which ends, as CommandLineToArgvW reads it, at the next quote when it
// starts with one, otherwise at the first space or tab. It starts with a space
// or tab unless it is empty.
func afterProgramName(raw string) string {
	var rest string
	if strings.HasPrefix(raw, `"`) {
		if i := strings.IndexByte(raw[1:], '"'); i >= 0 {
			rest = raw[i+2:]
		}
	} else if i := strings.IndexAny(raw, " \t"); i >= 0 {
		rest = raw[i:]
	}
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		rest = " " + rest
	}
	return rest
}

// joinKillOnCloseJob puts line.exe in a job that Windows ends when line.exe's
// last handle to it closes, which happens when line.exe exits or is killed.
// linecli.exe, started afterwards, joins the same job, so stopping line.exe
// (a timeout, Task Manager) never leaves linecli.exe running on its own, for
// example sending a message nobody waits for. Best effort: on failure
// line.exe still runs linecli.exe, as before.
func joinKillOnCloseJob() {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	// BREAKAWAY_OK is for the one process that must outlive line.exe: what
	// line uninstall leaves to delete line.exe itself. Nothing else asks for it.
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return
	}
	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		windows.CloseHandle(job)
	}
	// The handle stays open until line.exe exits.
}

// fail prints an error the way linecli does (JSON on stdout, exit code 1).
func fail(msg string) int {
	b, _ := json.Marshal(struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}{"ERROR", msg})
	fmt.Fprintln(os.Stdout, string(b))
	return 1
}
