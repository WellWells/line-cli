package cli

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/WellWells/line-cli/internal/line"
)

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// isRepeatable reports a flag that may be given more than once (send --file).
func isRepeatable(f *flag.Flag) bool {
	_, ok := f.Value.(*pathList)
	return ok
}

// pathList collects every value of a repeatable path flag.
type pathList []string

func (p *pathList) String() string { return strings.Join(*p, ", ") }

func (p *pathList) Set(v string) error {
	*p = append(*p, v)
	return nil
}

func isBoolFlag(f *flag.Flag) bool {
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
}

// parseArgs parses flags anywhere among args, plus the global output flags.
// Everything after "--" is positional. A flag given twice is an error, and so is
// an explicit value (--flag=false) on any bool flag named in strictBools.
// "-h"/"--help" asks for the command's help (see helpRequest).
func parseArgs(fs *flag.FlagSet, args []string, strictBools ...string) error {
	if flagSpy != nil {
		return flagSpy(fs)
	}
	globalFlags(fs)
	var flags, pos []string
	seen := map[any]bool{}
	help, asJSON := false, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if isHelpFlag(fs, a) {
			help = true
			continue
		}
		if a == "--json" && fs.Lookup("json") == nil {
			asJSON = true // only with --help; otherwise fs.Parse refuses it
		}
		if len(a) > 1 && strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			name, _, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
			f := fs.Lookup(name)
			if f == nil {
				continue
			}
			k := flagKey(f)
			if seen[k] && !isRepeatable(f) {
				return invalid("flag given more than once: " + a)
			}
			seen[k] = true
			if isBoolFlag(f) {
				if hasValue && slices.Contains(strictBools, f.Name) {
					return invalid("flag -" + f.Name + " takes no value: " + a)
				}
				continue
			}
			if !hasValue {
				if i+1 >= len(args) {
					return invalid("flag needs an argument: " + a)
				}
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		pos = append(pos, a)
	}
	err := fs.Parse(append(append(flags, "--"), pos...))
	if help || errors.Is(err, flag.ErrHelp) {
		if f := fs.Lookup("json"); f != nil && f.Value.String() == "true" {
			asJSON = true // help's own --json
		}
		return &helpRequest{fs.Name(), asJSON}
	}
	if err != nil {
		return flagError(fs, err)
	}
	return nil
}

// flagSpy, when set by a test, gets each command's own flags instead of them
// being parsed; its error ends the command before it does anything.
var flagSpy func(*flag.FlagSet) error

// flagKey identifies a flag's variable, so aliases bound to one variable
// (chats --type/--chat-type) count as the same flag.
func flagKey(f *flag.Flag) any {
	if v := reflect.ValueOf(f.Value); v.Kind() == reflect.Pointer {
		return v.Pointer()
	}
	return f.Name
}

func invalid(msg string) error {
	return &line.Error{Code: line.ErrInvalidArgument, Message: msg}
}

func positional(fs *flag.FlagSet) string {
	return strings.TrimSpace(strings.Join(fs.Args(), " "))
}

func checkLimit(name string, v int) error {
	if v < 0 {
		return invalid(name + " must not be negative")
	}
	return nil
}

// openDB opens LINE's database; tests swap in a synthetic one.
var openDB = line.Open

// rerunnable lists commands without side effects, which withReader may run
// again when LINE checkpointed the database while they read it. Any command
// is retried once after DB_UNREADABLE, unless the schema changed.
var rerunnable = map[string]bool{"chats": true, "contacts": true, "members": true, "read": true, "search": true, "find-file": true, "stats": true, "unread": true}

func (rc *runCtx) withReader(fn func(*line.Reader) error) error {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		var db *line.DB
		db, err = openDB(rc.notify, !rc.offline)
		if err != nil {
			return err
		}
		if attempt == 0 {
			rc.noteLineStopped()
		}
		err = fn(line.NewReader(db))
		changed := attempt == 0 && rerunnable[rc.cmd] && db.Changed()
		db.Close()
		if changed {
			continue
		}
		if le, ok := line.AsError(err); !ok || le.Code != line.ErrDBUnreadable || le.SchemaChanged() {
			return err
		}
	}
	return err
}

func (rc *runCtx) version(args []string) int {
	fs := newFlags("version")
	if err := parseArgs(fs, args); err != nil {
		return rc.fail(err)
	}
	if fs.NArg() > 0 {
		return rc.fail(invalid("version takes no arguments"))
	}
	fmt.Fprintln(rc.out, "line version", Version)
	return 0
}

// initKey and forgetKey are line.Init and line.Forget; tests swap them out.
var initKey, forgetKey = line.Init, line.Forget

func (rc *runCtx) init(args []string) int {
	fs := newFlags("init")
	forget := fs.Bool("forget", false, "delete the cached key")
	if err := parseArgs(fs, args); err != nil {
		return rc.fail(err)
	}
	if fs.NArg() > 0 {
		return rc.fail(invalid("init takes no arguments"))
	}
	if *forget {
		if err := forgetKey(); err != nil {
			return rc.fail(err)
		}
		return rc.emit(map[string]any{"ok": true, "forgotten": true})
	}
	if err := initKey(rc.notify); err != nil {
		return rc.fail(err)
	}
	return rc.emit(map[string]bool{"ok": true})
}

func (rc *runCtx) chats(args []string) int {
	fs := newFlags("chats")
	typ := fs.String("chat-type", "", "")
	fs.StringVar(typ, "type", "", "") // old name, kept as an alias
	limit := fs.Int("limit", 0, "")
	if err := parseArgs(fs, args); err != nil {
		return rc.fail(err)
	}
	if *typ != "" && !line.IsChatTypeFilter(*typ) {
		return rc.fail(invalidChatType(*typ))
	}
	if err := checkLimit("--limit", *limit); err != nil {
		return rc.fail(err)
	}
	var res []line.ChatSummary
	err := rc.withReader(func(r *line.Reader) (e error) {
		res, e = r.ListChats(line.ChatsOptions{Query: positional(fs), ChatType: *typ, Limit: *limit})
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	rc.noteLimit(len(res), line.EffectiveLimit(*limit, line.DefaultChatsLimit), "raise --limit (max 5000) or add a query")
	return rc.emitChats(res)
}

func (rc *runCtx) contacts(args []string) int {
	fs := newFlags("contacts")
	limit := fs.Int("limit", 0, "")
	members := fs.Bool("include-members", false, "")
	if err := parseArgs(fs, args); err != nil {
		return rc.fail(err)
	}
	if err := checkLimit("--limit", *limit); err != nil {
		return rc.fail(err)
	}
	var res *line.ContactList
	err := rc.withReader(func(r *line.Reader) (e error) {
		res, e = r.ListContacts(line.ContactsOptions{Query: positional(fs), Limit: *limit, IncludeMembers: *members})
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	rc.noteLimit(len(res.Contacts), line.EffectiveLimit(*limit, line.DefaultContactsLimit), "raise --limit (max 5000) or add a query")
	if n := res.MoreMembers; n > 0 && *members {
		rc.notify(fmt.Sprintf("note: %d more open-chat members match beyond --limit; raise --limit (max 5000) or add a query", n))
	} else if n > 0 {
		rc.notify(fmt.Sprintf("note: %d open-chat members also match; add --include-members to list them", n))
	}
	return rc.emitContacts(res.Contacts)
}

func parseRange(since, until string) (int64, int64, error) {
	st, err := parseTime(since, false)
	if err != nil {
		return 0, 0, err
	}
	ut, err := parseTime(until, true)
	if err != nil {
		return 0, 0, err
	}
	if st != 0 && ut != 0 && st > ut {
		return 0, 0, invalid("--since is later than --until")
	}
	return st, ut, nil
}

// Bounds of line watch, in seconds.
const (
	defaultWatchInterval = 2
	maxWatchInterval     = 3600
)

func (rc *runCtx) watch(args []string) int {
	fs := newFlags("watch")
	interval := fs.Int("interval", defaultWatchInterval, "")
	count := fs.Int("count", 0, "")
	timeout := fs.Int("timeout", 0, "")
	official := fs.Bool("include-official", false, "")
	if err := parseArgs(fs, args, "include-official"); err != nil {
		return rc.fail(err)
	}
	switch {
	case rc.outFile != "":
		return rc.fail(invalid("watch prints as messages arrive, so it cannot use --out; redirect stdout instead"))
	case *interval < 1 || *interval > maxWatchInterval:
		return rc.fail(invalid(fmt.Sprintf("--interval must be 1-%d seconds", maxWatchInterval)))
	case *count < 0:
		return rc.fail(invalid("--count must not be negative"))
	case *timeout < 0:
		return rc.fail(invalid("--timeout must not be negative"))
	}
	chat := positional(fs)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*timeout)*time.Second)
		defer cancel()
	}
	rc.startWatch()
	code, seen := 0, 0
	var watchErr error
	err := rc.withReader(func(r *line.Reader) error {
		opt := line.WatchOptions{IncludeOfficial: *official, Interval: time.Duration(*interval) * time.Second, Stop: ctx.Done()}
		if chat != "" {
			c, err := r.ResolveChatInfo(chat, false)
			if err != nil {
				return err
			}
			opt.ChatID = c.ChatID
		}
		rc.notify("note: watching for new messages; press Ctrl+C to stop")
		opt.Emit = func(h line.SearchHit) bool {
			seen++
			code = rc.emitSearch([]line.SearchHit{h})
			return code == 0 && (*count == 0 || seen < *count)
		}
		// Not returned: withReader would start over at the newest message and
		// skip what came in while the database could not be read.
		watchErr = r.Watch(opt)
		return nil
	})
	if err == nil {
		err = watchErr
	}
	if err != nil {
		return rc.fail(err)
	}
	return code
}

// startWatch sets the output of line watch: one line per message, each with
// sender_id, because no row ever sits beside another to show a shared name.
func (rc *runCtx) startWatch() { rc.jsonl, rc.ids = true, true }

func (rc *runCtx) read(args []string) int {
	fs := newFlags("read")
	since := fs.String("since", "", "")
	until := fs.String("until", "", "")
	limit := fs.Int("limit", 0, "")
	around := fs.String("around", "", "")
	offline := fs.Bool("offline", false, "")
	maxThreads := fs.Int("max-threads", 0, "")
	if err := parseArgs(fs, args); err != nil {
		return rc.fail(err)
	}
	rc.offline = *offline
	chat := positional(fs)
	*around = strings.TrimSpace(*around)
	if chat == "" && *around == "" {
		return rc.fail(invalid("read requires a chat_id or name, or --around <message_id>"))
	}
	if *around != "" && (*since != "" || *until != "") {
		return rc.fail(invalid("--around cannot be combined with --since or --until"))
	}
	if err := checkLimit("--limit", *limit); err != nil {
		return rc.fail(err)
	}
	if *maxThreads < 0 || *maxThreads > line.MaxReadThreads {
		return rc.fail(invalid(fmt.Sprintf("--max-threads must be between 1 and %d", line.MaxReadThreads)))
	}
	st, ut, err := parseRange(*since, *until)
	if err != nil {
		return rc.fail(err)
	}
	opt := line.ReadOptions{ChatArg: chat, SinceTs: st, UntilTs: ut, Limit: *limit, Around: *around}
	var res line.ReadResult
	var rep line.OnlineReport
	sess := line.NewSession() // kept if withReader runs again: LINE's server is not asked twice
	var resolved line.ChatSummary
	var fr *line.Freshness
	err = rc.withReader(func(r *line.Reader) (e error) {
		if chat == "" {
			// --around without a chat: everything below works on the message's chat.
			if chat, e = r.MessageChat(*around); e != nil {
				return
			}
		}
		if resolved, e = r.ResolveChatInfo(chat, false); e != nil {
			return
		}
		opt.ChatArg = resolved.ChatID
		if *around != "" {
			res.Messages, e = r.ReadHistory(opt)
		} else {
			r.UseServer(line.OnlineOptions{Enabled: !*offline, MaxThreads: *maxThreads, Notify: rc.notify, Session: sess})
			res, e = r.ReadChat(resolved, opt)
			rep = r.OnlineReport()
		}
		if e != nil {
			return
		}
		fr, e = r.ChatFreshness(resolved.ChatID)
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	rc.noteResolved(chat, resolved)
	hint := hintNone
	if *offline {
		hint = hintOffline
	}
	if *around != "" {
		rc.noteHistory(fr, res.Messages, 0, 0, hintRead)
		return rc.emitMessages(res.Messages, true)
	}
	rc.noteOnline(rep)
	rc.noteThreadsNotRead(resolved.ChatID, res, hint)
	thread := line.IsThreadChat(resolved.ChatID)
	switch {
	case !res.Complete && fr != nil && fr.StaleFor(ut):
		rc.noteStale(fr, ut, hint)
		if !thread { // a thread's stale note already says its replies are not stored
			rc.noteHistory(fr, res.Counted, st, ut, hint)
		}
	case !res.Complete || !thread:
		rc.noteHistory(fr, res.Counted, st, ut, hint)
	}
	next := "older: rerun with --until <first sent_at>"
	if st != 0 {
		next = "newer: rerun with --since <last sent_at>"
	}
	first, last := pageEnds(res.Counted, func(m line.Message) string { return m.SentAt })
	rc.notePage(len(res.Counted), opt.EffectiveLimit(), first, last, next)
	return rc.emitMessages(res.Messages, false)
}

func (rc *runCtx) search(args []string) int {
	fs := newFlags("search")
	chat := fs.String("chat", "", "")
	from := fs.String("from", "", "")
	typ := fs.String("type", "", "")
	ctype := fs.String("chat-type", "", "")
	since := fs.String("since", "", "")
	until := fs.String("until", "", "")
	limit := fs.Int("limit", 0, "")
	order := fs.String("order", "desc", "")
	if err := parseArgs(fs, args); err != nil {
		return rc.fail(err)
	}
	text := positional(fs)
	if text == "" && *typ == "" && *from == "" && *chat == "" && *ctype == "" && *since == "" && *until == "" {
		return rc.fail(invalid("search requires text or at least one filter"))
	}
	if *ctype != "" && !line.IsChatTypeFilter(*ctype) {
		return rc.fail(invalidChatType(*ctype))
	}
	if *chat != "" && *ctype != "" {
		return rc.fail(errChatAndType)
	}
	if *order != "asc" && *order != "desc" {
		return rc.fail(invalid("invalid --order: " + *order + " (valid: asc, desc)"))
	}
	if err := checkLimit("--limit", *limit); err != nil {
		return rc.fail(err)
	}
	st, ut, err := parseRange(*since, *until)
	if err != nil {
		return rc.fail(err)
	}
	if *typ == "link" {
		rc.notify("note: --type link is the older name of --type post (album and note posts); for messages with a URL or link preview use --type url")
	}
	var res []line.SearchHit
	var fr *line.Freshness
	err = rc.withReader(func(r *line.Reader) (e error) {
		res, e = r.Search(line.SearchOptions{
			Query: text, ChatID: *chat, From: *from, ContentType: *typ,
			ChatType: *ctype, SinceTs: st, UntilTs: ut, Limit: *limit, Order: *order,
		})
		if e == nil && *chat != "" {
			fr, e = r.ChatFreshness(*chat)
		}
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	rc.noteStale(fr, ut, hintRead)
	next := "older: rerun with --until <last sent_at>"
	if *order == "asc" {
		next = "newer: rerun with --since <last sent_at>"
	}
	first, last := pageEnds(res, func(h line.SearchHit) string { return h.SentAt })
	rc.notePage(len(res), line.EffectiveLimit(*limit, line.DefaultSearchLimit), first, last, next)
	return rc.emitSearch(res)
}

func (rc *runCtx) findFile(args []string) int {
	fs := newFlags("find-file")
	ext := fs.String("ext", "", "")
	chat := fs.String("chat", "", "")
	from := fs.String("from", "", "")
	ctype := fs.String("chat-type", "", "")
	since := fs.String("since", "", "")
	until := fs.String("until", "", "")
	limit := fs.Int("limit", 0, "")
	order := fs.String("order", "desc", "")
	noLocal := fs.Bool("no-local", false, "")
	if err := parseArgs(fs, args); err != nil {
		return rc.fail(err)
	}
	if *ctype != "" && !line.IsChatTypeFilter(*ctype) {
		return rc.fail(invalidChatType(*ctype))
	}
	if *chat != "" && *ctype != "" {
		return rc.fail(errChatAndType)
	}
	if *order != "asc" && *order != "desc" {
		return rc.fail(invalid("invalid --order: " + *order + " (valid: asc, desc)"))
	}
	if err := checkLimit("--limit", *limit); err != nil {
		return rc.fail(err)
	}
	st, ut, err := parseRange(*since, *until)
	if err != nil {
		return rc.fail(err)
	}
	var res []line.FileHit
	err = rc.withReader(func(r *line.Reader) (e error) {
		res, e = r.FindFile(line.FindFileOptions{
			Query: positional(fs), Ext: *ext, ChatID: *chat, ChatType: *ctype, From: *from,
			SinceTs: st, UntilTs: ut, Limit: *limit, Order: *order, LocalSearch: !*noLocal,
		})
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	next := "older: rerun with --until <last sent_at>"
	if *order == "asc" {
		next = "newer: rerun with --since <last sent_at>"
	}
	first, last := pageEnds(res, func(f line.FileHit) string { return f.SentAt })
	rc.notePage(len(res), line.EffectiveLimit(*limit, line.DefaultFindFileLimit), first, last, next)
	return rc.emitFiles(res)
}

func (rc *runCtx) preview(args []string) int {
	fs := newFlags("preview")
	since := fs.String("since", "", "")
	until := fs.String("until", "", "")
	limit := fs.Int("limit", 0, "")
	out := fs.String("out", "", "")
	if err := parseArgs(fs, args); err != nil {
		return rc.fail(err)
	}
	chat := positional(fs)
	if chat == "" {
		return rc.fail(invalid("preview requires a chat_id or name"))
	}
	if err := checkLimit("--limit", *limit); err != nil {
		return rc.fail(err)
	}
	st, ut, err := parseRange(*since, *until)
	if err != nil {
		return rc.fail(err)
	}
	var res []line.PreviewResult
	err = rc.withReader(func(r *line.Reader) (e error) {
		res, e = r.Preview(line.PreviewOptions{ChatArg: chat, SinceTs: st, UntilTs: ut, Limit: *limit, OutDir: *out})
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	first, last := pageEnds(res, func(p line.PreviewResult) string { return p.SentAt })
	rc.notePage(len(res), line.EffectiveLimit(*limit, line.DefaultPreviewLimit), first, last, pageNext(st))
	return rc.emitPreview(res)
}

// pageNext is the paging hint for lists that, like read, take the first N from
// --since or else the newest N, and print them oldest first.
func pageNext(since int64) string {
	if since != 0 {
		return "newer: rerun with --since <last sent_at>"
	}
	return "older: rerun with --until <first sent_at>"
}

func (rc *runCtx) download(args []string) int {
	fs := newFlags("download")
	id := fs.String("id", "", "")
	since := fs.String("since", "", "")
	until := fs.String("until", "", "")
	limit := fs.Int("limit", 0, "")
	out := fs.String("out", "", "")
	media := fs.Bool("media", false, "")
	if err := parseArgs(fs, args); err != nil {
		return rc.fail(err)
	}
	chat := positional(fs)
	if chat == "" && *id == "" {
		return rc.fail(invalid("download requires a chat_id or name, or --id"))
	}
	if err := checkLimit("--limit", *limit); err != nil {
		return rc.fail(err)
	}
	st, ut, err := parseRange(*since, *until)
	if err != nil {
		return rc.fail(err)
	}
	var res []line.DownloadResult
	err = rc.withReader(func(r *line.Reader) (e error) {
		res, e = r.Download(line.DownloadOptions{
			ChatArg: chat, ID: *id, SinceTs: st, UntilTs: ut, Limit: *limit, OutDir: *out, Media: *media, Notify: rc.notify,
		})
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	if *id == "" {
		first, last := pageEnds(res, func(d line.DownloadResult) string { return d.SentAt })
		rc.notePage(len(res), line.EffectiveLimit(*limit, line.DefaultDownloadLimit), first, last, pageNext(st))
	}
	return rc.emitDownload(res)
}

func (rc *runCtx) unread(args []string) int {
	fs := newFlags("unread")
	includeOfficial := fs.Bool("include-official", false, "")
	includeHidden := fs.Bool("include-hidden", false, "")
	limitChats := fs.Int("limit-chats", 0, "")
	perChat := fs.Int("per-chat-limit", 0, "")
	offline := fs.Bool("offline", false, "")
	if err := parseArgs(fs, args); err != nil {
		return rc.fail(err)
	}
	rc.offline = *offline
	if fs.NArg() > 0 {
		return rc.fail(invalid("unread takes no chat or other argument; use read <chat_id> for one chat, or pick it from the output by chat_id"))
	}
	if err := checkLimit("--limit-chats", *limitChats); err != nil {
		return rc.fail(err)
	}
	if err := checkLimit("--per-chat-limit", *perChat); err != nil {
		return rc.fail(err)
	}
	var res []line.UnreadChat
	var rep line.OnlineReport
	sess := line.NewSession()
	err := rc.withReader(func(r *line.Reader) (e error) {
		r.UseServer(line.OnlineOptions{Enabled: !*offline, Notify: rc.notify, Session: sess})
		res, e = r.Unread(line.UnreadOptions{LimitChats: *limitChats, IncludeOfficial: *includeOfficial, IncludeHidden: *includeHidden, PerChatLimit: *perChat, LineStopped: rc.dataAsOf != ""})
		rep = r.OnlineReport()
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	rc.noteOnline(rep)
	hint := hintNone
	if *offline {
		hint = hintOffline
	}
	rc.noteLimit(len(res), line.EffectiveLimit(*limitChats, line.DefaultUnreadChats), "raise --limit-chats")
	cut := 0
	for _, u := range res {
		if u.MoreUnread {
			cut++
		}
		if u.Stale {
			rc.notify(staleNote(u.ChatID, u.LastActivityAt, u.NewestStoredAt, hint))
		}
	}
	if cut > 0 {
		rc.sum.LimitReached = true
		rc.sum.Next = strings.TrimPrefix(rc.sum.Next+"; raise --per-chat-limit or page older with read <chat_id> --until <first sent_at> (available_count > messages)", "; ")
		rc.notify(fmt.Sprintf("note: %d chat(s) have more unread messages than --per-chat-limit %d; raise it or page older with read <chat_id> --until <first sent_at>", cut, line.EffectiveLimit(*perChat, line.DefaultUnreadPerChat)))
	}
	return rc.emitUnread(res)
}

// errChatAndType refuses --chat with --chat-type: one chat already has one type.
var errChatAndType = invalid("--chat and --chat-type cannot be combined; --chat already selects one chat (its chat_type is in the output)")

// maxInputBytes bounds what send reads from --stdin or --message-file; the
// rune limit is checked after normalization.
const maxInputBytes = 1 << 20

// actionFlags parses what react and unsend share and refuses a call that does
// not say whether to act; more names flags the command adds.
func actionFlags(fs *flag.FlagSet, cmd string, args []string, more ...string) (to, id *string, dry *bool, err error) {
	to = fs.String("to", "", "")
	id = fs.String("id", "", "")
	dry = fs.Bool("dry-run", false, "")
	yes := fs.Bool("yes", false, "")
	y := fs.Bool("y", false, "")
	if err = parseArgs(fs, args, append([]string{"dry-run", "yes", "y"}, more...)...); err != nil {
		return
	}
	switch {
	case fs.NArg() > 0:
		err = invalid(fmt.Sprintf("unexpected argument(s) %q", fs.Args()))
	case *to == "":
		err = invalid(cmd + " requires --to <chat_id|exact-name>")
	case *id == "":
		err = invalid(cmd + " requires --id <message id>")
	default:
		if err = line.CheckMessageID("--id", *id); err == nil && !*dry && !(*yes || *y) {
			err = invalid("refusing to " + cmd + " without confirmation; use --dry-run to preview or -y to do it")
		}
	}
	return
}

func (rc *runCtx) react(args []string) int {
	fs := newFlags("react")
	reaction := fs.String("reaction", "", "")
	remove := fs.Bool("remove", false, "")
	to, id, dry, err := actionFlags(fs, "react", args, "remove")
	if err != nil {
		return rc.fail(err)
	}
	if (*reaction == "") == !*remove {
		return rc.fail(invalid("react needs either --reaction <name> or --remove"))
	}
	var plan *line.ActionPlan
	err = rc.withReader(func(r *line.Reader) (e error) {
		plan, e = r.React(line.ReactOptions{To: *to, ID: *id, Reaction: *reaction, Remove: *remove, DryRun: *dry, Notify: rc.notify})
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	return rc.emitAction(plan)
}

func (rc *runCtx) unsend(args []string) int {
	to, id, dry, err := actionFlags(newFlags("unsend"), "unsend", args)
	if err != nil {
		return rc.fail(err)
	}
	var plan *line.ActionPlan
	err = rc.withReader(func(r *line.Reader) (e error) {
		plan, e = r.Unsend(line.UnsendOptions{To: *to, ID: *id, DryRun: *dry, Notify: rc.notify})
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	return rc.emitAction(plan)
}

func (rc *runCtx) send(args []string) int {
	fs := newFlags("send")
	to := fs.String("to", "", "")
	text := fs.String("message", "", "")
	file := fs.String("message-file", "", "")
	stdin := fs.Bool("stdin", false, "")
	dry := fs.Bool("dry-run", false, "")
	yes := fs.Bool("yes", false, "")
	y := fs.Bool("y", false, "")
	allowDup := fs.Bool("allow-duplicate", false, "")
	replyTo := fs.String("reply-to", "", "")
	var files, mentions pathList
	fs.Var(&files, "file", "")
	fs.Var(&mentions, "mention", "")
	mentionAll := fs.Bool("mention-all", false, "")
	asFile := fs.Bool("as-file", false, "")
	if err := parseArgs(fs, args, "stdin", "dry-run", "yes", "y", "allow-duplicate", "as-file", "mention-all"); err != nil {
		return rc.fail(err)
	}
	if fs.NArg() > 0 {
		if len(files) > 0 {
			return rc.fail(invalid(fmt.Sprintf("unexpected argument(s) %q; give each file with its own --file, and quote paths and names with spaces", fs.Args())))
		}
		return rc.fail(invalid(fmt.Sprintf("unexpected argument(s) %q; quote multi-word values (the --to name or the --message text), or use --message-file or --stdin", fs.Args())))
	}
	if *to == "" {
		return rc.fail(invalid("send requires --to <chat_id|exact-name>"))
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	sources := 0
	for _, n := range []string{"message", "message-file", "stdin"} {
		if given[n] {
			sources++
		}
	}
	if sources == 0 && len(files) == 0 {
		return rc.fail(invalid("send requires --message, --message-file, --stdin or --file"))
	}
	if sources > 1 {
		return rc.fail(invalid("give only one of --message, --message-file and --stdin"))
	}
	if *asFile && len(files) == 0 {
		return rc.fail(invalid("--as-file applies to --file; give the files to send with --file"))
	}
	if len(mentions) > 0 || *mentionAll {
		if len(files) > 0 {
			return rc.fail(invalid("--mention and --mention-all apply to a text message; send the files in a separate command"))
		}
		if slices.Contains(mentions, "") {
			return rc.fail(invalid("--mention needs a member id or exact name"))
		}
	}
	if given["reply-to"] {
		if err := line.CheckReplyID(*replyTo); err != nil {
			return rc.fail(err)
		}
	}
	sendFiles := func(text string, normalized bool) int {
		paths, err := expandFiles(files)
		if err != nil {
			return rc.fail(err)
		}
		prepared, err := line.PrepareFiles(paths, *asFile, *allowDup, rc.notify)
		if err != nil {
			return rc.fail(err)
		}
		return rc.sendFiles(line.SendFilesOptions{
			To: *to, Files: prepared, DryRun: *dry, AllowDuplicate: *allowDup, ReplyTo: *replyTo,
			Text: text, Normalized: normalized, Notify: rc.notify,
		})
	}
	if len(files) > 0 && sources == 0 {
		if !*dry && !(*yes || *y) {
			return rc.fail(invalid("refusing to send without confirmation; use --dry-run to preview or -y to send"))
		}
		return sendFiles("", false)
	}
	raw := *text
	switch {
	case *stdin:
		b, err := io.ReadAll(io.LimitReader(os.Stdin, maxInputBytes+1))
		if err != nil {
			return rc.fail(invalid("could not read --stdin"))
		}
		raw = string(b)
	case given["message-file"]:
		b, err := readMessageFile(*file)
		if err != nil {
			return rc.fail(err)
		}
		raw = string(b)
	}
	if len(raw) > maxInputBytes {
		return rc.fail(invalid("message input is larger than 1 MiB"))
	}
	msg, normalized, err := line.NormalizeSendText(raw)
	if err != nil {
		return rc.fail(err)
	}
	if given["message"] && os.Getenv("MSYSTEM") != "" && driveRooted(msg) {
		rc.notify("warning: the --message text starts with a drive path; Git Bash (MSYS) rewrites an argument starting with '/' into a Windows path, so check text in the output, and pass such text with --stdin or --message-file, or set MSYS_NO_PATHCONV=1")
	}
	if strings.ContainsRune(msg, utf8.RuneError) {
		rc.notify("warning: the text contains U+FFFD replacement characters, so it may have been mis-encoded before line-cli read it")
	} else if *stdin && strings.HasPrefix(raw, "\ufeff") && strings.Contains(msg, "??") {
		rc.notify("warning: the text starts with a BOM and contains '??'; Windows PowerShell 5.1 may have replaced non-ASCII characters. Set $OutputEncoding to UTF-8 or use --message-file")
	}
	if !*dry && !(*yes || *y) {
		return rc.fail(invalid("refusing to send without confirmation; use --dry-run to preview or -y to send"))
	}
	if len(files) > 0 {
		return sendFiles(msg, normalized)
	}
	var plan *line.SendPlan
	err = rc.withReader(func(r *line.Reader) (e error) {
		plan, e = r.Send(line.SendOptions{
			To: *to, Text: msg, DryRun: *dry, AllowDuplicate: *allowDup, ReplyTo: *replyTo,
			Mentions: mentions, MentionAll: *mentionAll, Normalized: normalized, Notify: rc.notify,
		})
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	return rc.emitSend(plan)
}

// sendFiles sends files (and the text before them) and prints the plan. A
// send that stopped at a failed file still prints what was sent, with the
// error code and message at the top level, and exits 1.
func (rc *runCtx) sendFiles(opt line.SendFilesOptions) int {
	var plan *line.SendFilesPlan
	err := rc.withReader(func(r *line.Reader) (e error) {
		plan, e = r.SendFiles(opt)
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	code := rc.emitSendFiles(plan)
	if plan.Error != "" {
		return 1
	}
	return code
}

// expandFiles expands wildcards in --file paths that do not exist as given,
// since PowerShell and cmd pass them to programs unexpanded.
func expandFiles(files []string) ([]string, error) {
	var out []string
	for _, f := range files {
		if _, err := os.Stat(f); err == nil || !strings.ContainsAny(f, "*?[") {
			out = append(out, f)
			continue
		}
		m, err := filepath.Glob(f)
		if err != nil {
			return nil, invalid("bad wildcard in --file " + strconv.Quote(f) + ": " + err.Error())
		}
		if len(m) == 0 {
			return nil, invalid("no file matches --file " + strconv.Quote(f))
		}
		for _, p := range m {
			if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

// driveRooted reports text starting like a Windows path: a drive letter, a
// colon and a slash or backslash.
func driveRooted(s string) bool {
	return len(s) >= 3 && 'A' <= s[0]&^0x20 && s[0]&^0x20 <= 'Z' && s[1] == ':' && (s[2] == '/' || s[2] == '\\')
}

func readMessageFile(path string) ([]byte, error) {
	if path == "" {
		return nil, invalid("--message-file needs a path")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, invalid("cannot read --message-file: " + err.Error())
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxInputBytes+1))
	if err != nil {
		return nil, invalid("cannot read --message-file: " + err.Error())
	}
	if len(b) > maxInputBytes {
		return nil, invalid("message input is larger than 1 MiB")
	}
	return messageFileText(b)
}

const saveAsUTF8 = "save it as UTF-8, e.g. with Set-Content -Encoding UTF8 or Out-File -Encoding utf8"

// messageFileText returns a message file as UTF-8. Windows PowerShell 5.1
// writes UTF-16 with a BOM ('>' and Out-File), which is converted; other
// non-UTF-8 files (Set-Content's ANSI code page) are refused.
func messageFileText(b []byte) ([]byte, error) {
	var order binary.ByteOrder
	switch {
	case bytes.HasPrefix(b, []byte{0xff, 0xfe}):
		order = binary.LittleEndian
	case bytes.HasPrefix(b, []byte{0xfe, 0xff}):
		order = binary.BigEndian
	case utf8.Valid(b):
		return b, nil
	default:
		return nil, invalid("--message-file is not valid UTF-8 (perhaps saved in the ANSI code page); " + saveAsUTF8)
	}
	bad := invalid("--message-file starts with a UTF-16 byte order mark but is not valid UTF-16; " + saveAsUTF8)
	if len(b)%2 != 0 {
		return nil, bad
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = order.Uint16(b[2*i:])
	}
	for i := 0; i < len(units); i++ {
		if !utf16.IsSurrogate(rune(units[i])) {
			continue
		}
		if i+1 == len(units) || utf16.DecodeRune(rune(units[i]), rune(units[i+1])) == utf8.RuneError {
			return nil, bad
		}
		i++
	}
	return []byte(string(utf16.Decode(units))), nil
}
