package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/WellWells/line-cli/internal/line"
)

// outOnlyFiles fails unless dir holds exactly the named entries.
func outOnlyFiles(t *testing.T, dir string, want ...string) {
	t.Helper()
	es, _ := os.ReadDir(dir)
	got := []string{}
	for _, e := range es {
		got = append(got, e.Name())
	}
	slices.Sort(want)
	if !slices.Equal(got, append([]string{}, want...)) {
		t.Fatalf("%d entries in %s, want %v", len(got), filepath.Base(dir), want)
	}
}

func outWantInvalid(t *testing.T, args []string, msg string) {
	t.Helper()
	code, out := runCLI(args...)
	var e errorOutput
	if code != 1 || json.Unmarshal(out, &e) != nil || e.Error != "INVALID_ARGUMENT" || !strings.Contains(e.Message, msg) {
		t.Errorf("%v: rc=%d out=%.200s", args, code, out)
	}
}

// A value-taking flag given last with no value must not take the "--" that
// parseArgs adds before the positional arguments.
func TestFlagMissingValueLast(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	cwd := t.TempDir()
	t.Chdir(cwd)
	for _, args := range [][]string{
		{"send", "--to", "u0", "--message"},
		{"send", "--to", "u0", "-y", "--message"},
		{"send", "--to", "u0", "--dry-run", "--compact", "--message"},
		{"preview", "c0", "--out"},
		{"download", "c0", "--out"},
		{"read", "c0", "--out"},
		{"version", "--out"},
		{"--out"},
		{"find-file", "--ext"},
		{"search", "x", "--chat"},
		{"chats", "--limit"},
	} {
		outWantInvalid(t, args, "needs an argument")
	}
	outOnlyFiles(t, cwd)
}

// --out FILE is checked before the command runs: a send must never happen
// when its result could not be saved.
func TestOutCheckedBeforeCommand(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	dir := t.TempDir()
	afile := filepath.Join(dir, "afile")
	if err := os.WriteFile(afile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(afile, "x.json")
	outWantInvalid(t, []string{"send", "--to", "u0", "--message", "m", "--dry-run", "--out", bad}, "-out")
	outWantInvalid(t, []string{"read", "c0", "--out", bad}, "-out")
	outWantInvalid(t, []string{"--out", bad, "version"}, "-out")
	outWantInvalid(t, []string{"version", "--out", filepath.Join(dir, "missing", "x.json")}, "does not create folders")
	outOnlyFiles(t, dir, "afile")

	// A command that fails after --out was accepted prints its error on
	// stdout and leaves neither the file nor the temp file behind.
	code, out := runCLI("read", "c0", "--out", filepath.Join(dir, "r.json"))
	var e errorOutput
	if code != 1 || json.Unmarshal(out, &e) != nil || e.Error == "" {
		t.Fatalf("failed command with --out: rc=%d out=%.200s", code, out)
	}
	outOnlyFiles(t, dir, "afile")

	// An existing file is replaced.
	path := filepath.Join(dir, "v.json")
	os.WriteFile(path, []byte("old"), 0o600)
	if code, _ := runCLI("help", "--json", "--out", path); code != 0 {
		t.Fatalf("existing file: rc=%d", code)
	}
	if b, _ := os.ReadFile(path); !json.Valid(b) {
		t.Fatal("existing file not replaced")
	}
	outOnlyFiles(t, dir, "afile", "v.json")
}

// If the file cannot be written after the command succeeded, the output is
// printed on stdout rather than lost.
func TestOutSaveFailureKeepsOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	var stdout, stderr bytes.Buffer
	rc := &runCtx{stdout: &stdout, err: &stderr}
	if err := rc.setOutFile(path); err != nil {
		t.Fatal(err)
	}
	rc.writeJSON(map[string]bool{"sent": true})
	if err := os.Mkdir(path, 0o700); err != nil { // the rename onto path now fails
		t.Fatal(err)
	}
	if code := rc.saveOut(); code != 0 {
		t.Fatalf("rc=%d", code)
	}
	var v map[string]bool
	if json.Unmarshal(stdout.Bytes(), &v) != nil || !v["sent"] || !strings.Contains(stderr.String(), "could not write --out") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	outOnlyFiles(t, dir, "x.json")
}

func TestHelpTextShowsGlobals(t *testing.T) {
	t.Setenv("LINECLI_LANG", "en")
	code, out := runCLI("read", "--help", "--text")
	for _, want := range []string{"Usage:", "--out FILE", "--jsonl", "--text", "line help read --json"} {
		if code != 0 || !strings.Contains(string(out), want) {
			t.Errorf("read --help lacks %q (rc=%d)", want, code)
		}
	}
}

func TestFindFileTextShowsLocalPath(t *testing.T) {
	var b bytes.Buffer
	rc := &runCtx{out: &b, text: true}
	rc.emitFiles([]line.FileHit{
		{SentAt: "t1", Filename: "a.pdf", LocalPath: `C:\x\a.pdf`},
		{SentAt: "t2", Filename: "b.pdf", ProbableLocalPath: `C:\y\b.pdf`},
	})
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	// --text escapes a backslash as \\ (see textEscapes).
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "\t"+`C:\\x\\a.pdf`) || !strings.HasSuffix(lines[1], "\t~"+`C:\\y\\b.pdf`) {
		t.Fatalf("text: %q", b.String())
	}
}
