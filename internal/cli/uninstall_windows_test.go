package cli

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestUninstallPlan(t *testing.T) {
	root := filepath.Join(`C:\Users\a\AppData\Local`, "linecli")
	exe := filepath.Join(root, "bin", "linecli.exe")
	installed := func(p string) bool { return strings.EqualFold(p, filepath.Join(root, "cmd", "line.exe")) }

	p, err := newUninstallPlan(exe, root, installed)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.targets, []string{root}) {
		t.Errorf("default install: targets %q", p.targets)
	}

	// LINECLI_INSTALL_DIR puts the program apart from the data.
	other := `D:\tools\line`
	exe2 := filepath.Join(other, "bin", "linecli.exe")
	p, err = newUninstallPlan(exe2, root, func(q string) bool { return strings.EqualFold(q, filepath.Join(other, "cmd", "line.exe")) })
	if err != nil {
		t.Fatal(err)
	}
	want := []string{exe2, filepath.Join(other, "cmd", "line.exe"), root}
	if !slices.Equal(p.targets, want) {
		t.Errorf("custom install: targets %q, want %q", p.targets, want)
	}

	// Anything else is a build, and its folder must never be deleted.
	for _, c := range []struct {
		exe    string
		exists bool
	}{
		{`C:\repo\linecli.exe`, true},
		{`C:\repo\bin\linecli.exe`, false},
		{`C:\repo\bin\other.exe`, true},
		{`C:\Temp\go-build1\exe\linecli.exe`, true},
		{`C:\linecli.exe`, true},
	} {
		if _, err := newUninstallPlan(c.exe, root, func(string) bool { return c.exists }); err == nil {
			t.Errorf("%s (launcher %v): want an error", c.exe, c.exists)
		}
	}
}

func TestPathWithout(t *testing.T) {
	cmd, bin := `C:\Users\a\AppData\Local\linecli\cmd`, `C:\Users\a\AppData\Local\linecli\bin`
	expand := func(s string) string { return strings.ReplaceAll(s, "%LOCALAPPDATA%", `C:\Users\a\AppData\Local`) }
	for _, c := range []struct{ in, want string }{
		{`C:\x;` + cmd + `;C:\y`, `C:\x;C:\y`},
		{cmd, ``},
		{`%LOCALAPPDATA%\linecli\cmd\;C:\y`, `C:\y`},
		{strings.ToUpper(bin) + `;%LOCALAPPDATA%\Other`, `%LOCALAPPDATA%\Other`},
		{`C:\x;;C:\y`, `C:\x;;C:\y`}, // nothing of ours: left exactly as it was
		{`C:\linecli\cmd2;C:\y`, `C:\linecli\cmd2;C:\y`},
	} {
		if got := pathWithout(c.in, []string{cmd, bin}, expand); got != c.want {
			t.Errorf("pathWithout(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// removeFromPath must keep the value's type, or %VARIABLE% entries stop
// expanding for every other program on the PATH.
func TestRemoveFromPathKeepsType(t *testing.T) {
	const keyPath = `Software\line-cli-test\Environment`
	k, _, err := registry.CreateKey(registry.CURRENT_USER, keyPath, registry.ALL_ACCESS)
	if err != nil {
		t.Skip(err)
	}
	defer func() {
		k.Close()
		registry.DeleteKey(registry.CURRENT_USER, keyPath)
		registry.DeleteKey(registry.CURRENT_USER, `Software\line-cli-test`)
	}()
	if err := k.SetExpandStringValue("Path", `%USERPROFILE%\x;C:\line\cmd`); err != nil {
		t.Fatal(err)
	}
	changed, err := removeFromPath(keyPath, []string{`C:\line\cmd`})
	if err != nil || !changed {
		t.Fatalf("changed %v, err %v", changed, err)
	}
	got, kind, err := k.GetStringValue("Path")
	if err != nil || got != `%USERPROFILE%\x` || kind != registry.EXPAND_SZ {
		t.Errorf("got %q kind %d err %v", got, kind, err)
	}
	if changed, err = removeFromPath(keyPath, []string{`C:\line\cmd`}); err != nil || changed {
		t.Errorf("second run: changed %v, err %v", changed, err)
	}
}

func TestUninstallArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"uninstall", "x"}, "unexpected argument"},
		{[]string{"uninstall", "--force"}, "-force"},
		// The test binary is not an installed line-cli, so nothing can go.
		{[]string{"uninstall", "-y"}, "installed"},
		{[]string{"uninstall"}, "installed"},
	} {
		rc, out, errs := runCLIErr(c.args...)
		if rc != 1 || !strings.Contains(string(out)+errs, c.want) {
			t.Errorf("%v: exit %d: %s%s", c.args, rc, string(out), errs)
		}
	}
}
