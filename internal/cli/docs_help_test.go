package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/WellWells/line-cli/internal/line"
)

// These run without LINE: they check help against the code.

var errSpied = errors.New("flags inspected")

// registeredFlags runs every command with flagSpy set, so each stops at
// parseArgs, and returns the flags each one defines (not the global ones).
func registeredFlags(t *testing.T) map[string]*flag.FlagSet {
	t.Helper()
	t.Setenv("LOCALAPPDATA", t.TempDir())
	oldDB, oldInit, oldForget := openDB, initKey, forgetKey
	openDB = func(func(string), bool) (*line.DB, error) { t.Fatal("a command opened the database"); return nil, nil }
	initKey = func(func(string)) error { t.Fatal("init scanned for the key"); return nil }
	forgetKey = func() error { t.Fatal("init --forget ran"); return nil }
	t.Cleanup(func() { openDB, initKey, forgetKey, flagSpy = oldDB, oldInit, oldForget, nil })
	got := map[string]*flag.FlagSet{}
	for name := range commands {
		var seen []*flag.FlagSet
		flagSpy = func(fs *flag.FlagSet) error { seen = append(seen, fs); return errSpied }
		code, out := runCLI(name)
		if code != 1 || len(seen) != 1 || seen[0].Name() != name {
			t.Fatalf("%s: rc=%d, parseArgs calls=%d, out=%.80s", name, code, len(seen), out)
		}
		got[name] = seen[0]
	}
	flagSpy = nil
	return got
}

var docFlagRe = regexp.MustCompile(`(?:^|[\s\[(|])(--?[a-z][a-z-]*)`)

// docFlagNames returns the flag names a help Flags list documents: the first
// word of each entry ("-y/--yes" is two names) and "--x is an older name".
func docFlagNames(flags []string) map[string]bool {
	out := map[string]bool{}
	alias := regexp.MustCompile(`--([a-z-]+) is an older name`)
	for _, f := range flags {
		first, _, _ := strings.Cut(f, " ")
		for _, n := range strings.Split(first, "/") {
			out[strings.TrimLeft(n, "-")] = true
		}
		for _, m := range alias.FindAllStringSubmatch(f, -1) {
			out[m[1]] = true
		}
	}
	return out
}

func TestHelpMatchesCommandsAndFlags(t *testing.T) {
	doc := helpDoc()
	var documented, dispatched []string
	for _, c := range doc.Commands {
		documented = append(documented, c.Name)
	}
	for name := range commands {
		dispatched = append(dispatched, name)
	}
	sort.Strings(documented)
	sort.Strings(dispatched)
	if !slices.Equal(documented, dispatched) {
		t.Fatalf("help documents %v, but the commands are %v", documented, dispatched)
	}
	global := map[string]bool{"text": true, "compact": true, "jsonl": true, "out": true, "h": true, "help": true}
	regs := registeredFlags(t)
	for _, c := range doc.Commands {
		fs := regs[c.Name]
		docNames := docFlagNames(c.Flags)
		// Aliases bound to one variable (chats --type/--chat-type) count as one flag.
		groups := map[any][]string{}
		fs.VisitAll(func(f *flag.Flag) { groups[flagKey(f)] = append(groups[flagKey(f)], f.Name) })
		for _, names := range groups {
			if !slices.ContainsFunc(names, func(n string) bool { return docNames[n] }) {
				t.Errorf("%s: flag %v is not in help flags %q", c.Name, names, c.Flags)
			}
		}
		for n := range docNames {
			if fs.Lookup(n) == nil {
				t.Errorf("%s: help documents --%s, which the command does not define", c.Name, n)
			}
		}
		for _, m := range docFlagRe.FindAllStringSubmatch(c.Usage, -1) {
			if n := strings.TrimLeft(m[1], "-"); fs.Lookup(n) == nil && !global[n] {
				t.Errorf("%s: usage %q names %s, which the command does not define", c.Name, c.Usage, m[1])
			}
		}
	}
}

// jsonFields lists the JSON names of t's fields, embedded structs flattened.
func jsonFields(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		switch {
		case name == "-":
		case f.Anonymous && name == "":
			out = append(out, jsonFields(f.Type)...)
		case name != "":
			out = append(out, name)
		}
	}
	return out
}

// docFields parses the field names an Output string lists at the top level and
// under "x[] of:" or "x{...}", leaving out parenthesised explanations.
func docFields(t *testing.T, output string) []string {
	t.Helper()
	_, s, _ := strings.Cut(output, ": ")
	list, _, _ := strings.Cut(messageFields, " (")
	s = strings.Replace(s, messageFields, list, 1)
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '(':
			depth++
		case r == ')':
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	s = strings.NewReplacer("[] of:", ",", "{", ",", "}", "", " or ", ",", "plus ", "").Replace(b.String())
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		if !regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(f) {
			t.Errorf("cannot parse %q in output %q", f, output)
		}
		out = append(out, f)
	}
	return out
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	sort.Strings(a)
	sort.Strings(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}

func TestHelpOutputMatchesJSONFields(t *testing.T) {
	msg := reflect.TypeFor[line.Message]()
	types := map[string][]reflect.Type{
		"status":    {reflect.TypeFor[line.Status](), reflect.TypeFor[line.StaleChats]()},
		"chats":     {reflect.TypeFor[line.ChatSummary]()},
		"contacts":  {reflect.TypeFor[line.ContactSummary]()},
		"members":   {reflect.TypeFor[line.Member]()},
		"read":      {msg},
		"search":    {reflect.TypeFor[line.SearchHit]()},
		"find-file": {reflect.TypeFor[line.FileHit]()},
		"watch":     {reflect.TypeFor[line.SearchHit](), msg},
		"stats":     {reflect.TypeFor[line.StatsResult](), reflect.TypeFor[line.StatsGroup]()},
		"preview":   {reflect.TypeFor[line.PreviewResult]()},
		"download":  {reflect.TypeFor[line.DownloadResult]()},
		"unread":    {reflect.TypeFor[line.UnreadChat](), msg},
		"send":      {reflect.TypeFor[line.SendPlan](), reflect.TypeFor[line.ReplyTarget](), reflect.TypeFor[line.MentionSpan](), reflect.TypeFor[line.SendFilesPlan](), reflect.TypeFor[line.FileSend]()},
		"react":     {reflect.TypeFor[line.ActionPlan](), reflect.TypeFor[line.ReplyTarget]()},
		"unsend":    {reflect.TypeFor[line.ActionPlan](), reflect.TypeFor[line.ReplyTarget]()},
		"update":    {reflect.TypeFor[updateResult]()},
		"skill":     {reflect.TypeFor[skillResult]()},
		"uninstall": {reflect.TypeFor[uninstallResult](), reflect.TypeFor[skillResult]()},
	}
	for _, c := range helpDoc().Commands {
		var want []string
		switch c.Name {
		case "version":
			continue // one line of text, no JSON fields
		case "init":
			want = []string{"ok", "forgotten"}
		default:
			ts, ok := types[c.Name]
			if !ok {
				if c.Output != "" {
					t.Errorf("%s: output documented but not checked", c.Name)
				}
				continue
			}
			for _, typ := range ts {
				want = append(want, jsonFields(typ)...)
			}
		}
		if got := docFields(t, c.Output); !sameSet(got, want) {
			t.Errorf("%s: help output lists %v, the JSON has %v", c.Name, got, want)
		}
	}
	// Fields of the structs inside a message are described in messageFields.
	nested := func(typ reflect.Type) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct {
			return
		}
		for _, f := range jsonFields(typ) {
			if !regexp.MustCompile(`\b` + f + `\b`).MatchString(messageFields) {
				t.Errorf("messageFields does not describe %s.%s", typ.Name(), f)
			}
		}
	}
	for i := 0; i < msg.NumField(); i++ {
		if f := msg.Field(i); f.Type.Kind() != reflect.String {
			nested(f.Type)
		}
	}
}

func TestHelpSectionsForAgents(t *testing.T) {
	doc := helpDoc()
	// Every ErrorCode constant in internal/line/errors.go is documented.
	f, err := parser.ParseFile(token.NewFileSet(), "../line/errors.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	ast.Inspect(f, func(n ast.Node) bool {
		if vs, ok := n.(*ast.ValueSpec); ok && len(vs.Values) == 1 {
			if id, ok := vs.Type.(*ast.Ident); ok && id.Name == "ErrorCode" {
				codes = append(codes, strings.Trim(vs.Values[0].(*ast.BasicLit).Value, `"`))
			}
		}
		return true
	})
	if len(codes) == 0 || !sameSet(append(codes, "ERROR"), doc.Errors) || len(doc.ErrorDocs) != len(doc.Errors) {
		t.Errorf("error codes in help %v, in errors.go %v", doc.Errors, codes)
	}
	all := strings.Join(slices.Concat(doc.Glossary, doc.Semantics, doc.Safety), "\n")
	for _, want := range []string{
		"u = ", "c = group", "m = open chat", "t = open-chat thread", "p = open-chat member", "v = bot",
		"sent_at", "from_me", "event row", "unsent row", "CHAT_AMBIGUOUS", "SENDER_AMBIGUOUS",
		"untrusted", "--message-file", "$OutputEncoding", "--dry-run", "suspend", "dedupe by id",
		"default", "--out FILE",
	} {
		if !strings.Contains(all, want) && !strings.Contains(limitsDoc(), want) {
			t.Errorf("help glossary, semantics and safety lack %q", want)
		}
	}
	steps := ""
	for _, r := range doc.Recipes {
		if r.Goal == "" || len(r.Steps) == 0 {
			t.Errorf("empty recipe %+v", r)
		}
		steps += strings.Join(r.Steps, "\n") + "\n"
	}
	for _, want := range []string{"line chats", "line contacts", "--since", "line search", "read --around", "line stats",
		"--dry-run", "-y", "find-file", "download --id", "unread"} {
		if !strings.Contains(steps, want) {
			t.Errorf("recipes lack %q", want)
		}
	}
	for _, want := range []string{"chats 50,", "read 200 (500", "20 with --around", "stats 100", "capped at 5000"} {
		if !strings.Contains(limitsDoc(), want) {
			t.Errorf("limits lack %q: %s", want, limitsDoc())
		}
	}
	if !strings.Contains(doc.About, "line help <topic>") || !strings.Contains(doc.About, "status") {
		t.Errorf("about does not point to topics and status: %s", doc.About)
	}
}

// TestHelpEventNames checks that the event field's doc names every event the
// parser can give (the values of eventTypes and noticeKeys in message.go).
func TestHelpEventNames(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../line/message.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || (vs.Names[0].Name != "eventTypes" && vs.Names[0].Name != "noticeKeys") {
			return true
		}
		for _, e := range vs.Values[0].(*ast.CompositeLit).Elts {
			names = append(names, strings.Trim(e.(*ast.KeyValueExpr).Value.(*ast.BasicLit).Value, `"`))
		}
		return false
	})
	start := strings.Index(messageFields, "event: ")
	end := strings.Index(messageFields[start:], "; targets")
	if len(names) < 10 || start < 0 || end < 0 {
		t.Fatalf("event names %v, event doc at %d..%d", names, start, end)
	}
	doc := messageFields[start : start+end]
	for _, name := range names {
		if !regexp.MustCompile(`[ |]` + regexp.QuoteMeta(name) + `\b`).MatchString(doc) {
			t.Errorf("event doc lacks %q: %s", name, doc)
		}
	}
}

// TestHelpCommandListsGlobalFlags checks that one command's help also gives the
// global flags, so an agent reading only 'help read --json' finds --out and --ids.
func TestHelpCommandListsGlobalFlags(t *testing.T) {
	code, out := runCLI("help", "read", "--json")
	var c struct {
		Name   string   `json:"name"`
		Flags  []string `json:"flags"`
		Global []string `json:"global_flags"`
	}
	if code != 0 || json.Unmarshal(out, &c) != nil || c.Name != "read" || len(c.Flags) == 0 {
		t.Fatalf("help read --json: rc=%d out=%.200s", code, out)
	}
	all := strings.Join(c.Global, "\n")
	for _, want := range []string{"--ids", "--out FILE", "--jsonl", "--text"} {
		if !strings.Contains(all, want) {
			t.Errorf("help read --json global_flags lack %q: %v", want, c.Global)
		}
	}
}

// TestHelpIDsFlagSaysMessageIDs checks that --ids says it adds message ids to
// read and unread, which paging needs to dedupe by id.
func TestHelpIDsFlagSaysMessageIDs(t *testing.T) {
	for _, d := range helpDoc().GlobalDocs {
		if strings.HasPrefix(d, "--ids:") {
			if !strings.Contains(d, "read and unread") || !strings.Contains(d, "dedupe") {
				t.Errorf("--ids doc does not say it adds message ids for paging: %s", d)
			}
			return
		}
	}
	t.Error("no --ids doc")
}

func TestHelpTopics(t *testing.T) {
	for _, topic := range helpTopics {
		code, out := runCLI("help", topic, "--json")
		var v []any
		if code != 0 || json.Unmarshal(out, &v) != nil || len(v) == 0 {
			t.Errorf("help %s --json: rc=%d out=%.80s", topic, code, out)
		}
		code, out = runCLI("help", topic)
		if code != 0 || !strings.HasPrefix(string(out), "    ") {
			t.Errorf("help %s: rc=%d out=%.80s", topic, code, out)
		}
	}
	for _, args := range [][]string{{"help", "--json"}, {"help", "--compact"}, {"--jsonl", "help"}} {
		code, out := runCLI(args...)
		var doc helpInfo
		if code != 0 || json.Unmarshal(bytes.TrimSpace(out), &doc) != nil || doc.Tool != "line" {
			t.Errorf("%v: rc=%d out=%.80s", args, code, out)
		}
	}
	code, out := runCLI("help", "nope")
	var e errorOutput
	if code != 1 || json.Unmarshal(out, &e) != nil || !strings.Contains(e.Message, "glossary") {
		t.Errorf("help nope: rc=%d out=%s", code, out)
	}
}
