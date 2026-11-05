package cli

import (
	"flag"
	"slices"
	"strings"
	"unicode/utf8"
)

// suggest returns the candidates closest to what was typed, for a "did you
// mean" hint. It ignores letter case and allows one edit (a letter missing,
// extra or wrong, or two letters swapped) for up to 4 letters and two for
// more, keeping only the closest; one or two letters must match exactly. When
// nothing is that close, it returns the candidates that start with what was
// typed (at least 3 letters). At most 3; nil when nothing fits.
func suggest(typed string, candidates []string) []string {
	t := strings.ToLower(typed)
	n := utf8.RuneCountInString(t)
	if n == 0 {
		return nil
	}
	maxDist := 2
	switch {
	case n <= 2:
		maxDist = 0
	case n <= 4:
		maxDist = 1
	}
	best, out := maxDist, []string(nil)
	for _, c := range candidates {
		switch d := editDistance(t, strings.ToLower(c)); {
		case d > best:
		case d < best || out == nil:
			best, out = d, []string{c}
		case d == best && !slices.Contains(out, c):
			out = append(out, c)
		}
	}
	if out == nil && n >= 3 {
		for _, c := range candidates {
			if strings.HasPrefix(strings.ToLower(c), t) && !slices.Contains(out, c) {
				out = append(out, c)
			}
		}
	}
	return out[:min(len(out), 3)]
}

// editDistance counts the edits (insert, delete or change a letter, or swap
// two adjacent letters) that turn a into b: the optimal string alignment
// distance, over runes.
func editDistance(a, b string) int {
	s, t := []rune(a), []rune(b)
	d := make([][]int, len(s)+1)
	for i := range d {
		d[i] = make([]int, len(t)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(s); i++ {
		for j := 1; j <= len(t); j++ {
			cost := 1
			if s[i-1] == t[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && s[i-1] == t[j-2] && s[i-2] == t[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(s)][len(t)]
}

// didYouMean is the hint for what was typed: "; did you mean X?" for the
// error message and "你是不是要打 X？" for a person, each suggestion written
// by show; both "" when no candidate is close.
func didYouMean(typed string, candidates []string, show func(string) string) (en, zh string) {
	s := suggest(typed, candidates)
	if len(s) == 0 {
		return "", ""
	}
	for i, c := range s {
		s[i] = show(c)
	}
	return "; did you mean " + orList(s, ", ", " or ") + "?",
		"你是不是要打 " + strings.ReplaceAll(orList(s, "、", " 或 "), "'", "") + "？"
}

// orList joins "a", "a or b", "a, b or c" with the separators given.
func orList(s []string, sep, or string) string {
	if len(s) < 2 {
		return strings.Join(s, "")
	}
	return strings.Join(s[:len(s)-1], sep) + or + s[len(s)-1]
}

// typoError is a mistyped name: the INVALID_ARGUMENT error, and zh, which fail
// prints on stderr for a person who reads Chinese at a console. The JSON error
// stays English, for agents.
type typoError struct {
	error
	zh string
}

// typo is the error for a mistyped name: msg for the JSON error and, when
// there is a hint, what + hint in Chinese.
func typo(msg, what, hint string) error {
	if hint == "" {
		return invalid(msg)
	}
	return &typoError{invalid(msg), what + "，" + hint}
}

// commandNames lists the commands, sorted.
func commandNames() []string {
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// quoted shows a suggestion as a whole command line: 'line read'.
func quoted(prefix string) func(string) string {
	return func(s string) string { return "'" + prefix + s + "'" }
}

// flagName writes a flag as people type it: -y, --limit.
func flagName(name string) string {
	if utf8.RuneCountInString(name) == 1 {
		return "-" + name
	}
	return "--" + name
}

func flagNames(fs *flag.FlagSet, extra ...string) []string {
	var names []string
	fs.VisitAll(func(f *flag.Flag) { names = append(names, f.Name) })
	return append(names, extra...)
}

// flagError is parseArgs' error for what the flag package refused; for a
// flag the command does not have, with what was probably meant: a flag of the
// command (or a global one) spelled alike.
func flagError(fs *flag.FlagSet, err error) error {
	name, ok := strings.CutPrefix(err.Error(), "flag provided but not defined: -")
	if !ok {
		return invalid(err.Error())
	}
	en, zh := didYouMean(name, flagNames(fs, "help"), flagName)
	if name == "json" {
		ex := "line help " + fs.Name() + " --json"
		en = "; output is JSON by default (--json goes only with help, as in '" + ex + "')"
		zh = "輸出本來就是 JSON；--json 只用在 help，例如 " + ex + "。"
	}
	return typo(err.Error()+en, fs.Name()+" 沒有 "+flagName(name)+" 這個旗標", zh)
}

// unknownLeadingFlag is the error for a flag before the command that is not a
// global flag, such as a command's own flag given too early.
func unknownLeadingFlag(a string) error {
	fs := newFlags("line")
	globalFlags(fs)
	globals := flagNames(fs)
	name, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "=")
	en, zh := didYouMean(name, append(slices.Clone(globals), "help", "version"), flagName)
	if en == "" {
		shown := make([]string, len(globals))
		for i, g := range globals {
			shown[i] = flagName(g)
		}
		en = "; only " + strings.Join(shown[:len(shown)-1], ", ") + " and " + shown[len(shown)-1] + " go before the command"
	}
	return typo("unknown flag: "+a+en+" (see 'line help')", "沒有 "+a+" 這個旗標", zh)
}
