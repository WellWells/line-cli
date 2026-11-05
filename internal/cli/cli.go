package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/WellWells/line-cli/internal/line"
)

type runCtx struct {
	out     io.Writer // command output: stdout, or a buffer saved to --out FILE
	stdout  io.Writer
	err     io.Writer
	text    bool
	jsonl   bool
	compact bool
	ids     bool // --ids: sender_id on every row, not only where a name is shared
	outFile string
	outTmp  *os.File // created next to outFile when --out is parsed, renamed onto it at the end
	saved   bytes.Buffer
	sum     outSummary
	cmd     string
	offline bool // --offline: nothing may reach the network, so LINE is not started either
	// dataAsOf is set when LINE is not running: the newest database write.
	dataAsOf string
}

// outSummary is printed on stdout instead of the output when --out FILE is set.
type outSummary struct {
	SavedAs      string `json:"saved_as"`
	Count        int    `json:"count"`
	Bytes        int    `json:"bytes"`
	FirstSentAt  string `json:"first_sent_at,omitempty"`
	LastSentAt   string `json:"last_sent_at,omitempty"`
	LimitReached bool   `json:"limit_reached"`
	Next         string `json:"next,omitempty"`
	counted      bool
}

var (
	runMu  sync.Mutex
	active *runCtx // the Run in progress; parseArgs binds the global flags to it
)

func Run(args []string, stdout, stderr io.Writer) int {
	code, _ := run(args, stdout, stderr)
	return code
}

// run is Run that also returns the finished run, for what Main does after it.
func run(args []string, stdout, stderr io.Writer) (int, *runCtx) {
	runMu.Lock()
	defer runMu.Unlock()
	rc := &runCtx{out: stdout, stdout: stdout, err: stderr}
	active = rc
	defer func() {
		active = nil
		rc.dropOut()
	}()
	n, err := rc.leadingFlags(args)
	if err != nil {
		return rc.fail(err), rc
	}
	code := rc.dispatch(args[n:])
	if code == 0 && rc.outFile != "" {
		return rc.saveOut(), rc
	}
	return code, rc
}

// commands maps each command name to its handler; help documents every one.
var commands = map[string]func(*runCtx, []string) int{
	"help": (*runCtx).helpCmd, "version": (*runCtx).version, "status": (*runCtx).status, "init": (*runCtx).init,
	"chats": (*runCtx).chats, "contacts": (*runCtx).contacts, "members": (*runCtx).members, "read": (*runCtx).read,
	"search": (*runCtx).search, "find-file": (*runCtx).findFile, "stats": (*runCtx).stats, "preview": (*runCtx).preview,
	"download": (*runCtx).download, "unread": (*runCtx).unread, "send": (*runCtx).send, "react": (*runCtx).react, "unsend": (*runCtx).unsend,
	"skill": (*runCtx).skill, "uninstall": (*runCtx).uninstall, "update": (*runCtx).update, "watch": (*runCtx).watch,
}

func (rc *runCtx) dispatch(args []string) int {
	if len(args) == 0 {
		return rc.helpCmd(nil)
	}
	cmd, cmdArgs := args[0], args[1:]
	switch cmd {
	case "-h", "--help":
		cmd = "help"
	case "--version":
		cmd = "version"
	}
	rc.cmd = cmd
	run, ok := commands[cmd]
	if !ok {
		if strings.HasPrefix(cmd, "-") {
			return rc.fail(unknownLeadingFlag(cmd))
		}
		en, zh := didYouMean(cmd, commandNames(), quoted("line "))
		return rc.fail(typo("unknown command: "+cmd+en+" (see 'line help')", "沒有 "+cmd+" 這個指令", zh))
	}
	return run(rc, cmdArgs)
}

// globalFlags adds the output flags every command accepts to fs, bound to the
// running command, so parseArgs treats them like the command's own flags: not
// after "--" and not when they are another flag's value. A command's own flag
// of the same name wins (preview and download use --out for a directory).
func globalFlags(fs *flag.FlagSet) {
	rc := active
	if rc == nil {
		rc = &runCtx{}
	}
	for name, p := range map[string]*bool{"text": &rc.text, "jsonl": &rc.jsonl, "compact": &rc.compact, "ids": &rc.ids} {
		if fs.Lookup(name) == nil {
			fs.BoolVar(p, name, *p, "")
		}
	}
	if fs.Lookup("out") == nil {
		fs.Func("out", "", rc.setOutFile)
	}
}

// leadingFlags applies global flags given before the command name and returns
// how many arguments they took.
func (rc *runCtx) leadingFlags(args []string) (int, error) {
	fs := newFlags("line")
	globalFlags(fs)
	i := 0
	for i < len(args) {
		name, _, hasValue := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		f := fs.Lookup(name)
		if !strings.HasPrefix(args[i], "-") || f == nil {
			break
		}
		n := 1
		if !hasValue && !isBoolFlag(f) {
			n = 2
		}
		if err := fs.Parse(args[i:min(i+n, len(args))]); err != nil {
			return 0, invalid(err.Error())
		}
		i += n
	}
	return i, nil
}

// setOutFile checks --out FILE before the command runs, so an unwritable path
// fails before any work (or a send) is done: the folder must exist and the file,
// if present, must be writable. The output goes to a temp file beside it.
func (rc *runCtx) setOutFile(p string) error {
	if strings.TrimSpace(p) == "" {
		return errors.New("empty path")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
		return errors.New("is a directory: " + abs)
	}
	dir := filepath.Dir(abs)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return errors.New("no such folder: " + dir + " (--out does not create folders)")
	}
	if f, err := os.OpenFile(abs, os.O_WRONLY, 0); err == nil {
		f.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("cannot write: " + err.Error())
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(abs)+".tmp*")
	if err != nil {
		return errors.New("cannot write in " + dir + ": " + err.Error())
	}
	rc.dropOut()
	rc.outFile, rc.outTmp = abs, tmp
	rc.out = &rc.saved
	return nil
}

// dropOut removes the --out temp file unless saveOut renamed it.
func (rc *runCtx) dropOut() {
	if rc.outTmp != nil {
		rc.outTmp.Close()
		os.Remove(rc.outTmp.Name())
		rc.outTmp = nil
	}
}

// saveOut writes the buffered output to --out FILE and prints a summary on
// stdout. The command has already succeeded (a message may have been sent), so
// if the file cannot be written after all, the output goes to stdout instead.
func (rc *runCtx) saveOut() int {
	b := rc.saved.Bytes()
	tmp := rc.outTmp
	_, err := tmp.Write(b)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), rc.outFile)
	}
	if err != nil {
		rc.dropOut()
		rc.notify("warning: could not write --out file (" + err.Error() + "); the output follows on stdout instead")
		rc.stdout.Write(b)
		return 0
	}
	rc.outTmp = nil
	s := rc.sum
	s.SavedAs, s.Bytes = rc.outFile, len(b)
	if !s.counted {
		s.Count = bytes.Count(b, []byte("\n"))
	}
	if err := rc.encode(rc.stdout, s); err != nil {
		return rc.fail(err)
	}
	return 0
}

// isTerminal reports whether w is a console a person reads; pipes, files and
// --out buffers are not. A variable so tests can stand in for a console.
var isTerminal = func(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// encode writes v as JSON, indented only for a terminal: agents read it through
// a pipe or a file, where indentation costs tokens and helps nobody. --compact
// keeps it on one line on a terminal too; with --jsonl a list becomes one
// compact JSON value per line and anything else one line.
func (rc *runCtx) encode(w io.Writer, v any) error {
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	if !rc.compact && !rc.jsonl && isTerminal(w) {
		enc.SetIndent("", "  ")
	}
	if rv := reflect.ValueOf(v); rc.jsonl && rv.Kind() == reflect.Slice {
		for i := 0; i < rv.Len(); i++ {
			if err := enc.Encode(rv.Index(i).Interface()); err != nil {
				return err
			}
		}
	} else if err := enc.Encode(v); err != nil {
		return err
	}
	return bw.Flush()
}

func (rc *runCtx) writeJSON(v any) error { return rc.encode(rc.out, v) }

func (rc *runCtx) emit(v any) int {
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Slice {
		rc.sum.Count, rc.sum.counted = rv.Len(), true
	} else if !rc.sum.counted {
		rc.sum.Count, rc.sum.counted = 1, true
	}
	if err := rc.writeJSON(v); err != nil {
		return rc.fail(err)
	}
	return 0
}

type errorOutput struct {
	Error      string `json:"error"`
	Message    string `json:"message"`
	Candidates any    `json:"candidates,omitempty"`
}

// helpRequest is what parseArgs returns for "<cmd> --help" or "<cmd> -h";
// fail prints that command's help instead of an error, as JSON with --json.
type helpRequest struct {
	cmd  string
	json bool
}

func (h *helpRequest) Error() string { return "help requested for " + h.cmd }

func isHelpFlag(fs *flag.FlagSet, a string) bool {
	name := strings.TrimLeft(a, "-")
	return (a == "-h" || a == "-help" || a == "--help") && fs.Lookup(name) == nil
}

func (rc *runCtx) fail(err error) int {
	if hr := (*helpRequest)(nil); errors.As(err, &hr) {
		rc.cmd = "" // asking about a command is not running it
		return rc.commandHelp(hr.cmd, hr.json || rc.compact || rc.jsonl)
	}
	if te := (*typoError)(nil); errors.As(err, &te) {
		if isTerminal(rc.err) && userLang() == langZhTW {
			rc.notify(te.zh)
		}
		err = te.error
	}
	out := errorOutput{Error: "ERROR", Message: err.Error()}
	if le, ok := line.AsError(err); ok {
		out.Error = string(le.Code)
		out.Message = le.Message
		out.Candidates = le.CandidateList()
	}
	w := rc.stdout
	if w == nil {
		w = rc.out
	}
	rc.encode(w, out)
	return 1
}

// helpCmd runs "help [command|topic] [--json]": short help for people, or with
// --json (or --compact, --jsonl) the machine-readable help for agents.
func (rc *runCtx) helpCmd(args []string) int {
	fs := newFlags("help")
	asJSON := fs.Bool("json", false, "")
	if err := parseArgs(fs, args); err != nil {
		return rc.fail(err)
	}
	if fs.NArg() > 1 {
		return rc.fail(invalid("help takes one command or topic"))
	}
	wantJSON := *asJSON || rc.compact || rc.jsonl
	if name := positional(fs); name != "" {
		return rc.commandHelp(name, wantJSON)
	}
	if wantJSON {
		return rc.emit(helpDoc())
	}
	printOverview(rc.out, userLang())
	return 0
}

// commandHelp prints one command's help, or one help topic: for people, or
// as JSON when asJSON.
func (rc *runCtx) commandHelp(name string, asJSON bool) int {
	doc := helpDoc()
	if t := doc.topic(name); t != nil {
		if asJSON {
			return rc.emit(t)
		}
		rc.printTopic(t)
		return 0
	}
	for _, c := range doc.Commands {
		if c.Name != name {
			continue
		}
		if asJSON {
			return rc.emit(cmdHelp{cmdDoc: c, GlobalFlags: doc.GlobalDocs})
		}
		printCommandHelp(rc.out, userLang(), name)
		return 0
	}
	// The command names come from the help, not from commands: help is one of
	// them, so using commands here would make its initialization refer to itself.
	names := slices.Clone(helpTopics)
	for _, c := range doc.Commands {
		names = append(names, c.Name)
	}
	slices.Sort(names)
	en, zh := didYouMean(name, names, quoted("line help "))
	msg := "unknown command or help topic: " + name + en + " (topics: " + strings.Join(helpTopics, ", ") + "; see 'line help')"
	return rc.fail(typo(msg, "沒有 "+name+" 這個指令或說明主題", zh))
}

func (rc *runCtx) notify(m string) { fmt.Fprintln(rc.err, "line:", m) }

// noteLimit warns on stderr when a list reached its limit, so callers know to
// page, and records the same signal for the --out summary.
func (rc *runCtx) noteLimit(n, limit int, next string) {
	rc.sum.Count, rc.sum.counted = n, true
	if n >= limit {
		rc.sum.LimitReached = true
		rc.sum.Next = strings.NewReplacer("<first sent_at>", orDefault(rc.sum.FirstSentAt, "<first sent_at>"),
			"<last sent_at>", orDefault(rc.sum.LastSentAt, "<last sent_at>")).Replace(next)
		rc.notify(fmt.Sprintf("note: returned %d = limit %d, more may exist; %s", n, limit, next))
	}
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

const boundaryRepeats = " (items in the boundary second repeat; dedupe by id, or for read without --ids by sent_at, sender and content)"

// notePage is noteLimit for pages cut by sent_at. sent_at is second-precise, so
// when a full page sits in one second, rerunning from that sent_at returns the
// same page; only a larger --limit gets past it.
func (rc *runCtx) notePage(n, limit int, first, last, next string) {
	rc.sum.FirstSentAt, rc.sum.LastSentAt = first, last
	if n >= limit && first != "" && first == last {
		rc.sum.Count, rc.sum.counted, rc.sum.LimitReached = n, true, true
		rc.sum.Next = "rerun with a larger --limit (all " + strconv.Itoa(n) + " items were sent at " + first + ")"
		rc.notify(fmt.Sprintf("note: returned %d = limit %d, all sent at %s; paging by sent_at cannot pass this second, rerun with a larger --limit", n, limit, first))
		return
	}
	rc.noteLimit(n, limit, next+boundaryRepeats)
}

// pageEnds returns the sent_at of the first and last items.
func pageEnds[T any](v []T, sentAt func(T) string) (string, string) {
	if len(v) == 0 {
		return "", ""
	}
	return sentAt(v[0]), sentAt(v[len(v)-1])
}

// maxUnixSeconds is 9999-12-31T23:59:59Z; larger times would overflow the
// millisecond bounds the queries use.
const maxUnixSeconds = 253402300799

func parseTime(s string, endOfDay bool) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		switch {
		case len(s) == 8:
			if t, err := time.ParseInLocation("20060102", s, time.Local); err == nil {
				return dayTime(t, endOfDay), nil
			}
		case n >= 1e12:
			return checkUnix(s, n/1000)
		case n >= 1e9:
			return checkUnix(s, n)
		}
		return 0, invalid("invalid time: " + s + " (an integer must be YYYYMMDD, unix seconds or unix milliseconds)")
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return dayTime(t, endOfDay), nil
	}
	for _, l := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return t.Unix(), nil
		}
	}
	return 0, invalid("invalid time: " + s + " (use YYYY-MM-DD, YYYYMMDD, 'YYYY-MM-DD HH:MM', RFC3339, unix seconds or unix milliseconds)")
}

func dayTime(t time.Time, endOfDay bool) int64 {
	if endOfDay {
		t = t.AddDate(0, 0, 1).Add(-time.Second)
	}
	return t.Unix()
}

func checkUnix(s string, sec int64) (int64, error) {
	if sec > maxUnixSeconds {
		return 0, invalid("invalid time: " + s + " (out of range)")
	}
	return sec, nil
}
