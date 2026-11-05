package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/WellWells/line-cli/internal/line"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const userEnvironmentKey = "Environment"

// uninstallResult is the output of line uninstall.
type uninstallResult struct {
	Removed []string      `json:"removed"`
	Skills  []skillResult `json:"skills"`
}

// uninstallPlan is what line uninstall removes. The two programs go one by one
// and their folders only when that leaves them empty: LINECLI_INSTALL_DIR can
// point at a folder other programs share. The store is all line-cli's own.
type uninstallPlan struct {
	exe, launcher string
	root          string
	store         string
	targets       []string // what the person is told, and what must be gone at the end
}

// newUninstallPlan refuses anything but the installed layout, so a build run
// from a repository or a temporary folder can never delete where it sits.
func newUninstallPlan(exe, store string, exists func(string) bool) (*uninstallPlan, error) {
	bin := filepath.Dir(exe)
	root := filepath.Dir(bin)
	launcher := filepath.Join(root, "cmd", "line.exe")
	if !strings.EqualFold(filepath.Base(exe), "linecli.exe") || !strings.EqualFold(filepath.Base(bin), "bin") || !exists(launcher) {
		return nil, invalid("line uninstall removes an installed line-cli, and this program is not one: " + exe)
	}
	p := &uninstallPlan{exe: exe, launcher: launcher, root: root, store: store, targets: []string{store}}
	if !strings.EqualFold(root, store) {
		p.targets = []string{exe, launcher, store}
	}
	return p, nil
}

func (rc *runCtx) uninstall(args []string) int {
	fs := newFlags("uninstall")
	yes := fs.Bool("yes", false, "")
	y := fs.Bool("y", false, "")
	if err := parseArgs(fs, args, "yes", "y"); err != nil {
		return rc.fail(err)
	}
	if fs.NArg() > 0 {
		return rc.fail(invalid(fmt.Sprintf("unexpected argument(s) %q", fs.Args())))
	}
	exe, err := os.Executable()
	if err != nil {
		return rc.fail(err)
	}
	p, err := newUninstallPlan(exe, line.StoreDir(), func(path string) bool {
		fi, err := os.Stat(path)
		return err == nil && fi.Mode().IsRegular()
	})
	if err != nil {
		return rc.fail(err)
	}
	if !*yes && !*y {
		if !canPrompt() {
			return rc.fail(invalid("line uninstall deletes line-cli, its key cache and its downloads; add -y to go ahead"))
		}
		if !rc.confirmUninstall(p, userLang()) {
			return rc.fail(errors.New("cancelled; nothing was removed"))
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return rc.fail(err)
	}
	// Moving the running programs comes first: it is what can be refused, and
	// up to here line-cli still works.
	trash, err := p.moveProgramsOut()
	if err != nil {
		return rc.fail(err)
	}
	res := uninstallResult{Removed: p.targets, Skills: newSkillSetup(os.Getenv, home).uninstall()}
	var problems []string
	for _, r := range res.Skills {
		if r.Status == "failed" {
			problems = append(problems, r.Path+": "+r.Note)
		}
	}
	dirs := []string{filepath.Dir(p.launcher), filepath.Dir(p.exe)}
	if changed, err := removeFromPath(userEnvironmentKey, dirs); err != nil {
		problems = append(problems, "the user PATH: "+err.Error())
	} else if changed {
		announceEnvironmentChange()
	}
	if err := os.RemoveAll(p.store); err != nil {
		problems = append(problems, err.Error())
	}
	for _, d := range append(dirs, p.root) {
		os.Remove(d) // only when empty
	}
	for _, t := range p.targets {
		if _, err := os.Lstat(t); !errors.Is(err, os.ErrNotExist) {
			problems = append(problems, t+" is still there")
		}
	}
	if err := removeLater(trash); err != nil {
		fmt.Fprintf(rc.err, "line: note: delete %s yourself: %v\n", trash, err)
	}
	if len(problems) > 0 {
		return rc.fail(errors.New("line-cli was only partly removed: " + strings.Join(problems, "; ")))
	}
	if !rc.text {
		return rc.emit(res)
	}
	for _, t := range res.Removed {
		fmt.Fprintln(rc.out, t)
	}
	return 0
}

func (rc *runCtx) confirmUninstall(p *uninstallPlan, l lang) bool {
	fmt.Fprintln(rc.err, l.pick([2]string{
		"This deletes line-cli, its skill for AI agents, and these files, including anything downloaded here:",
		"這會刪除 line-cli、AI agent 的 skill，以及下列檔案，包含下載到這裡的檔案：",
	}))
	for _, t := range p.targets {
		fmt.Fprintln(rc.err, "  "+t)
	}
	fmt.Fprint(rc.err, l.pick([2]string{"Continue? [y/N] ", "確定要刪除嗎？[y/N] "}))
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

// moveProgramsOut moves linecli.exe and line.exe, both running, to a new folder
// beside the install folder: Windows lets a running program be moved on its
// volume but not deleted. It returns that folder, or an error with both back
// in place.
func (p *uninstallPlan) moveProgramsOut() (string, error) {
	trash := filepath.Join(filepath.Dir(p.root), fmt.Sprintf("linecli-uninstall-%d", os.Getpid()))
	if err := os.Mkdir(trash, 0o700); err != nil {
		return "", err
	}
	exe := filepath.Join(trash, filepath.Base(p.exe))
	if err := os.Rename(p.exe, exe); err != nil {
		os.Remove(trash)
		return "", err
	}
	if err := os.Rename(p.launcher, filepath.Join(trash, filepath.Base(p.launcher))); err != nil {
		os.Rename(exe, p.exe)
		os.Remove(trash)
		return "", err
	}
	return trash, nil
}

// removeLater deletes dir once the programs in it have exited, which they can
// only leave to another process. line.exe's job ends everything in it when
// line.exe exits, so that process breaks away from the job; a job that does not
// allow it (an agent's sandbox, say) only means it may not finish.
func removeLater(dir string) error {
	shell := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	script := fmt.Sprintf(`"%s" /d /c for /l %%i in (1,1,10) do @if exist "%s" (ping -n 2 127.0.0.1 >nul & rmdir /s /q "%s" 2>nul)`, shell, dir, dir)
	start := func(flags uint32) error {
		c := exec.Command(shell)
		c.Dir = filepath.Dir(shell) // not a folder being removed
		c.SysProcAttr = &syscall.SysProcAttr{CmdLine: script, CreationFlags: flags}
		return c.Start()
	}
	if start(windows.CREATE_NO_WINDOW|windows.CREATE_BREAKAWAY_FROM_JOB) == nil {
		return nil
	}
	return start(windows.CREATE_NO_WINDOW)
}

// pathWithout returns a PATH value without the entries that are dirs. A value
// with none of them comes back unchanged, empty entries and all.
func pathWithout(value string, dirs []string, expand func(string) string) string {
	same := func(a, b string) bool { return strings.EqualFold(strings.TrimRight(a, `\`), strings.TrimRight(b, `\`)) }
	entries := strings.Split(value, ";")
	kept := make([]string, 0, len(entries))
	for _, e := range entries {
		ours := false
		for _, d := range dirs {
			ours = ours || same(expand(e), d)
		}
		if !ours {
			kept = append(kept, e)
		}
	}
	if len(kept) == len(entries) {
		return value
	}
	return strings.Join(kept, ";")
}

// removeFromPath takes dirs off the Path value under the given key, keeping the
// value's type (usually REG_EXPAND_SZ) and its %VARIABLE% entries.
func removeFromPath(keyPath string, dirs []string) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer k.Close()
	value, kind, err := k.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	next := pathWithout(value, dirs, func(s string) string {
		if full, err := registry.ExpandString(s); err == nil {
			return full
		}
		return s
	})
	if next == value {
		return false, nil
	}
	if kind == registry.EXPAND_SZ {
		return true, k.SetExpandStringValue("Path", next)
	}
	return true, k.SetStringValue("Path", next)
}

// announceEnvironmentChange tells Explorer the environment changed, so terminals
// opened from now on get the new PATH. Best effort, like the install script.
func announceEnvironmentChange() {
	const (
		broadcast       = 0xffff
		wmSettingChange = 0x1a
		abortIfHung     = 2
	)
	name, err := windows.UTF16PtrFromString(userEnvironmentKey)
	if err != nil {
		return
	}
	var result uintptr
	windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW").Call(
		broadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(name)), abortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
}
