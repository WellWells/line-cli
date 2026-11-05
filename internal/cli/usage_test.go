package cli

import (
	"encoding/json"
	"flag"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// These run without LINE: language detection and the help people read.

func TestDetectLang(t *testing.T) {
	for _, c := range []struct {
		env    map[string]string
		system []string
		want   lang
	}{
		{nil, nil, langEN},
		{nil, []string{"zh-TW", "en-US"}, langZhTW},
		{nil, []string{"zh-Hant-TW"}, langZhTW},
		{nil, []string{"zh-CN"}, langZhTW},
		{nil, []string{"en-US", "zh-TW"}, langEN},
		{nil, []string{"ja-JP", "zh-TW"}, langEN}, // an unsupported first language is English
		{map[string]string{"LINECLI_LANG": "zh-TW"}, []string{"en-US"}, langZhTW},
		{map[string]string{"LINECLI_LANG": "en"}, []string{"zh-TW"}, langEN},
		{map[string]string{"LANG": "zh_TW.UTF-8"}, []string{"en-US"}, langZhTW},
		{map[string]string{"LANG": "zh_CN.UTF-8"}, nil, langZhTW},
		{map[string]string{"LANG": "ja_JP.UTF-8"}, []string{"zh-TW"}, langEN},
		{map[string]string{"LANG": "C.UTF-8"}, []string{"zh-TW"}, langZhTW},
		{map[string]string{"LC_ALL": "POSIX", "LANG": "en_US.UTF-8"}, []string{"zh-TW"}, langEN},
		{map[string]string{"LANGUAGE": "zh_TW:en"}, nil, langZhTW},
		{map[string]string{"LINECLI_LANG": "zh", "LANG": "en_US.UTF-8"}, nil, langZhTW},
	} {
		got := detectLang(func(k string) string { return c.env[k] }, func() []string { return c.system })
		if got != c.want {
			t.Errorf("env %v system %v: got %d, want %d", c.env, c.system, got, c.want)
		}
	}
}

var humanFlagRe = regexp.MustCompile(`(?:^|[\s,])--?([a-z][a-z-]*)`)

// Every command has help for people in both languages, and it names exactly
// the flags the command defines.
func TestHumanHelpMatchesCommandsAndFlags(t *testing.T) {
	byName := map[string]humanCmd{}
	for _, c := range humanCmds() {
		if _, dup := byName[c.name]; dup {
			t.Errorf("%s: listed twice", c.name)
		}
		byName[c.name] = c
		texts := append([][2]string{c.short, c.long}, func() (out [][2]string) {
			for _, f := range c.flags {
				out = append(out, f.text)
			}
			return
		}()...)
		for _, tx := range texts {
			if tx[0] == "" || tx[1] == "" || tx[0] == tx[1] {
				t.Errorf("%s: text %q needs English and Traditional Chinese", c.name, tx)
			}
		}
		if len(c.usage) == 0 || (c.group != -1 && (c.group < groupRead || c.group > groupSetup)) {
			t.Errorf("%s: no usage or a bad group", c.name)
		}
	}
	global := map[string]bool{"text": true, "compact": true, "jsonl": true, "out": true, "h": true, "help": true}
	for name, fs := range registeredFlags(t) {
		c, ok := byName[name]
		if !ok {
			t.Errorf("%s has no help for people", name)
			continue
		}
		helped := map[string]bool{}
		for _, f := range c.flags {
			for _, m := range humanFlagRe.FindAllStringSubmatch(f.flag, -1) {
				helped[m[1]] = true
				if fs.Lookup(m[1]) == nil {
					t.Errorf("%s: help names --%s, which the command does not define", name, m[1])
				}
			}
		}
		groups := map[any][]string{}
		fs.VisitAll(func(f *flag.Flag) { groups[flagKey(f)] = append(groups[flagKey(f)], f.Name) })
		for _, names := range groups {
			if !slices.ContainsFunc(names, func(n string) bool { return helped[n] || global[n] }) {
				t.Errorf("%s: flag %v is missing from the help for people", name, names)
			}
		}
		for _, u := range c.usage {
			if !strings.HasPrefix(u, name) {
				t.Errorf("%s: usage %q does not start with the command", name, u)
			}
		}
	}
}

func TestHumanHelpOutput(t *testing.T) {
	useEdition(t, "full")
	t.Setenv("LINECLI_LANG", "en")
	for _, args := range [][]string{nil, {"help"}, {"-h"}, {"--help"}, {"help", "--text"}} {
		code, out := runCLI(args...)
		s := string(out)
		if code != 0 || json.Valid(out) || !strings.HasPrefix(s, "line-cli reads your own LINE PC data") {
			t.Errorf("%v: rc=%d out=%.80s", args, code, out)
			continue
		}
		for _, c := range humanCmds() {
			if c.group >= 0 && !strings.Contains(s, "\n  line "+c.name) {
				t.Errorf("%v: overview lacks %s", args, c.name)
			}
		}
		for _, want := range []string{"line help --json", "line skill install", "--text"} {
			if !strings.Contains(s, want) {
				t.Errorf("%v: overview lacks %q", args, want)
			}
		}
	}
	code, out := runCLI("send", "--help")
	if s := string(out); code != 0 || !strings.HasPrefix(s, "Usage:\n  line send --to") || !strings.Contains(s, "--dry-run") || !strings.Contains(s, "Flags:") {
		t.Errorf("send --help: rc=%d out=%s", code, out)
	}

	t.Setenv("LINECLI_LANG", "zh-TW")
	code, out = runCLI()
	if s := string(out); code != 0 || !strings.Contains(s, "用法：line <指令> [參數]") || !strings.Contains(s, "讀取（唯讀）") {
		t.Errorf("zh-TW overview: rc=%d out=%.200s", code, out)
	}
	code, out = runCLI("help", "chats")
	if s := string(out); code != 0 || !strings.HasPrefix(s, "用法：\n  line chats") || !strings.Contains(s, "參數：") || !strings.Contains(s, "（預設 50）") {
		t.Errorf("zh-TW help chats: rc=%d out=%s", code, out)
	}
	// Agents get the same JSON in every language.
	code, out = runCLI("read", "--help", "--json")
	var c cmdDoc
	if code != 0 || json.Unmarshal(out, &c) != nil || c.Name != "read" {
		t.Errorf("zh-TW read --help --json: rc=%d out=%.80s", code, out)
	}
}
