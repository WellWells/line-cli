package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These run without LINE: argument handling, output formats and help only.

func TestParseTimeIntegerForms(t *testing.T) {
	day := time.Date(2026, 10, 30, 0, 0, 0, 0, time.Local).Unix()
	for _, c := range []struct {
		in       string
		endOfDay bool
		want     int64
	}{
		{"20261030", false, day},
		{"20261030", true, day + 86399},
		{"2026-10-30", true, day + 86399},
		{"1793340134", false, 1793340134},
		{"1793340134095", false, 1793340134},
		{"1793340134095", true, 1793340134},
		{"2026-10-30T08:00:00Z", false, time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC).Unix()},
	} {
		got, err := parseTime(c.in, c.endOfDay)
		if err != nil || got != c.want {
			t.Errorf("parseTime(%q, %v) = %d, %v; want %d", c.in, c.endOfDay, got, err, c.want)
		}
	}
	for _, in := range []string{"2026", "0", "-5", "123456789", "99999999", "20261301", "999999999999", "9223372036854775807", "99999999999999999999", "yesterday"} {
		if _, err := parseTime(in, false); err == nil || !strings.Contains(err.Error(), "INVALID_ARGUMENT") {
			t.Errorf("parseTime(%q) accepted: %v", in, err)
		}
	}
}

func outWithActive(t *testing.T) *runCtx {
	t.Helper()
	runMu.Lock()
	rc := &runCtx{out: io.Discard, err: io.Discard}
	active = rc
	t.Cleanup(func() { active = nil; runMu.Unlock() })
	return rc
}

func TestGlobalFlagsOnlyWhereTheyAreFlags(t *testing.T) {
	rc := outWithActive(t)
	fs := newFlags("send")
	msg := fs.String("message", "", "")
	if err := parseArgs(fs, []string{"--message", "--text"}); err != nil || *msg != "--text" || rc.text {
		t.Fatalf("flag value: msg=%q text=%v err=%v", *msg, rc.text, err)
	}
	fs = newFlags("read")
	if err := parseArgs(fs, []string{"x", "--text", "--compact"}); err != nil || positional(fs) != "x" || !rc.text || !rc.compact {
		t.Fatalf("trailing globals: pos=%q text=%v compact=%v err=%v", positional(fs), rc.text, rc.compact, err)
	}
	rc.text = false
	fs = newFlags("search")
	if err := parseArgs(fs, []string{"--", "--text", "-1", "--jsonl"}); err != nil || positional(fs) != "--text -1 --jsonl" || rc.text || rc.jsonl {
		t.Fatalf("after --: pos=%q text=%v jsonl=%v err=%v", positional(fs), rc.text, rc.jsonl, err)
	}
	fs = newFlags("preview")
	dir := fs.String("out", "", "")
	if err := parseArgs(fs, []string{"x", "--out", "media"}); err != nil || *dir != "media" || rc.outFile != "" {
		t.Fatalf("command --out must win: dir=%q outFile=%q err=%v", *dir, rc.outFile, err)
	}
}

func TestCommandHelpFlag(t *testing.T) {
	// Should a command ever act before parsing its flags, it finds no LINE data here.
	t.Setenv("LOCALAPPDATA", t.TempDir())
	for _, c := range helpDoc().Commands {
		if c.Name == "init" {
			continue
		}
		for _, flag := range []string{"--help", "-h"} {
			code, out := runCLI(c.Name, flag, "--json")
			var got cmdDoc
			if code != 0 || json.Unmarshal(out, &got) != nil || got.Name != c.Name || got.Usage != c.Usage {
				t.Errorf("%s %s --json: rc=%d out=%.80s", c.Name, flag, code, out)
			}
			code, out = runCLI(c.Name, flag)
			if code != 0 || !strings.Contains(string(out), "line "+c.Name) || json.Valid(out) {
				t.Errorf("%s %s: rc=%d out=%.80s", c.Name, flag, code, out)
			}
		}
	}
	for _, args := range [][]string{{"help", "stats", "--json"}, {"-h", "stats", "--json"}, {"--help", "stats", "--json"}, {"stats", "--help", "--compact"}} {
		code, out := runCLI(args...)
		var got cmdDoc
		if code != 0 || json.Unmarshal(out, &got) != nil || got.Name != "stats" {
			t.Errorf("%v: rc=%d", args, code)
		}
	}
	for _, args := range [][]string{{"help", "nope"}, {"send", "--message", "-h"}} {
		code, out := runCLI(args...)
		var e errorOutput
		if code != 1 || json.Unmarshal(out, &e) != nil || e.Error != "INVALID_ARGUMENT" {
			t.Errorf("%v: rc=%d out=%s", args, code, out)
		}
	}
}

func TestCompactAndJSONL(t *testing.T) {
	var b bytes.Buffer
	rc := &runCtx{jsonl: true}
	rc.encode(&b, []map[string]int{{"a": 1}, {"a": 2}})
	if b.String() != "{\"a\":1}\n{\"a\":2}\n" {
		t.Fatalf("jsonl list: %q", b.String())
	}
	b.Reset()
	rc.encode(&b, []int{})
	if b.Len() != 0 {
		t.Fatalf("jsonl empty list: %q", b.String())
	}
	b.Reset()
	rc = &runCtx{compact: true}
	rc.encode(&b, []string{"<a&b>"})
	if b.String() != "[\"<a&b>\"]\n" {
		t.Fatalf("compact: %q", b.String())
	}
	for _, args := range [][]string{{"--jsonl", "help", "--json"}, {"help", "--json", "--compact"}, {"read", "--jsonl"}} {
		_, out := runCLI(args...)
		if bytes.Count(out, []byte("\n")) != 1 || !json.Valid(out) {
			t.Errorf("%v: not one JSON line: %q", args, out)
		}
	}
	// Not a terminal, so one line; output_lean_test covers a terminal.
	if _, out := runCLI("help", "--json"); bytes.Count(out, []byte("\n")) != 1 {
		t.Error("piped default output is not one line")
	}
}

func TestOutFileSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "help.json")
	code, out := runCLI("help", "--json", "--out", path)
	var s map[string]any
	if code != 0 || json.Unmarshal(out, &s) != nil {
		t.Fatalf("rc=%d out=%s", code, out)
	}
	b, err := os.ReadFile(path)
	if err != nil || !json.Valid(b) {
		t.Fatalf("file: %v", err)
	}
	if s["saved_as"] != path || s["count"] != 1.0 || s["bytes"] != float64(len(b)) || s["limit_reached"] != false {
		t.Fatalf("summary %v", s)
	}
	for _, k := range []string{"saved_as", "count", "bytes", "limit_reached"} {
		if _, ok := s[k]; !ok {
			t.Fatalf("summary lacks %s", k)
		}
	}
	code, out = runCLI("--out", dir, "version")
	if code != 1 || !strings.Contains(string(out), "INVALID_ARGUMENT") {
		t.Fatalf("--out DIR: rc=%d %s", code, out)
	}
	failed := filepath.Join(dir, "err.json")
	code, out = runCLI("read", "--out", failed)
	if code != 1 || !strings.Contains(string(out), "INVALID_ARGUMENT") {
		t.Fatalf("error with --out: rc=%d %s", code, out)
	}
	if _, err := os.Stat(failed); !os.IsNotExist(err) {
		t.Fatal("--out file written for a failed command")
	}
	outOnlyFiles(t, dir, "help.json")
}

func TestPagingSignalForOutSummary(t *testing.T) {
	rc := &runCtx{out: io.Discard, err: io.Discard}
	rc.notePage(3, 3, "A", "B", "older: rerun with --until <first sent_at>")
	if !rc.sum.LimitReached || rc.sum.Count != 3 || rc.sum.Next != "older: rerun with --until A"+boundaryRepeats || rc.sum.FirstSentAt != "A" || rc.sum.LastSentAt != "B" {
		t.Fatalf("page: %+v", rc.sum)
	}
	rc = &runCtx{out: io.Discard, err: io.Discard}
	rc.notePage(2, 2, "S", "S", "older: x")
	if !rc.sum.LimitReached || !strings.Contains(rc.sum.Next, "larger --limit") {
		t.Fatalf("stall: %+v", rc.sum)
	}
	rc = &runCtx{out: io.Discard, err: io.Discard}
	rc.notePage(1, 2, "A", "A", "older: x")
	if rc.sum.LimitReached || rc.sum.Next != "" {
		t.Fatalf("below limit: %+v", rc.sum)
	}
}

func TestNewFlagErrorsBeforeOpeningDB(t *testing.T) {
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"stats"}, "chat_id"},
		{[]string{"stats", "x", "--by", "week"}, "--by"},
		{[]string{"stats", "x", "--since", "2026"}, "invalid time"},
		{[]string{"stats", "x", "--limit", "-1"}, "--limit"},
		{[]string{"find-file", "--order", "up"}, "--order"},
		{[]string{"find-file", "--chat-type", "bogus"}, "--chat-type"},
		{[]string{"find-file", "--until", "123"}, "invalid time"},
		{[]string{"preview", "x", "--until", "20261301"}, "invalid time"},
		{[]string{"download", "x", "--since", "2026-10-30", "--until", "2026-10-01"}, "later than"},
	} {
		code, out := runCLI(c.args...)
		var e errorOutput
		if code != 1 || json.Unmarshal(out, &e) != nil || e.Error != "INVALID_ARGUMENT" || !strings.Contains(e.Message, c.msg) {
			t.Errorf("%v: rc=%d out=%s", c.args, code, out)
		}
	}
}

func TestHelpDocumentsOutputTrack(t *testing.T) {
	doc := helpDoc()
	names := make([]string, len(doc.Commands))
	byName := map[string]cmdDoc{}
	for i, c := range doc.Commands {
		names[i] = c.Name
		byName[c.Name] = c
	}
	if i := strings.Index(strings.Join(names, " "), "find-file stats "); i < 0 {
		t.Errorf("stats does not follow find-file: %v", names)
	}
	if strings.Join(doc.Global, " ") != "--text --compact --jsonl --ids --out" {
		t.Errorf("global_flags must stay bare flag names: %q", doc.Global)
	}
	global := strings.Join(doc.GlobalDocs, " ")
	for _, want := range []string{"--text", "--jsonl", "--compact", "--ids", "--out FILE", "--help", "does not create folders"} {
		if !strings.Contains(global, want) {
			t.Errorf("global flag docs lack %s", want)
		}
	}
	for _, want := range []string{"2>&1", "limit_reached", "bytes"} {
		if !strings.Contains(doc.Output, want) {
			t.Errorf("output text lacks %s", want)
		}
	}
	if !strings.Contains(doc.Time, "YYYYMMDD") || !strings.Contains(doc.Time, "milliseconds") {
		t.Error("time format lacks YYYYMMDD or milliseconds")
	}
	for name, wants := range map[string][]string{
		"preview":   {"default 50", "unsupported", "write_failed", "chat_id", "note", "LOCALAPPDATA", "newest"},
		"download":  {"unsupported", "write_failed", "chat_id", "LOCALAPPDATA", "voice", "newest"},
		"find-file": {"--until", "--chat-type", "--order", "local_path"},
		"stats":     {"--by", "--include-events", "group_count", "--limit does not apply"},
	} {
		all := byName[name].Usage + byName[name].Summary + strings.Join(byName[name].Flags, " ") + byName[name].Output
		for _, w := range wants {
			if !strings.Contains(all, w) {
				t.Errorf("%s help lacks %q", name, w)
			}
		}
	}
}

// line version prints one line of text, as git --version does; the output flags
// that shape JSON have nothing to do.
func TestVersionIsOneLineOfText(t *testing.T) {
	want := "line version " + Version + "\n"
	for _, args := range [][]string{{"version"}, {"--version"}, {"version", "--compact"}, {"--jsonl", "version"}} {
		if code, out := runCLI(args...); code != 0 || string(out) != want {
			t.Errorf("%v: rc=%d out=%q, want %q", args, code, out, want)
		}
	}
}
