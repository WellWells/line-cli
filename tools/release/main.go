// Command release cuts a line-cli release on this computer: it adds the
// release's section to CHANGELOG.md from the commit subjects since the last
// tag, commits it and creates the annotated tag. It does not push; pushing
// the tag publishes the release (.github/workflows/release.yml).
//
// line-cli is in beta: a build's version is the time of the commit it was
// built from, 0.YYYYMMDD.HHMM in Taiwan time (cli.BetaVersion), so a beta
// release is named after its release commit's time. Official releases will be
// 1.x.y, set in internal/cli/version.go: x for new features or other changes,
// y for bug fixes only.
//
//	go run ./tools/release -n      # preview the version and the changelog section
//	go run ./tools/release         # a beta release, such as v0.20261031.1530
//	go run ./tools/release 1.0.0   # the first official release
//	go run ./tools/release minor   # after it, 1.0.0 -> 1.1.0: new features or other changes
//	go run ./tools/release patch   # 1.1.0 -> 1.1.1: bug fixes only
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/WellWells/line-cli/internal/cli"
)

const (
	versionFile   = "internal/cli/version.go"
	changelogFile = "CHANGELOG.md"
)

// now is the release time; tests replace it.
var now = time.Now

type version struct{ major, minor, patch int }

func (v version) String() string { return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch) }
func (v version) tag() string    { return "v" + v.String() }

func (v version) less(w version) bool {
	if v.major != w.major {
		return v.major < w.major
	}
	if v.minor != w.minor {
		return v.minor < w.minor
	}
	return v.patch < w.patch
}

var versionRe = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// parseVersion reads "1.2.3" or "v1.2.3"; pre-release and build suffixes are
// not used by line-cli.
func parseVersion(s string) (version, error) {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return version{}, fmt.Errorf("%q is not a version like 1.2.3", s)
	}
	n := make([]int, 3)
	for i := range n {
		var err error
		if n[i], err = strconv.Atoi(m[i+1]); err != nil {
			return version{}, fmt.Errorf("%q: %w", s, err)
		}
	}
	return version{n[0], n[1], n[2]}, nil
}

// official is the official release after cur, the release constant ("" in
// beta), for "minor", "patch" or an explicit 1.x.y version.
func official(cur, arg string) (version, error) {
	var c version
	if cur != "" {
		var err error
		if c, err = parseVersion(cur); err != nil {
			return version{}, fmt.Errorf("%s: %w", versionFile, err)
		}
	}
	var v version
	switch {
	case cur == "" && (arg == "minor" || arg == "patch"):
		return version{}, errors.New("line-cli is in beta: run without arguments for a beta release, or give the first official version, 1.0.0")
	case arg == "minor":
		v = version{c.major, c.minor + 1, 0}
	case arg == "patch":
		v = version{c.major, c.minor, c.patch + 1}
	default:
		var err error
		if v, err = parseVersion(arg); err != nil {
			return version{}, err
		}
	}
	if v.major < 1 {
		return version{}, fmt.Errorf("%s: official releases are 1.x.y; a beta takes its version from the commit time, so run without arguments", v)
	}
	if cur != "" && !c.less(v) {
		return version{}, fmt.Errorf("%s is not after %s", v, c)
	}
	return v, nil
}

var releaseRe = regexp.MustCompile(`(?m)^const release = "([^"]*)"\r?$`)

// readRelease returns the release constant in internal/cli/version.go: the
// official version, or "" in beta.
func readRelease(src string) (string, error) {
	m := releaseRe.FindAllStringSubmatch(src, -1)
	if len(m) != 1 {
		return "", fmt.Errorf("%s must have exactly one line `const release = \"...\"`", versionFile)
	}
	return m[0][1], nil
}

func setRelease(src string, v version) string {
	return releaseRe.ReplaceAllLiteralString(src, `const release = "`+v.String()+`"`)
}

var headingRe = regexp.MustCompile(`(?m)^## v?([0-9]+\.[0-9]+\.[0-9]+)\b`)

// topVersion is the version of the newest section in CHANGELOG.md.
func topVersion(changelog string) (version, error) {
	m := headingRe.FindStringSubmatch(changelog)
	if m == nil {
		return version{}, fmt.Errorf("%s has no section like \"## 0.1.0 - 2026-10-31\"", changelogFile)
	}
	return parseVersion(m[1])
}

// insertSection puts a release's section above the newest one, keeping the
// introduction at the top of CHANGELOG.md.
func insertSection(changelog, section string) string {
	if loc := headingRe.FindStringIndex(changelog); loc != nil {
		return changelog[:loc[0]] + section + "\n" + changelog[loc[0]:]
	}
	if changelog != "" && !strings.HasSuffix(changelog, "\n") {
		changelog += "\n"
	}
	return changelog + "\n" + section
}

type commit struct{ hash, subject string }

// entry is one changelog line made from a conventional commit subject such
// as "feat(chat)!: ...".
type entry struct {
	kind     string // feat, fix, ... ; "" when the subject is not conventional
	scope    string
	breaking bool
	text     string
	hash     string
}

var subjectRe = regexp.MustCompile(`^([a-z]+)(?:\(([^)]*)\))?(!)?: *(.+)$`)

func parseSubject(c commit) entry {
	m := subjectRe.FindStringSubmatch(c.subject)
	if m == nil {
		return entry{text: c.subject, hash: c.hash}
	}
	text := m[4]
	if m[2] != "" {
		text = m[2] + ": " + text
	}
	return entry{kind: m[1], scope: m[2], breaking: m[3] == "!", text: text, hash: c.hash}
}

// skipped commit types say nothing to someone choosing or updating a version.
var skipped = map[string]bool{"test": true, "style": true, "ci": true}

// internalScopes are the private core and the public/private split. They do not
// change what line-cli does, and CHANGELOG.md is a public file, so their commits
// stay out of it even as feat or fix.
var internalScopes = map[string]bool{"cshared": true, "public": true}

// groups lists the changelog headings in order.
var groups = []struct {
	title string
	match func(entry) bool
}{
	{"Breaking changes", func(e entry) bool { return e.breaking }},
	{"Features", func(e entry) bool { return e.kind == "feat" }},
	{"Fixes", func(e entry) bool { return e.kind == "fix" }},
	{"Other", func(entry) bool { return true }},
}

func isRelease(c commit) bool { return strings.HasPrefix(c.subject, "chore(release)") }

// notes is the body of a release's section: the commits grouped by type, in
// the order given (oldest first) within each group.
func notes(commits []commit) string {
	var b strings.Builder
	used := make([]bool, len(commits))
	entries := make([]entry, len(commits))
	for i, c := range commits {
		entries[i] = parseSubject(c)
		used[i] = isRelease(c) || skipped[entries[i].kind] || internalScopes[entries[i].scope]
	}
	for _, g := range groups {
		var lines []string
		for i, e := range entries {
			if !used[i] && g.match(e) {
				used[i] = true
				lines = append(lines, "- "+e.text+" ("+e.hash+")")
			}
		}
		if len(lines) > 0 {
			fmt.Fprintf(&b, "### %s\n\n%s\n\n", g.title, strings.Join(lines, "\n"))
		}
	}
	if b.Len() == 0 {
		return "- Tests and internal changes only. Commands and output are unchanged.\n"
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func section(v version, t time.Time, commits []commit) string {
	return "## " + v.String() + " - " + t.In(cli.Taipei).Format("2006-01-02 15:04") + "\n\n" + notes(commits)
}

// needsMinor reports why a patch release would break the 1.x.y rule: y is for
// bug fixes only, so new features and incompatible changes go into x.
func needsMinor(commits []commit) string {
	for _, c := range commits {
		if e := parseSubject(c); e.breaking || e.kind == "feat" {
			return c.hash + " " + c.subject
		}
	}
	return ""
}

func gitWith(env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimRight(out.String(), "\r\n"), nil
}

func git(args ...string) (string, error) { return gitWith(nil, args...) }

// commitsSince lists the non-merge commits after tag, oldest first.
func commitsSince(tag string) ([]commit, error) {
	out, err := git("log", "--no-merges", "--reverse", "--format=%h%x09%s", tag+"..HEAD")
	if err != nil || out == "" {
		return nil, err
	}
	var cs []commit
	for ln := range strings.SplitSeq(out, "\n") {
		h, s, _ := strings.Cut(strings.TrimRight(ln, "\r"), "\t")
		cs = append(cs, commit{h, s})
	}
	return cs, nil
}

func runGo(out io.Writer, args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	fs.SetOutput(out)
	dryRun := fs.Bool("n", false, "only show the new version and changelog section; change nothing")
	noTest := fs.Bool("no-test", false, "skip go vet and go test")
	fs.Usage = func() {
		fmt.Fprintln(out, "usage: go run ./tools/release [-n] [-no-test] [1.x.y|minor|patch]")
		fmt.Fprintln(out, "Without a version it cuts a beta release named after the commit time, 0.YYYYMMDD.HHMM.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		fs.Usage()
		return errors.New("give at most one version")
	}
	if _, err := os.Stat("go.mod"); err != nil {
		return errors.New("run from the repository root")
	}

	src, err := os.ReadFile(versionFile)
	if err != nil {
		return err
	}
	cur, err := readRelease(string(src))
	if err != nil {
		return err
	}
	changelog, err := os.ReadFile(changelogFile)
	if err != nil {
		return err
	}
	lastTag, err := git("describe", "--tags", "--abbrev=0", "--match", "v[0-9]*")
	if err != nil {
		return fmt.Errorf("no release tag before HEAD: %w", err)
	}
	last, err := parseVersion(lastTag)
	if err != nil {
		return fmt.Errorf("the last tag: %w", err)
	}
	if top, err := topVersion(string(changelog)); err != nil {
		return err
	} else if top != last {
		return fmt.Errorf("%s starts at %s but the last tag is %s", changelogFile, top, lastTag)
	}
	commits, err := commitsSince(lastTag)
	if err != nil {
		return err
	}
	if len(commits) == 0 {
		return fmt.Errorf("no commits since %s", lastTag)
	}

	t := now()
	var v version
	if fs.NArg() == 0 {
		if cur != "" {
			return fmt.Errorf("line-cli is past the beta (release %s in %s); give minor or patch", cur, versionFile)
		}
		if v, err = parseVersion(cli.BetaVersion(t)); err != nil {
			return err
		}
	} else {
		if v, err = official(cur, fs.Arg(0)); err != nil {
			return err
		}
		if last.major >= 1 && v.major == last.major && v.minor == last.minor {
			if c := needsMinor(commits); c != "" {
				return fmt.Errorf("%s: y is for bug fixes only, but %s has new features or incompatible changes (%s); use minor", v, lastTag, c)
			}
		}
	}
	if !last.less(v) {
		return fmt.Errorf("%s is not after the last release %s", v, lastTag)
	}
	if _, err := git("rev-parse", "-q", "--verify", "refs/tags/"+v.tag()); err == nil {
		return fmt.Errorf("tag %s already exists", v.tag())
	}
	sec := section(v, t, commits)
	if *dryRun {
		fmt.Fprintf(out, "%s -> %s, %d commits. %s would get:\n\n%s\n", lastTag, v.tag(), len(commits), changelogFile, sec)
		return nil
	}

	if st, err := git("status", "--porcelain"); err != nil {
		return err
	} else if st != "" {
		return fmt.Errorf("the working tree has changes; commit them or set them aside first:\n%s", st)
	}
	if !*noTest {
		for _, a := range [][]string{{"vet", "./..."}, {"test", "./..."}} {
			if err := runGo(out, a...); err != nil {
				return err
			}
		}
	}
	if fs.NArg() > 0 {
		if err := os.WriteFile(versionFile, []byte(setRelease(string(src), v)), 0o644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(changelogFile, []byte(insertSection(string(changelog), sec)), 0o644); err != nil {
		return err
	}
	if _, err := git("add", "--", versionFile, changelogFile); err != nil {
		return err
	}
	// The release commit is dated t: a beta build of it gets version v.
	date := t.Format(time.RFC3339)
	if _, err := gitWith([]string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}, "commit", "-q", "-m", "chore(release): "+v.tag()); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		ct, err := git("log", "-1", "--format=%ct")
		if err != nil {
			return err
		}
		unix, _ := strconv.ParseInt(ct, 10, 64)
		if got := cli.BetaVersion(time.Unix(unix, 0)); got != v.String() {
			return fmt.Errorf("the release commit's time gives version %s, not %s; it was committed but not tagged", got, v)
		}
	}
	_, body, _ := strings.Cut(sec, "\n\n")
	if _, err := git("tag", "-a", v.tag(), "-m", "line-cli "+v.tag()+"\n\n"+body); err != nil {
		return err
	}
	head, _ := git("rev-parse", "--short", "HEAD")
	push := v.tag()
	if branch, _ := git("branch", "--show-current"); branch != "" {
		push = branch + " " + push
	}
	fmt.Fprintf(out, "Released %s at %s (%s updated). To publish it:\n\n  git push origin %s\n", v.tag(), head, changelogFile, push)
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "release:", err)
		}
		os.Exit(1)
	}
}
