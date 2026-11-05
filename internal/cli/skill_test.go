package cli

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	linecli "github.com/WellWells/line-cli"
)

// These run without LINE: the skill command and the skill itself.

// skillHome makes an empty home folder and data folder for one test, with
// nobody at the console unless the test says so, and returns the home folder.
func skillHome(t *testing.T, agentDirs ...string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	for _, d := range agentDirs {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	prompt, pick := canPrompt, runPicker
	t.Cleanup(func() { canPrompt, runPicker = prompt, pick })
	canPrompt = func() bool { return false }
	runPicker = func(*picker, lang) (bool, error) {
		t.Error("the checklist showed")
		return false, nil
	}
	return home
}

func runSkill(t *testing.T, args ...string) map[string]skillResult {
	t.Helper()
	code, out := runCLI(append([]string{"skill"}, args...)...)
	var rs []skillResult
	if code != 0 || json.Unmarshal(out, &rs) != nil || len(rs) == 0 || rs[0].Agent != sharedID {
		t.Fatalf("skill %v: rc=%d out=%s", args, code, out)
	}
	got := map[string]skillResult{}
	for _, r := range rs {
		got[r.Agent] = r
	}
	return got
}

func statuses(got map[string]skillResult) map[string]string {
	m := map[string]string{}
	for k, r := range got {
		m[k] = r.Status
	}
	return m
}

func wantStatuses(t *testing.T, step string, got map[string]skillResult, want map[string]string) {
	t.Helper()
	if g := statuses(got); !mapsEqual(g, want) {
		t.Errorf("%s: statuses %v, want %v", step, g, want)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func savedChoice(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(skillChoicePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var c skillChoice
	if err != nil || json.Unmarshal(b, &c) != nil || c.Agents == nil {
		t.Fatalf("saved choice: %q, %v", b, err)
	}
	return c.Agents
}

func readSkill(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("read %s: %v", path, err)
	}
	return string(b)
}

// With nobody to ask, install writes the skill in the shared folder and a copy
// for every agent found, without saving that as a choice.
func TestSkillInstallAndUninstall(t *testing.T) {
	home := skillHome(t, ".claude", `.gemini\config`) // Claude Code and Antigravity
	shared := filepath.Join(home, ".agents", "skills", "line-cli")
	claude := filepath.Join(home, ".claude", "skills", "line-cli")
	agy := filepath.Join(home, ".gemini", "config", "skills", "line-cli")

	got := runSkill(t, "install")
	wantStatuses(t, "install", got, map[string]string{sharedID: "installed", "claude-code": "installed", "antigravity": "installed"})
	if got["claude-code"].Path != filepath.Join(claude, "SKILL.md") {
		t.Errorf("claude-code: %+v", got["claude-code"])
	}
	for _, d := range []string{shared, claude, agy} {
		if linkTarget(d) != "" {
			t.Errorf("%s is a link, not a copy", d)
		}
		if readSkill(t, filepath.Join(d, "SKILL.md")) != linecli.Skill {
			t.Errorf("%s does not hold the skill", d)
		}
	}
	if c := savedChoice(t); c != nil {
		t.Errorf("saved %v, though nobody chose", c)
	}

	wantStatuses(t, "again", runSkill(t, "install"), map[string]string{sharedID: "unchanged", "claude-code": "unchanged", "antigravity": "unchanged"})
	for _, d := range []string{shared, claude} {
		if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wantStatuses(t, "over old copies", runSkill(t, "install"), map[string]string{sharedID: "updated", "claude-code": "updated", "antigravity": "unchanged"})

	// A copy with someone's own files beside it keeps them.
	keep := filepath.Join(agy, "notes.md")
	if err := os.WriteFile(keep, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	wantStatuses(t, "with notes", runSkill(t, "install"), map[string]string{sharedID: "unchanged", "claude-code": "unchanged", "antigravity": "unchanged"})

	got = runSkill(t, "uninstall")
	wantStatuses(t, "uninstall", got, map[string]string{sharedID: "removed", "claude-code": "removed", "antigravity": "removed"})
	for _, d := range []string{claude, shared} {
		if _, err := os.Lstat(d); !os.IsNotExist(err) {
			t.Errorf("uninstall left %s", d)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("uninstall removed a file it did not write")
	}
	if c := savedChoice(t); c == nil || len(c) != 0 {
		t.Errorf("after uninstall the saved choice is %v, want none", c)
	}
	// Updates then leave the skill uninstalled.
	got = runSkill(t, "install", "--update")
	wantStatuses(t, "update after uninstall", got, map[string]string{sharedID: "not_installed", "claude-code": "not_selected", "antigravity": "not_selected"})

	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude-elsewhere"))
	if err := os.Mkdir(filepath.Join(home, "claude-elsewhere"), 0o755); err != nil {
		t.Fatal(err)
	}
	got = runSkill(t, "install", "--agent", "claude-code")
	if got["claude-code"].Path != filepath.Join(home, "claude-elsewhere", "skills", "line-cli", "SKILL.md") || got["claude-code"].Status != "installed" {
		t.Errorf("CLAUDE_CONFIG_DIR: %+v", got["claude-code"])
	}
}

// --agent chooses, and the choice is saved: later installs without --agent
// follow it, adding and removing agents to match.
func TestSkillAgentChoice(t *testing.T) {
	home := skillHome(t, ".claude", `.gemini\config`, ".codex", ".kiro")
	claude := filepath.Join(home, ".claude", "skills", "line-cli")

	got := runSkill(t, "install", "--agent", "claude,codex")
	wantStatuses(t, "--agent claude,codex", got, map[string]string{sharedID: "installed", "claude-code": "installed", "antigravity": "not_selected", "kiro-cli": "not_selected"})
	if c := savedChoice(t); !slices.Equal(c, []string{sharedID, "claude-code"}) {
		t.Errorf("saved %v", c)
	}
	// Kiro was found, so it is offered; Windsurf was not, so it is not listed.
	if _, ok := got["windsurf"]; ok {
		t.Error("lists Windsurf, which is not installed")
	}

	// Without --agent, the saved choice wins over the agents found.
	wantStatuses(t, "install", runSkill(t, "install"), map[string]string{sharedID: "unchanged", "claude-code": "unchanged", "antigravity": "not_selected", "kiro-cli": "not_selected"})
	wantStatuses(t, "install -y", runSkill(t, "install", "-y"), map[string]string{sharedID: "unchanged", "claude-code": "unchanged", "antigravity": "not_selected", "kiro-cli": "not_selected"})

	got = runSkill(t, "install", "--agent", "antigravity, kiro-cli")
	wantStatuses(t, "switch agents", got, map[string]string{sharedID: "unchanged", "claude-code": "removed", "antigravity": "installed", "kiro-cli": "installed"})
	if _, err := os.Lstat(claude); !os.IsNotExist(err) {
		t.Error("the agent taken off the list kept the skill")
	}

	// none adds and removes nothing, and keeps what is there current.
	for _, d := range []string{`.agents\skills`, `.gemini\config\skills`} {
		if err := os.WriteFile(filepath.Join(home, d, "line-cli", "SKILL.md"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got = runSkill(t, "install", "--agent", "none")
	wantStatuses(t, "--agent none", got, map[string]string{sharedID: "updated", "claude-code": "not_selected", "antigravity": "updated", "kiro-cli": "unchanged"})
	if c := savedChoice(t); c == nil || len(c) != 0 {
		t.Errorf("--agent none saved %v", c)
	}

	// An agent named but not found gets the skill all the same.
	got = runSkill(t, "install", "--agent", "windsurf")
	if got["windsurf"].Status != "installed" || readSkill(t, got["windsurf"].Path) != linecli.Skill {
		t.Errorf("--agent windsurf: %+v", got["windsurf"])
	}

	for _, args := range [][]string{
		{"skill", "install", "--agent", "bogus"},
		{"skill", "install", "--agent", "none,claude-code"},
		{"skill", "install", "--agent", ""},
		{"skill", "uninstall", "--agent", "claude-code"},
		{"skill", "uninstall", "--yes"},
		{"skill", "--update"},
	} {
		code, out := runCLI(args...)
		var e errorOutput
		if code != 1 || json.Unmarshal(out, &e) != nil || e.Error != "INVALID_ARGUMENT" {
			t.Errorf("%v: rc=%d out=%s", args, code, out)
		}
	}
}

// A person at a console gets the checklist: the agents found (or the saved
// choice) ticked, the answer saved, and --update asks only the first time.
func TestSkillChecklist(t *testing.T) {
	home := skillHome(t, ".claude")
	canPrompt = func() bool { return true }
	var shown [][]pickItem
	answer := func(ok bool, toggle ...string) {
		runPicker = func(p *picker, _ lang) (bool, error) {
			shown = append(shown, slices.Clone(p.items))
			for i := range p.items {
				if slices.Contains(toggle, p.items[i].id) {
					p.items[i].on = !p.items[i].on
				}
			}
			if p.summary(p.selected()) == nil {
				t.Error("no summary")
			}
			return ok, nil
		}
	}
	ticked := func(items []pickItem) []string {
		var ids []string
		for _, it := range items {
			if it.on {
				ids = append(ids, it.id)
			}
		}
		return ids
	}

	// First run, skipped: nothing installed, and the empty answer saved so
	// the install script does not ask again.
	answer(false)
	got := runSkill(t, "install", "--update")
	wantStatuses(t, "skipped", got, map[string]string{sharedID: "not_installed", "claude-code": "not_selected", "antigravity": "agent_not_found"})
	if len(shown) != 1 || !slices.Equal(ticked(shown[0]), []string{sharedID, "claude-code"}) || !shown[0][0].locked {
		t.Fatalf("checklist: %+v", shown)
	}
	if c := savedChoice(t); c == nil || len(c) != 0 {
		t.Errorf("skipping saved %v", c)
	}
	runSkill(t, "install", "--update")
	if len(shown) != 1 {
		t.Error("--update asked again after the checklist was skipped")
	}

	// install without --update always asks; untick Claude Code, tick Antigravity.
	answer(true, "claude-code", "antigravity")
	got = runSkill(t, "install")
	wantStatuses(t, "chosen", got, map[string]string{sharedID: "installed", "claude-code": "not_selected", "antigravity": "installed"})
	if c := savedChoice(t); !slices.Equal(c, []string{sharedID, "antigravity"}) {
		t.Errorf("saved %v", c)
	}
	if _, err := os.Stat(filepath.Join(home, ".gemini", "config", "skills", "line-cli", "SKILL.md")); err != nil {
		t.Error("the chosen agent did not get the skill")
	}

	// The next checklist starts from the saved choice; skipping it keeps it.
	answer(false)
	runSkill(t, "install")
	if n := len(shown); !slices.Equal(ticked(shown[n-1]), []string{sharedID, "antigravity"}) {
		t.Errorf("second checklist ticked %v", ticked(shown[n-1]))
	}
	if c := savedChoice(t); !slices.Equal(c, []string{sharedID, "antigravity"}) {
		t.Errorf("skipping changed the saved choice to %v", c)
	}

	// Updates follow the saved choice without asking, and so does -y.
	n := len(shown)
	runSkill(t, "install", "--update")
	runSkill(t, "install", "-y")
	if len(shown) != n {
		t.Error("asked though a choice was saved")
	}

	// A console that cannot show the checklist falls back to the agents found.
	runPicker = func(*picker, lang) (bool, error) { return false, errors.New("no console") }
	rc, out, errText := runCLIErr("skill", "install")
	if rc != 0 || !strings.Contains(errText, "cannot show the agent checklist") {
		t.Errorf("no console: rc=%d out=%s err=%s", rc, out, errText)
	}
}

// mklink makes a junction the way people and npx skills do, with cmd's mklink:
// line-cli itself never makes one.
func mklink(t *testing.T, link, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J %s %s: %v %s", link, target, err, out)
	}
}

// Junctions from the release that made them, or from npx skills, become real
// copies; what they pointed to is left as it was.
func TestSkillReplacesLinks(t *testing.T) {
	home := skillHome(t, ".claude", `.gemini\config`)
	shared := filepath.Join(home, ".agents", "skills", "line-cli")
	claude := filepath.Join(home, ".claude", "skills", "line-cli")
	agy := filepath.Join(home, ".gemini", "config", "skills", "line-cli")
	if _, err := writeSkillFile(shared); err != nil {
		t.Fatal(err)
	}
	mklink(t, claude, shared)
	// A link to somewhere else, such as a clone of the repo.
	repo := filepath.Join(home, "src", "line-cli", "skills", "line-cli")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "SKILL.md"), []byte("---\nname: line-cli\n---\nfrom the repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mklink(t, agy, repo)

	got := runSkill(t, "install")
	wantStatuses(t, "install", got, map[string]string{sharedID: "unchanged", "claude-code": "updated", "antigravity": "updated"})
	for _, d := range []string{claude, agy} {
		if linkTarget(d) != "" || readSkill(t, filepath.Join(d, "SKILL.md")) != linecli.Skill {
			t.Errorf("%s is still a link, or lacks the skill", d)
		}
	}
	if readSkill(t, filepath.Join(shared, "SKILL.md")) != linecli.Skill || readSkill(t, filepath.Join(repo, "SKILL.md")) != "---\nname: line-cli\n---\nfrom the repo\n" {
		t.Error("replacing a link changed what it pointed to")
	}

	// uninstall removes a link, never what it points to.
	if err := os.RemoveAll(claude); err != nil {
		t.Fatal(err)
	}
	mklink(t, claude, repo)
	got = runSkill(t, "uninstall")
	if got["claude-code"].Status != "removed" {
		t.Errorf("uninstall a link: %+v", got["claude-code"])
	}
	if _, err := os.Lstat(claude); !os.IsNotExist(err) {
		t.Error("uninstall left the link")
	}
	if readSkill(t, filepath.Join(repo, "SKILL.md")) == "" {
		t.Error("uninstall removed what the link pointed to")
	}
}

// line-cli must never make a junction or link: antivirus software takes an
// unsigned program doing that in other programs' folders for an exploit.
func TestSkillMakesNoLinks(t *testing.T) {
	for _, f := range []string{"skill.go", "skillpick.go", "skillpick_windows.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"FSCTL_SET_REPARSE_POINT", "CreateSymbolicLink", "os.Symlink", "os.Link(", "mklink"} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s uses %s", f, bad)
			}
		}
	}
}

// A line-cli folder that is not line-cli's skill is left alone.
func TestSkillLeavesOtherSkills(t *testing.T) {
	home := skillHome(t, ".claude")
	other := filepath.Join(home, ".claude", "skills", "line-cli", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("---\nname: something-else\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runSkill(t, "install"); got["claude-code"].Status != "conflict" {
		t.Errorf("install: %+v", got["claude-code"])
	}
	if got := runSkill(t, "uninstall"); got["claude-code"].Status != "not_installed" {
		t.Errorf("uninstall: %+v", got["claude-code"])
	}
	if readSkill(t, other) != "---\nname: something-else\n---\n" {
		t.Error("changed a skill line-cli did not write")
	}
}

// A place that cannot be written or removed (antivirus software locks it, or no
// permission) is status failed with the reason, and the other places still go
// ahead, before it and after it.
func TestSkillKeepsGoingWhenOnePlaceFails(t *testing.T) {
	home := skillHome(t, ".claude", filepath.Join(".gemini", "config"))
	shared := filepath.Join(home, ".agents", "skills", "line-cli", "SKILL.md")
	claude := filepath.Join(home, ".claude", "skills", "line-cli", "SKILL.md")
	anti := filepath.Join(home, ".gemini", "config", "skills", "line-cli", "SKILL.md")
	wantStatuses(t, "first install", runSkill(t, "install"), map[string]string{sharedID: "installed", "claude-code": "installed", "antigravity": "installed"})

	// An older skill everywhere; the shared folder's and Claude Code's copies
	// cannot be written.
	const stale = "---\nname: line-cli\n---\nold\n"
	for _, p := range []string{shared, claude, anti} {
		if err := os.WriteFile(p, []byte(stale), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{shared, claude} {
		if err := os.Chmod(p, 0o444); err != nil {
			t.Fatal(err)
		}
	}
	got := runSkill(t, "install")
	wantStatuses(t, "locked install", got, map[string]string{sharedID: "failed", "claude-code": "failed", "antigravity": "updated"})
	for _, id := range []string{sharedID, "claude-code"} {
		if r := got[id]; !strings.Contains(r.Note, r.Path) {
			t.Errorf("%s: note %q does not say what failed", id, r.Note)
		}
	}
	if got["antigravity"].Note != "" {
		t.Errorf("antigravity: note %q on success", got["antigravity"].Note)
	}
	if readSkill(t, anti) != linecli.Skill || readSkill(t, claude) != stale {
		t.Error("the skill was not written where it could be, or was where it could not")
	}
	code, out := runCLI("skill", "install", "--text")
	if code != 0 || !regexp.MustCompile(`(?m)^claude-code\s+failed\s+.*SKILL\.md\s+\S`).Match(out) {
		t.Errorf("--text: rc=%d, no reason on the failed row:\n%s", code, out)
	}

	// Uninstall goes on past a copy another program holds open.
	for _, p := range []string{shared, claude} {
		if err := os.Chmod(p, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runSkill(t, "install")
	f, err := os.Open(anti) // shares reading and writing, not deleting
	if err != nil {
		t.Fatal(err)
	}
	got = runSkill(t, "uninstall")
	f.Close()
	wantStatuses(t, "held uninstall", got, map[string]string{sharedID: "removed", "claude-code": "removed", "antigravity": "failed"})
	if got["antigravity"].Note == "" {
		t.Error("antigravity: no reason for failed")
	}
	if c := savedChoice(t); c == nil || len(c) != 0 {
		t.Errorf("uninstall saved %v, want none", c)
	}
	wantStatuses(t, "uninstall again", runSkill(t, "uninstall"), map[string]string{sharedID: "not_installed", "claude-code": "not_installed", "antigravity": "removed"})
}

// A file where an agent's skills folder should be: that agent fails, the
// shared folder is still written.
func TestSkillFailsWhereFolderCannotBeMade(t *testing.T) {
	home := skillHome(t, ".claude")
	if err := os.WriteFile(filepath.Join(home, ".claude", "skills"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got := runSkill(t, "install")
	if got[sharedID].Status != "installed" || got["claude-code"].Status != "failed" || got["claude-code"].Note == "" {
		t.Errorf("install: %+v", got)
	}
}

// The skill was named linecli before: install and uninstall remove that copy,
// but not someone else's skill in a folder of that name.
func TestSkillRemovesLegacyLinecli(t *testing.T) {
	home := skillHome(t, ".claude")
	legacyDir := filepath.Join(home, ".claude", "skills", "linecli")
	legacy := filepath.Join(legacyDir, "SKILL.md")
	writeLegacy := func(content string) {
		t.Helper()
		if err := os.MkdirAll(legacyDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(legacy, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writeLegacy("---\r\nname: linecli\r\ndescription: old\r\n---\r\n\r\n# linecli\r\n")
	if got := runSkill(t, "install"); got["claude-code"].Status != "installed" {
		t.Fatalf("install: %+v", got["claude-code"])
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Error("install left the old linecli skill")
	}

	writeLegacy("---\nname: linecli\ndescription: old\n---\n")
	runSkill(t, "uninstall")
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Error("uninstall left the old linecli skill")
	}

	writeLegacy("---\nname: someone-elses-skill\n---\n")
	runSkill(t, "install")
	runSkill(t, "uninstall")
	if _, err := os.Stat(legacy); err != nil {
		t.Error("removed a skill line-cli did not write")
	}
}

func TestSkillNameOf(t *testing.T) {
	for in, want := range map[string]string{
		"---\nname: line-cli\ndescription: x\n---\nbody":      "line-cli",
		"---\r\ndescription: x\r\nname: 'linecli'\r\n---\r\n": "linecli",
		"name: line-cli\n":                          "",
		"---\nname: line-cli\n":                     "",
		"---\ndescription: no name\n---\nname: x\n": "",
	} {
		if got := skillNameOf([]byte(in)); got != want {
			t.Errorf("skillNameOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSkillPicker(t *testing.T) {
	items := []pickItem{
		{id: sharedID, label: "Shared folder", path: `~\.agents\skills`, note: "Read by Codex", locked: true, on: true},
		{id: "claude-code", label: "Claude Code", path: `~\.claude\skills`, on: true, found: true},
		{id: "antigravity", label: "Antigravity", path: `~\.gemini\config\skills`},
	}
	p := newPicker(slices.Clone(items), func(sel []string) []string { return []string{strings.Join(sel, "+")} })
	if p.cur != 1 {
		t.Fatalf("the cursor starts on %d, want the first unlocked item", p.cur)
	}
	press := func(s string) (done, ok bool) {
		for _, k := range parseKeys(s) {
			if done, ok = p.handle(k); done {
				return
			}
		}
		return
	}
	press("\x1b[B ") // down to Antigravity, tick it
	if got := p.selected(); !slices.Equal(got, []string{"claude-code", "antigravity"}) {
		t.Errorf("selected %v", got)
	}
	press("\x1b[B") // wraps past the locked shared folder to Claude Code
	if p.cur != 1 {
		t.Errorf("cursor at %d after wrapping", p.cur)
	}
	press("a") // all were on: untick all
	if len(p.selected()) != 0 {
		t.Errorf("a with all ticked left %v", p.selected())
	}
	press("a")
	lines := p.render(langEN, 0)
	if !slices.Contains(lines, "> [x] Claude Code    ~\\.claude\\skills         detected") ||
		!slices.Contains(lines, "  [*] Shared folder  ~\\.agents\\skills         always") ||
		!slices.Contains(lines, "      Read by Codex") {
		t.Errorf("render:\n%s", strings.Join(lines, "\n"))
	}
	for _, l := range p.render(langZhTW, 30) {
		if displayWidth(l) > 29 {
			t.Errorf("line wider than the console: %q", l)
		}
	}

	// Enter shows the summary; Esc goes back to the list; Enter twice installs.
	if done, _ := press("\r"); done || p.stage != stageConfirm || !slices.Contains(p.render(langEN, 0), "claude-code+antigravity") {
		t.Fatalf("Enter: stage %d, render %q", p.stage, p.render(langEN, 0))
	}
	press("\x1b")
	if p.stage != stageList {
		t.Errorf("Esc in the summary: stage %d", p.stage)
	}
	if done, ok := press("\r\r"); !done || !ok {
		t.Errorf("Enter, Enter: done=%v ok=%v", done, ok)
	}

	for _, keys := range []string{"\x1b", "q", "\x03", "\ry\x1b"} {
		p := newPicker(slices.Clone(items), func([]string) []string { return nil })
		done, ok := false, false
		for _, k := range parseKeys(keys) {
			if done, ok = p.handle(k); done {
				break
			}
		}
		if want := keys == "\ry\x1b"; !done || ok != want {
			t.Errorf("%q: done=%v ok=%v", keys, done, ok)
		}
	}
	if got := parseKeys("\x1b[H\x1b[1;5Cjk x"); !slices.Equal(got, []pickKey{keyDown, keyUp, keyToggle, keyToggle}) {
		t.Errorf("parseKeys skipped other keys wrong: %v", got)
	}
}

func TestSkillPickItemsAndSummary(t *testing.T) {
	home := skillHome(t, ".claude", ".codex", ".roo")
	s := newSkillSetup(os.Getenv, home)
	var ids []string
	for _, it := range s.pickItems(s.detected(), langZhTW) {
		ids = append(ids, it.id)
		if it.id == sharedID && (!strings.Contains(it.note, "Codex") || !strings.Contains(it.note, "已偵測到：Codex")) {
			t.Errorf("shared note %q", it.note)
		}
	}
	if !slices.Equal(ids, []string{sharedID, "claude-code", "antigravity", "roo"}) {
		t.Errorf("items %v", ids)
	}
	for _, r := range s.install([]string{sharedID, "roo"}) {
		if r.Status == "failed" {
			t.Fatalf("install: %+v", r)
		}
	}
	sum := strings.Join(s.summary([]string{sharedID, "claude-code"}, langEN), "\n")
	for _, want := range []string{`~\.agents\skills\line-cli`, "write a copy (Claude Code)", "remove (Roo Code, not selected)"} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary lacks %q:\n%s", want, sum)
		}
	}
}

func TestSkillPrintAndArgs(t *testing.T) {
	code, out := runCLI("skill")
	if code != 0 || string(out) != linecli.Skill {
		t.Errorf("skill: rc=%d, printed %d bytes, the skill has %d", code, len(out), len(linecli.Skill))
	}
	for _, args := range [][]string{{"skill", "bogus"}, {"skill", "install", "extra"}} {
		code, out := runCLI(args...)
		var e errorOutput
		if code != 1 || json.Unmarshal(out, &e) != nil || e.Error != "INVALID_ARGUMENT" {
			t.Errorf("%v: rc=%d out=%s", args, code, out)
		}
	}
}

// The skill is what agents read first: its frontmatter must parse, and every
// command and flag it shows must exist.
func TestSkillContent(t *testing.T) {
	s := linecli.Skill
	front, body, ok := strings.Cut(strings.TrimPrefix(s, "---\n"), "\n---\n")
	if !strings.HasPrefix(s, "---\n") || !ok {
		t.Fatal("SKILL.md must start with a --- frontmatter block")
	}
	fields := map[string]string{}
	for _, l := range strings.Split(front, "\n") {
		k, v, ok := strings.Cut(l, ": ")
		if !ok {
			t.Fatalf("frontmatter line %q is not key: value", l)
		}
		fields[k] = v
	}
	if fields["name"] != "line-cli" || skillNameOf([]byte(s)) != skillFolderName {
		t.Errorf("name %q", fields["name"])
	}
	// A plain YAML value: no ": ", " #" or leading quote, and short enough for every agent.
	if d := fields["description"]; d == "" || len(d) > 1024 || strings.Contains(d, ": ") || strings.Contains(d, " #") || strings.ContainsAny(d[:1], `"'`) {
		t.Errorf("description %q", d)
	}
	if strings.ContainsFunc(s, func(r rune) bool { return r > 0x7e && r != '\n' }) {
		t.Error("SKILL.md must be English (ASCII)")
	}
	regs := registeredFlags(t)
	// Commands start a line (in the code blocks) or follow a backtick; "line"
	// elsewhere is an English word.
	cmdRe := regexp.MustCompile("(?m)(?:^|`)line ([a-z][a-z-]*)((?: [^`\n#]*)?)")
	matches := cmdRe.FindAllStringSubmatch(body, -1)
	if len(matches) < 10 {
		t.Fatalf("found only %d commands in SKILL.md", len(matches))
	}
	for _, m := range matches {
		fs, ok := regs[m[1]]
		if !ok {
			t.Errorf("SKILL.md shows 'line %s', which is not a command", m[1])
			continue
		}
		for _, f := range regexp.MustCompile(`(?:^|\s)--?([a-z][a-z-]*)`).FindAllStringSubmatch(m[2], -1) {
			if fs.Lookup(f[1]) == nil && !map[string]bool{"text": true, "compact": true, "jsonl": true, "ids": true, "out": true, "json": true}[f[1]] {
				t.Errorf("SKILL.md shows 'line %s ... --%s', which %s does not define", m[1], f[1], m[1])
			}
		}
	}
}
