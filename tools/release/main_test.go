package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOfficialVersion(t *testing.T) {
	for _, c := range []struct{ cur, arg, want string }{
		{"", "1.0.0", "1.0.0"},
		{"", "v1.0.0", "1.0.0"},
		{"1.0.0", "minor", "1.1.0"},
		{"1.1.0", "patch", "1.1.1"},
		{"1.1.1", "1.3.0", "1.3.0"},
	} {
		v, err := official(c.cur, c.arg)
		if err != nil || v.String() != c.want {
			t.Errorf("official(%q, %q) = %s, %v; want %s", c.cur, c.arg, v, err, c.want)
		}
	}
	for _, c := range []struct{ cur, arg, why string }{
		{"", "minor", "beta"},
		{"", "patch", "beta"},
		{"", "0.3.0", "1.x.y"},
		{"1.1.0", "1.1.0", "not after"},
		{"1.1.0", "1.0.9", "not after"},
		{"1.1.0", "1.2", "not a version"},
		{"1.1.0", "1.2.0-rc1", "not a version"},
		{"1.1.0", "", "not a version"},
	} {
		if v, err := official(c.cur, c.arg); err == nil || !strings.Contains(err.Error(), c.why) {
			t.Errorf("official(%q, %q) = %s, %v; want an error with %q", c.cur, c.arg, v, err, c.why)
		}
	}
}

func TestSetReleaseRewritesOnlyTheConstant(t *testing.T) {
	src := "package cli\n\n// const release = \"x\" in a comment\nconst release = \"\"\n\nvar s = \"const release = \\\"9.9.9\\\"\"\n"
	cur, err := readRelease(src)
	if err != nil || cur != "" {
		t.Fatalf("readRelease = %q, %v", cur, err)
	}
	got := setRelease(src, version{1, 0, 0})
	if want := strings.Replace(src, "const release = \"\"\n", "const release = \"1.0.0\"\n", 1); got != want {
		t.Fatalf("setRelease:\n%s\nwant:\n%s", got, want)
	}
	if _, err := readRelease("package cli\n"); err == nil {
		t.Error("readRelease without the constant should fail")
	}
}

func TestInsertSectionKeepsIntroOnTop(t *testing.T) {
	log := "# Changelog\n\nIntro.\n\n## 0.1.0 - 2026-10-31\n\n- First release\n"
	got := insertSection(log, "## 0.20261102.930 - 2026-11-02 09:30\n\n- new\n")
	want := "# Changelog\n\nIntro.\n\n## 0.20261102.930 - 2026-11-02 09:30\n\n- new\n\n## 0.1.0 - 2026-10-31\n\n- First release\n"
	if got != want {
		t.Fatalf("insertSection:\n%s\nwant:\n%s", got, want)
	}
	if v, err := topVersion(got); err != nil || v != (version{0, 20261102, 930}) {
		t.Fatalf("topVersion = %s, %v", v, err)
	}
	if got := insertSection("# Changelog", "## 0.1.0 - 2026-10-31\n"); got != "# Changelog\n\n## 0.1.0 - 2026-10-31\n" {
		t.Fatalf("insertSection into a changelog without sections: %q", got)
	}
}

func TestNotesGroupCommitsByType(t *testing.T) {
	got := notes([]commit{
		{"a1", "fix(read): fix paging"},
		{"a2", "feat(chat): accept chat_id prefixes"},
		{"a3", "test(live): tests only"},
		{"a4", "docs: update README"},
		{"a5", "feat(output)!: omit IDs by default"},
		{"a6", "chore(release): v0.1.1"},
		{"a7", "Old message before merge"},
		{"a8", "style: gofmt"},
	})
	want := "### Breaking changes\n\n- output: omit IDs by default (a5)\n\n" +
		"### Features\n\n- chat: accept chat_id prefixes (a2)\n\n" +
		"### Fixes\n\n- read: fix paging (a1)\n\n" +
		"### Other\n\n- update README (a4)\n- Old message before merge (a7)\n"
	if got != want {
		t.Fatalf("notes:\n%s\nwant:\n%s", got, want)
	}
	if got := notes([]commit{{"b1", "test: x"}}); !strings.Contains(got, "internal changes") {
		t.Fatalf("notes with only tests: %q", got)
	}
}

// CHANGELOG.md is a public file: commits about the private core and the
// public/private split must not reach it, even when they are feat or fix.
func TestNotesLeaveOutInternalWork(t *testing.T) {
	got := notes([]commit{
		{"a1", "feat(cshared): export line_x"},
		{"a2", "fix(public): check markers"},
		{"a3", "ci(release): update workflow"},
		{"a4", "feat(chat): a user-facing feature"},
	})
	want := "### Features\n\n- chat: a user-facing feature (a4)\n"
	if got != want {
		t.Fatalf("notes:\n%s\nwant:\n%s", got, want)
	}
}

func TestNeedsMinor(t *testing.T) {
	if c := needsMinor([]commit{{"a", "fix: x"}, {"b", "docs: y"}}); c != "" {
		t.Errorf("fixes only, got %q", c)
	}
	for _, s := range []string{"feat(chat): x", "fix(cli)!: y"} {
		if needsMinor([]commit{{"a", "fix: z"}, {"b", s}}) == "" {
			t.Errorf("%q should need a minor release", s)
		}
	}
}

// TestRepoVersionMatchesChangelog keeps the release constant and CHANGELOG.md
// in step: an official version set by hand without its section fails here.
func TestRepoVersionMatchesChangelog(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", versionFile))
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(filepath.Join("..", "..", changelogFile))
	if err != nil {
		t.Fatal(err)
	}
	cur, err := readRelease(string(src))
	if err != nil {
		t.Fatal(err)
	}
	top, err := topVersion(string(log))
	if err != nil {
		t.Fatal(err)
	}
	if cur != "" && cur != top.String() {
		t.Fatalf("release in %s is %s but %s starts at %s; release with go run ./tools/release", versionFile, cur, changelogFile, top)
	}
	if cur == "" && top.major != 0 {
		t.Fatalf("%s starts at %s but %s is still in beta", changelogFile, top, versionFile)
	}
}

// TestReleaseInGitRepo cuts releases in a throwaway repository: a beta named
// after the commit time, then 1.0.0, the patch rule and a minor release.
func TestReleaseInGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.gitconfig")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Keep the user's and the system's git settings (signing, hooks, autocrlf) out.
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "test")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "test@example.com")
	}
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "internal", "cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	defer func(f func() time.Time) { now = f }(now)
	at := func(s string) {
		tm, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		now = func() time.Time { return tm }
	}
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(name, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	gitT := func(args ...string) string {
		t.Helper()
		out, err := git(args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	n := 0
	commitAs := func(subject string) {
		t.Helper()
		n++
		write("f.txt", strings.Repeat("x", n))
		gitT("add", ".")
		gitT("commit", "-q", "-m", subject)
	}
	release := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := run(append([]string{"-no-test"}, args...), &out)
		return out.String(), err
	}
	wantErr := func(want string, args ...string) {
		t.Helper()
		if _, err := release(args...); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("release %v: %v, want an error with %q", args, err, want)
		}
	}

	write("go.mod", "module example.com/x\n\ngo 1.27\n")
	write(versionFile, "package cli\n\nconst release = \"\"\n")
	write(changelogFile, "# Changelog\n\n## 0.1.0 - 2026-10-31\n\n- First release\n")
	gitT("init", "-q", "-b", "main")
	gitT("add", ".")
	gitT("commit", "-q", "-m", "init")
	gitT("tag", "-a", "v0.1.0", "-m", "v0.1.0")
	at("2026-11-02T01:30:45Z") // 09:30 in Taiwan
	wantErr("no commits since v0.1.0")
	commitAs("fix(read): fix paging")
	commitAs("feat(chat): accept prefixes")
	commitAs("test: add tests")

	// Beta: no version argument, named after the release commit's time.
	wantErr("in beta", "minor")
	wantErr("1.x.y", "0.3.0")
	out, err := release("-n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "v0.1.0 -> v0.20261102.930, 3 commits") || !strings.Contains(out, "- chat: accept prefixes (") {
		t.Fatalf("dry run output:\n%s", out)
	}
	write("untracked.txt", "x")
	wantErr("working tree")
	if err := os.Remove("untracked.txt"); err != nil {
		t.Fatal(err)
	}
	if st := gitT("status", "--porcelain"); st != "" {
		t.Fatalf("the dry run and the refused runs changed files:\n%s", st)
	}
	if out, err = release(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "git push origin main v0.20261102.930") {
		t.Fatalf("release output:\n%s", out)
	}
	if got := read(versionFile); got != "package cli\n\nconst release = \"\"\n" {
		t.Fatalf("a beta release changed %s:\n%s", versionFile, got)
	}
	log := read(changelogFile)
	if !strings.HasPrefix(log, "# Changelog\n\n## 0.20261102.930 - 2026-11-02 09:30\n\n### Features\n\n- chat: accept prefixes (") ||
		!strings.Contains(log, "### Fixes\n\n- read: fix paging (") || strings.Contains(log, "add tests") ||
		!strings.Contains(log, "\n## 0.1.0 - 2026-10-31\n") {
		t.Fatalf("%s:\n%s", changelogFile, log)
	}
	if got := gitT("log", "-1", "--format=%s %ct"); got != "chore(release): v0.20261102.930 "+strconv.FormatInt(now().Unix(), 10) {
		t.Fatalf("release commit %q", got)
	}
	if got := gitT("describe", "--tags", "--exact-match"); got != "v0.20261102.930" {
		t.Fatalf("HEAD is tagged %q", got)
	}
	if msg := gitT("tag", "-l", "--format=%(contents)", "v0.20261102.930"); !strings.Contains(msg, "line-cli v0.20261102.930") || !strings.Contains(msg, "- chat: accept prefixes (") {
		t.Fatalf("tag message:\n%s", msg)
	}
	if st := gitT("status", "--porcelain"); st != "" {
		t.Fatalf("release left changes:\n%s", st)
	}
	wantErr("no commits since v0.20261102.930")

	// The first official release, then x for features and y for fixes only.
	commitAs("fix(send): fix a bug")
	at("2026-11-03T08:00:00Z")
	if _, err := release("1.0.0"); err != nil {
		t.Fatal(err)
	}
	if got := read(versionFile); !strings.Contains(got, "const release = \"1.0.0\"\n") {
		t.Fatalf("%s:\n%s", versionFile, got)
	}
	if v, _ := topVersion(read(changelogFile)); v != (version{1, 0, 0}) {
		t.Fatalf("%s starts at %s", changelogFile, v)
	}
	commitAs("feat(search): add a feature")
	wantErr("past the beta")
	wantErr("bug fixes only", "patch")
	if _, err := release("minor"); err != nil {
		t.Fatal(err)
	}
	if got := gitT("describe", "--tags", "--exact-match"); got != "v1.1.0" {
		t.Fatalf("HEAD is tagged %q", got)
	}
	commitAs("fix(search): fix a bug")
	if _, err := release("patch"); err != nil {
		t.Fatal(err)
	}
	if got := read(versionFile); !strings.Contains(got, "const release = \"1.1.1\"\n") {
		t.Fatalf("%s:\n%s", versionFile, got)
	}
}
