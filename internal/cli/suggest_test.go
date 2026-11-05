package cli

import (
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestEditDistance(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"read", "read", 0},
		{"red", "read", 1},   // a letter missing
		{"reaad", "read", 1}, // one extra
		{"reed", "read", 1},  // one wrong
		{"raed", "read", 1},  // two swapped
		{"sned", "send", 1},
		{"stat", "status", 2},
		{"群組", "群組們", 1}, // runes, not bytes
	} {
		if got := editDistance(c.a, c.b); got != c.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSuggest(t *testing.T) {
	cmds := commandNames()
	for _, c := range []struct {
		typed string
		want  []string
	}{
		{"raed", []string{"read"}},
		{"sned", []string{"send"}},
		{"READ", []string{"read"}},
		{"chat", []string{"chats"}},
		{"stat", []string{"stats"}}, // the closest only, not status too
		{"downlaod", []string{"download"}},
		{"serach", []string{"search"}},
		{"contact", []string{"contacts"}},
		{"find", []string{"find-file"}},      // nothing close: what starts with it
		{"sta", []string{"stats", "status"}}, // several start with it
		{"updat", []string{"update"}},
		{"upgrad", nil},
		{"ls", nil},
		{"x", nil},
		{"", nil},
		{"bogus", nil},
	} {
		if got := suggest(c.typed, cmds); !slices.Equal(got, c.want) {
			t.Errorf("suggest(%q) = %q, want %q", c.typed, got, c.want)
		}
	}
	flags := []string{"chat-type", "dry-run", "limit", "message", "y", "yes"}
	for typed, want := range map[string]string{"limt": "limit", "dryrun": "dry-run", "chat_type": "chat-type", "mesage": "message", "Limit": "limit"} {
		if got := suggest(typed, flags); !slices.Equal(got, []string{want}) {
			t.Errorf("suggest(%q, flags) = %q, want %q", typed, got, want)
		}
	}
}

func TestOrList(t *testing.T) {
	for _, c := range []struct {
		in     []string
		en, zh string
	}{
		{[]string{"a"}, "a", "a"},
		{[]string{"a", "b"}, "a or b", "a 或 b"},
		{[]string{"a", "b", "c"}, "a, b or c", "a、b 或 c"},
	} {
		if got := orList(c.in, ", ", " or "); got != c.en {
			t.Errorf("orList(%q) = %q, want %q", c.in, got, c.en)
		}
		if got := orList(c.in, "、", " 或 "); got != c.zh {
			t.Errorf("orList(%q) = %q, want %q", c.in, got, c.zh)
		}
	}
}

// A person at a Chinese console also gets the hint in Chinese on stderr; the
// JSON error stays the same English one agents read.
func TestTypoNoteInChinese(t *testing.T) {
	defer func(f func(io.Writer) bool) { isTerminal = f }(isTerminal)
	isTerminal = func(io.Writer) bool { return true }
	t.Setenv("LINECLI_LANG", "zh-TW")
	for _, c := range []struct {
		args []string
		msg  string // part of the JSON message
		note string // the whole stderr; "" = none
	}{
		{[]string{"raed"}, "did you mean 'line read'?", "line: 沒有 raed 這個指令，你是不是要打 line read？\n"},
		{[]string{"sta"}, "did you mean", "line: 沒有 sta 這個指令，你是不是要打 line stats 或 line status？\n"},
		{[]string{"read", "--limt", "5", "x"}, "did you mean --limit?", "line: read 沒有 --limt 這個旗標，你是不是要打 --limit？\n"},
		{[]string{"chats", "--json"}, "output is JSON by default", "line: chats 沒有 --json 這個旗標，輸出本來就是 JSON；--json 只用在 help，例如 line help chats --json。\n"},
		{[]string{"help", "raed"}, "did you mean 'line help read'?", "line: 沒有 raed 這個指令或說明主題，你是不是要打 line help read？\n"},
		{[]string{"--txet", "chats"}, "did you mean --text?", "line: 沒有 --txet 這個旗標，你是不是要打 --text？\n"},
		{[]string{"skill", "instal"}, "did you mean 'line skill install'?", "line: skill 沒有 instal 這個動作，你是不是要打 line skill install？\n"},
		{[]string{"bogus"}, "unknown command: bogus", ""}, // nothing to suggest
		{[]string{"chats", "--nope"}, "-nope", ""},
	} {
		rc, out, stderr := runCLIErr(c.args...)
		var e errorOutput
		if rc != 1 || json.Unmarshal(out, &e) != nil || e.Error != "INVALID_ARGUMENT" || !strings.Contains(e.Message, c.msg) {
			t.Errorf("%v: rc=%d out=%s", c.args, rc, out)
		}
		if stderr != c.note {
			t.Errorf("%v: stderr %q, want %q", c.args, stderr, c.note)
		}
	}
	// Not for English, nor when no person reads the console.
	t.Setenv("LINECLI_LANG", "en")
	if _, _, stderr := runCLIErr("raed"); stderr != "" {
		t.Errorf("English: stderr %q", stderr)
	}
	t.Setenv("LINECLI_LANG", "zh-TW")
	isTerminal = func(io.Writer) bool { return false }
	if _, _, stderr := runCLIErr("raed"); stderr != "" {
		t.Errorf("not a console: stderr %q", stderr)
	}
}

// Mistyped commands, flags, help topics and skill actions are still
// INVALID_ARGUMENT, now with a hint at what was probably meant.
func TestDidYouMeanErrors(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string // "" = no hint
	}{
		{[]string{"raed"}, "unknown command: raed; did you mean 'line read'? (see 'line help')"},
		{[]string{"sta"}, "did you mean 'line stats' or 'line status'?"},
		{[]string{"bogus"}, ""},
		{[]string{"read", "--limt", "5", "x"}, "flag provided but not defined: -limt; did you mean --limit?"},
		{[]string{"send", "--to", "x", "--dryrun"}, "did you mean --dry-run?"},
		{[]string{"chats", "--txet"}, "did you mean --text?"}, // global flags count too
		{[]string{"chats", "--hlep"}, "did you mean --help?"},
		{[]string{"chats", "--nope"}, ""},
		{[]string{"chats", "--json"}, "output is JSON by default (--json goes only with help, as in 'line help chats --json')"},
		{[]string{"help", "raed"}, "unknown command or help topic: raed; did you mean 'line help read'?"},
		{[]string{"help", "recipe"}, "did you mean 'line help recipes'?"},
		{[]string{"help", "bogus"}, ""},
		{[]string{"--txet", "chats"}, "unknown flag: --txet; did you mean --text? (see 'line help')"},
		{[]string{"--verison"}, "did you mean --version?"},
		{[]string{"--limit", "5", "read", "x"}, "unknown flag: --limit; only --compact, --ids, --jsonl, --out and --text go before the command (see 'line help')"},
		{[]string{"skill", "instal"}, "skill takes install, uninstall or nothing, not instal; did you mean 'line skill install'?"},
	} {
		rc, out := runCLI(c.args...)
		var e errorOutput
		if rc != 1 || json.Unmarshal(out, &e) != nil || e.Error != "INVALID_ARGUMENT" {
			t.Errorf("%v: rc=%d out=%s", c.args, rc, out)
			continue
		}
		if c.want == "" && strings.Contains(e.Message, "did you mean") || c.want != "" && !strings.Contains(e.Message, c.want) {
			t.Errorf("%v: message %q, want it to contain %q", c.args, e.Message, c.want)
		}
	}
}
