// Package linecli holds the files built into the line-cli program.
package linecli

import _ "embed"

// Skill is the agent skill that teaches Claude Code, Codex and Antigravity to
// use line-cli. 'line skill install' writes it into their skill folders, so
// the skill always matches the installed program.
//
//go:embed skills/line-cli/SKILL.md
var Skill string
