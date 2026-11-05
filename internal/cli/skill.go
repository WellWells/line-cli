package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	linecli "github.com/WellWells/line-cli"
	"github.com/WellWells/line-cli/internal/line"
)

// The line-cli skill goes where the skills CLI (npx skills add -g --copy) puts
// a skill: in the shared folder ~\.agents\skills, which Codex, Cursor, GitHub
// Copilot, Gemini CLI, OpenCode and other agents read, and a copy in the folder
// of each chosen agent that reads only its own. Copies, not junctions: an
// unsigned program making junctions in other programs' folders looks like an
// exploit to antivirus software. Every
// install rewrites the skill wherever it is, so no copy goes stale. A junction
// or link left by an earlier install, or by npx skills, becomes a copy.

const (
	skillFolderName = "line-cli"
	sharedID        = "universal" // the shared folder, in --agent and the output
)

// skillAgent is an AI agent that reads skills from a folder of its own.
type skillAgent struct {
	id, name string
	homes    []string // the agent counts as installed when one of these exists
	dir      string   // <its skills folder>\line-cli
	listed   bool     // in the checklist even when not found
	found    bool
}

// sharedReader is an agent that reads the shared folder.
type sharedReader struct{ id, name, home string }

// skillSetup is where the skill can go on this PC.
type skillSetup struct {
	home    string
	shared  string // ~\.agents\skills\line-cli
	readers []sharedReader
	agents  []skillAgent
}

// newSkillSetup lists the skill folders after the agent table of the skills
// CLI: Claude Code $CLAUDE_CONFIG_DIR\skills (default ~\.claude\skills) and
// Antigravity ~\.gemini\config\skills are always offered, the other agents
// that keep their own folder only when found.
func newSkillSetup(getenv func(string) string, home string) *skillSetup {
	in := func(p ...string) string { return filepath.Join(append([]string{home}, p...)...) }
	claude := getenv("CLAUDE_CONFIG_DIR")
	if claude == "" {
		claude = in(".claude")
	}
	codex := getenv("CODEX_HOME")
	if codex == "" {
		codex = in(".codex")
	}
	s := &skillSetup{
		home:   home,
		shared: in(".agents", "skills", skillFolderName),
		readers: []sharedReader{
			{"codex", "Codex", codex},
			{"cursor", "Cursor", in(".cursor")},
			{"github-copilot", "GitHub Copilot", in(".copilot")},
			{"gemini-cli", "Gemini CLI", in(".gemini", "settings.json")},
			{"opencode", "OpenCode", in(".config", "opencode")},
			{"amp", "Amp", in(".config", "amp")},
			{"cline", "Cline", in(".cline")},
		},
	}
	agent := func(id, name string, listed bool, skills string, homes ...string) {
		a := skillAgent{id: id, name: name, homes: homes, dir: filepath.Join(skills, skillFolderName), listed: listed}
		for _, h := range homes {
			if _, err := os.Stat(h); err == nil {
				a.found = true
			}
		}
		s.agents = append(s.agents, a)
	}
	agent("claude-code", "Claude Code", true, filepath.Join(claude, "skills"), claude)
	agent("antigravity", "Antigravity", true, in(".gemini", "config", "skills"), in(".gemini", "config"), in(".gemini", "antigravity"))
	agent("windsurf", "Windsurf", false, in(".codeium", "windsurf", "skills"), in(".codeium", "windsurf"))
	agent("kiro-cli", "Kiro", false, in(".kiro", "skills"), in(".kiro"))
	agent("qwen-code", "Qwen Code", false, in(".qwen", "skills"), in(".qwen"))
	agent("roo", "Roo Code", false, in(".roo", "skills"), in(".roo"))
	agent("trae", "Trae", false, in(".trae", "skills"), in(".trae"))
	agent("continue", "Continue", false, in(".continue", "skills"), in(".continue"))
	agent("augment", "Augment", false, in(".augment", "skills"), in(".augment"))
	agent("junie", "Junie", false, in(".junie", "skills"), in(".junie"))
	agent("openhands", "OpenHands", false, in(".openhands", "skills"), in(".openhands"))
	return s
}

func (s *skillSetup) agent(id string) *skillAgent {
	for i := range s.agents {
		if s.agents[i].id == id {
			return &s.agents[i]
		}
	}
	return nil
}

// sharedName names the agents that read the shared folder, for people.
func (s *skillSetup) sharedName() string {
	var names []string
	for _, r := range s.readers[:5] {
		names = append(names, r.name)
	}
	return strings.Join(names, ", ") + " and other agents"
}

// readersFound names the agents found that read the shared folder.
func (s *skillSetup) readersFound() []string {
	var names []string
	for _, r := range s.readers {
		if _, err := os.Stat(r.home); err == nil {
			names = append(names, r.name)
		}
	}
	return names
}

// detected is the choice when nobody chooses: the shared folder and every
// agent found.
func (s *skillSetup) detected() []string {
	ids := []string{sharedID}
	for _, a := range s.agents {
		if a.found {
			ids = append(ids, a.id)
		}
	}
	return ids
}

// parseAgents reads an --agent list: agent ids separated by commas, or none.
// Any choice but none includes the shared folder.
func (s *skillSetup) parseAgents(list string) ([]string, error) {
	fields := strings.FieldsFunc(strings.ToLower(list), func(r rune) bool { return r == ',' || r == ';' || r == ' ' })
	if len(fields) == 0 {
		return nil, invalid("--agent needs agents separated by commas, or none")
	}
	if len(fields) == 1 && fields[0] == "none" {
		return []string{}, nil
	}
	ids := []string{sharedID}
	for _, f := range fields {
		if f == "claude" {
			f = "claude-code"
		}
		switch {
		case f == "none":
			return nil, invalid("--agent none cannot go with agents")
		case f == sharedID || f == "agents" || f == "copilot" || slices.ContainsFunc(s.readers, func(r sharedReader) bool { return r.id == f }):
			// they read the shared folder, which ids already has
		case s.agent(f) != nil:
			if !slices.Contains(ids, f) {
				ids = append(ids, f)
			}
		default:
			return nil, invalid(fmt.Sprintf("unknown agent %q in --agent; agents: %s, or none", f, strings.Join(s.agentIDs(), ", ")))
		}
	}
	return ids, nil
}

func (s *skillSetup) agentIDs() []string {
	ids := []string{sharedID}
	for _, r := range s.readers {
		ids = append(ids, r.id)
	}
	for _, a := range s.agents {
		ids = append(ids, a.id)
	}
	return ids
}

// skillChoice is the saved answer to "which agents get the skill", so updates
// install it the same way without asking again.
type skillChoice struct {
	// Agents is the shared folder and the chosen agents; empty when the person
	// chose none, or skipped the checklist.
	Agents []string `json:"agents"`
}

func skillChoicePath() string { return filepath.Join(line.StoreDir(), "skill.json") }

// loadChoice reads the saved choice, keeping only the ids this version knows.
func (s *skillSetup) loadChoice() ([]string, bool) {
	b, err := os.ReadFile(skillChoicePath())
	if err != nil {
		return nil, false
	}
	var c skillChoice
	if json.Unmarshal(b, &c) != nil || c.Agents == nil {
		return nil, false
	}
	ids := []string{}
	for _, id := range c.Agents {
		if s.agent(id) != nil && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(c.Agents) > 0 {
		ids = append([]string{sharedID}, ids...)
	}
	return ids, true
}

func saveChoice(ids []string) error {
	b, err := json.Marshal(skillChoice{Agents: append([]string{}, ids...)})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(skillChoicePath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(skillChoicePath(), append(b, '\n'), 0o644)
}

type skillResult struct {
	Agent  string `json:"agent"`
	Name   string `json:"name"`
	Path   string `json:"path"`           // the SKILL.md the agent reads
	Status string `json:"status"`         // see the skill command's help
	Note   string `json:"note,omitempty"` // why, for status failed
}

// set records how writing or removing the skill went. A place that fails is
// status failed, with why in note, and install and uninstall go on with the
// other places: one folder that antivirus software locks, or that this user
// cannot write, must not keep every other agent's copy stale.
func (r *skillResult) set(status string, err error) {
	if err != nil {
		r.Status, r.Note = "failed", err.Error()
		return
	}
	r.Status = status
}

func (rc *runCtx) skill(args []string) int {
	fs := newFlags("skill")
	agentList := fs.String("agent", "", "")
	yes := fs.Bool("yes", false, "")
	y := fs.Bool("y", false, "")
	update := fs.Bool("update", false, "")
	if err := parseArgs(fs, args, "yes", "y", "update"); err != nil {
		return rc.fail(err)
	}
	if fs.NArg() > 1 {
		return rc.fail(invalid("skill takes install, uninstall or nothing"))
	}
	action := positional(fs)
	if action != "" && action != "install" && action != "uninstall" {
		en, zh := didYouMean(action, []string{"install", "uninstall"}, quoted("line skill "))
		return rc.fail(typo("skill takes install, uninstall or nothing, not "+action+en, "skill 沒有 "+action+" 這個動作", zh))
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	for _, name := range []string{"agent", "yes", "y", "update"} {
		if set[name] && action != "install" {
			return rc.fail(invalid("--" + name + " goes with skill install"))
		}
	}
	if action == "" {
		fmt.Fprint(rc.out, linecli.Skill)
		return 0
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return rc.fail(err)
	}
	s := newSkillSetup(os.Getenv, home)
	var results []skillResult
	if action == "uninstall" {
		results = s.uninstall()
		err = saveChoice(nil) // updates leave it uninstalled
	} else {
		var ids []string
		var remember bool
		if ids, remember, err = rc.chooseAgents(s, *agentList, set["agent"], *yes || *y, *update); err == nil {
			results = s.install(ids)
			if remember {
				err = saveChoice(ids)
			}
		}
	}
	if err != nil {
		return rc.fail(err)
	}
	if !rc.text {
		return rc.emit(results)
	}
	for _, r := range results {
		fields := []string{r.Agent, r.Status, r.Path}
		if r.Note != "" {
			fields = append(fields, r.Note)
		}
		fmt.Fprintln(rc.out, cols(fields...))
	}
	return 0
}

// chooseAgents decides where install puts the skill, and whether to save that
// as the person's choice. An empty list means no agent is added or removed.
//
//   - --agent: that list, saved.
//   - A saved choice, with --update or --yes or when nobody can answer: that.
//   - --yes, or nobody can answer: the shared folder and the agents found.
//   - Otherwise the checklist, with the saved choice or the agents found
//     ticked; skipping it saves an empty choice when none was saved, so the
//     install script stops asking.
func (rc *runCtx) chooseAgents(s *skillSetup, list string, listSet, yes, update bool) ([]string, bool, error) {
	if listSet {
		ids, err := s.parseAgents(list)
		return ids, true, err
	}
	saved, hasSaved := s.loadChoice()
	ask := !yes && canPrompt()
	if hasSaved && (update || !ask) {
		return saved, false, nil
	}
	if !ask {
		return s.detected(), false, nil
	}
	ticked := s.detected()
	if len(saved) > 0 {
		ticked = saved
	}
	l := userLang()
	p := newPicker(s.pickItems(ticked, l), func(sel []string) []string { return s.summary(append([]string{sharedID}, sel...), l) })
	ok, err := runPicker(p, l)
	if err != nil {
		fmt.Fprintf(rc.err, "line: cannot show the agent checklist (%v); installing for the agents found\n", err)
		return s.detected(), false, nil
	}
	if !ok {
		return []string{}, !hasSaved, nil
	}
	return append([]string{sharedID}, p.selected()...), true, nil
}

var skillText = struct {
	sharedLabel, sharedNote, found, write, copy, remove [2]string
}{
	sharedLabel: [2]string{"Shared folder", "共用資料夾"},
	sharedNote:  [2]string{"Codex, Cursor, GitHub Copilot, Gemini CLI, OpenCode and more read it", "Codex、Cursor、GitHub Copilot、Gemini CLI、OpenCode 等 agent 都讀這裡"},
	found:       [2]string{"(detected: %s)", "（已偵測到：%s）"},
	write:       [2]string{"write the skill (Codex and more read it)", "寫入 skill（Codex 等讀這裡）"},
	copy:        [2]string{"write a copy (%s)", "寫入一份複本（%s）"},
	remove:      [2]string{"remove (%s, not selected)", "移除（%s 未勾選）"},
}

// pickItems is the checklist: the shared folder, always in, then the agents
// always offered, those found and those ticked.
func (s *skillSetup) pickItems(ticked []string, l lang) []pickItem {
	note := l.pick(skillText.sharedNote)
	if found := s.readersFound(); len(found) > 0 {
		sep := ", "
		if l == langZhTW {
			sep = "、"
		}
		note += "\n" + fmt.Sprintf(l.pick(skillText.found), strings.Join(found, sep))
	}
	items := []pickItem{{id: sharedID, label: l.pick(skillText.sharedLabel), path: s.tilde(filepath.Dir(s.shared)), note: note, locked: true, on: true}}
	for _, a := range s.agents {
		on := slices.Contains(ticked, a.id)
		if a.listed || a.found || on {
			items = append(items, pickItem{id: a.id, label: a.name, path: s.tilde(filepath.Dir(a.dir)), on: on, found: a.found})
		}
	}
	return items
}

// summary says what install(ids) will do, for the checklist's last step.
func (s *skillSetup) summary(ids []string, l lang) []string {
	rows := [][2]string{{s.tilde(s.shared), l.pick(skillText.write)}}
	for _, a := range s.agents {
		switch {
		case slices.Contains(ids, a.id):
			rows = append(rows, [2]string{s.tilde(a.dir), fmt.Sprintf(l.pick(skillText.copy), a.name)})
		case isInstalled(inspectSkill(a.dir)):
			rows = append(rows, [2]string{s.tilde(a.dir), fmt.Sprintf(l.pick(skillText.remove), a.name)})
		}
	}
	return table(rows)
}

// tilde shows a path under the home folder as ~\...
func (s *skillSetup) tilde(p string) string {
	if rel, err := filepath.Rel(s.home, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join("~", rel)
	}
	return p
}

// install puts the skill in the shared folder and the agents in ids, and
// removes it from the other agents. With ids empty it adds and removes
// nothing, but still rewrites the skill wherever it is, so no copy goes stale.
// A place that fails is status failed and does not stop the others.
func (s *skillSetup) install(ids []string) []skillResult {
	s.removeLegacy()
	chosen := map[string]bool{}
	for _, id := range ids {
		chosen[id] = true
	}
	removeOthers := len(ids) > 0
	if !removeOthers { // keep the skill current where it is
		chosen[sharedID] = isInstalled(inspectSkill(s.shared))
		for _, a := range s.agents {
			chosen[a.id] = isInstalled(inspectSkill(a.dir))
		}
	}
	shared := skillResult{Agent: sharedID, Name: s.sharedName(), Path: filepath.Join(s.shared, "SKILL.md"), Status: "not_installed"}
	if chosen[sharedID] {
		shared.set(installShared(s.shared))
	}
	results := []skillResult{shared}
	for _, a := range s.agents {
		r := skillResult{Agent: a.id, Name: a.name, Path: filepath.Join(a.dir, "SKILL.md")}
		present := isInstalled(inspectSkill(a.dir))
		switch {
		case chosen[a.id]:
			r.set(copySkill(a.dir))
		case removeOthers && present:
			r.set(removeSkill(a.dir))
		case !a.found:
			r.Status = "agent_not_found"
		default:
			r.Status = "not_selected"
		}
		if a.listed || a.found || chosen[a.id] || present {
			results = append(results, r)
		}
	}
	return results
}

// uninstall removes the skill from every agent and the shared folder. A place
// that fails is status failed and does not stop the others.
func (s *skillSetup) uninstall() []skillResult {
	s.removeLegacy()
	results := []skillResult{{Agent: sharedID, Name: s.sharedName(), Path: filepath.Join(s.shared, "SKILL.md")}}
	for _, a := range s.agents {
		r := skillResult{Agent: a.id, Name: a.name, Path: filepath.Join(a.dir, "SKILL.md")}
		r.set(removeSkill(a.dir))
		if a.listed || a.found || r.Status != "not_installed" {
			results = append(results, r)
		}
	}
	results[0].set(removeSkill(s.shared)) // after any old links to it
	return results
}

// skillFolder is what a <skills>\line-cli folder holds.
type skillFolder int

const (
	folderNone     skillFolder = iota // nothing, or an empty folder
	folderLink                        // a junction or symbolic link
	folderOurs                        // line-cli's SKILL.md and nothing else
	folderOursPlus                    // line-cli's SKILL.md and files someone added
	folderOther                       // not line-cli's skill: left alone
)

// inspectSkill reports what dir holds, and where it links to if a link.
func inspectSkill(dir string) (skillFolder, string) {
	fi, err := os.Lstat(dir)
	if err != nil {
		return folderNone, ""
	}
	if t := linkTarget(dir); t != "" {
		return folderLink, t
	}
	if !fi.IsDir() {
		return folderOther, ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return folderOther, ""
	}
	if len(entries) == 0 {
		return folderNone, ""
	}
	// A SKILL.md naming no skill (a damaged copy) in a folder named line-cli is
	// line-cli's; one naming another skill is not.
	b, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if name := skillNameOf(b); err != nil || name != "" && name != skillFolderName {
		return folderOther, ""
	}
	if len(entries) == 1 {
		return folderOurs, ""
	}
	return folderOursPlus, ""
}

// linkTarget returns where path links to when it is a junction or symbolic
// link, else "" (also when it does not exist).
func linkTarget(path string) string {
	t, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(t, `\??\`)
}

func isInstalled(f skillFolder, _ string) bool {
	return f == folderLink || f == folderOurs || f == folderOursPlus
}

// skillNameOf returns the name in a SKILL.md's frontmatter.
func skillNameOf(b []byte) string {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	front, _, ok := strings.Cut(strings.TrimPrefix(s, "---\n"), "\n---")
	if !strings.HasPrefix(s, "---\n") || !ok {
		return ""
	}
	for _, l := range strings.Split(front, "\n") {
		if v, ok := strings.CutPrefix(l, "name:"); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

// writeSkillFile writes the skill into dir: installed, updated or unchanged.
func writeSkillFile(dir string) (string, error) {
	path := filepath.Join(dir, "SKILL.md")
	old, err := os.ReadFile(path)
	if err == nil && string(old) == linecli.Skill {
		return "unchanged", nil
	}
	status := "updated"
	if errors.Is(err, os.ErrNotExist) {
		status = "installed"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(linecli.Skill), 0o644); err != nil {
		return "", err
	}
	return status, nil
}

// installShared writes the skill in the shared folder.
func installShared(dir string) (string, error) { return copySkill(dir) }

// copySkill writes the skill into dir as a copy. A junction or link there is
// removed first (never what it points to) and becomes a real folder.
func copySkill(dir string) (string, error) {
	switch f, _ := inspectSkill(dir); f {
	case folderOther:
		return "conflict", nil
	case folderLink:
		if err := os.Remove(dir); err != nil {
			return "", err
		}
		_, err := writeSkillFile(dir)
		return "updated", err
	}
	return writeSkillFile(dir)
}

// removeSkill removes line-cli's skill from dir: a link (never what it points
// to), or SKILL.md and then the folder, unless someone added files there.
func removeSkill(dir string) (string, error) {
	switch f, _ := inspectSkill(dir); f {
	case folderLink:
		if err := os.Remove(dir); err != nil {
			return "", err
		}
	case folderOurs, folderOursPlus:
		if err := os.Remove(filepath.Join(dir, "SKILL.md")); err != nil {
			return "", err
		}
		os.Remove(dir) // fails, as it should, if other files are there
	default:
		return "not_installed", nil
	}
	return "removed", nil
}

// legacySkill is the skill's name before line-cli. install and uninstall remove
// that copy, so an agent never has two skills for LINE, one of them telling it
// to run a linecli command that is no longer on PATH.
const legacySkill = "linecli"

// removeLegacy deletes skills\linecli\SKILL.md beside every place the skill
// goes when it is line-cli's old skill (its frontmatter names it linecli), and
// then its folder when nothing else is in it.
func (s *skillSetup) removeLegacy() {
	dirs := []string{s.shared}
	for _, a := range s.agents {
		dirs = append(dirs, a.dir)
	}
	for _, d := range dirs {
		old := filepath.Join(filepath.Dir(d), legacySkill)
		if linkTarget(old) != "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(old, "SKILL.md"))
		if err != nil || skillNameOf(b) != legacySkill {
			continue
		}
		if os.Remove(filepath.Join(old, "SKILL.md")) == nil {
			os.Remove(old) // fails, as it should, if other files are there
		}
	}
}
