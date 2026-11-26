package cli

// line notes and line albums: a group's notes (記事本) and albums (相簿),
// which LINE PC never stores, read from and written to LINE's server by the
// full build (internal/line/timeline_*.go). Each command has one flag set; a
// first argument that names a subcommand picks it, and every other flag a
// subcommand does not take is refused.

import (
	"flag"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/WellWells/line-cli/internal/line"
)

// The subcommands of notes and albums; a group named like one is given by
// its chat_id.
var (
	notesSubs  = []string{"add", "comment", "delete", "download"}
	albumsSubs = []string{"add", "delete", "download"}
)

// timelineSub splits the positional arguments: a first word that names a
// subcommand is the subcommand, the rest is the chat.
func timelineSub(args []string, subs []string) (sub, chat string) {
	if len(args) > 0 && slices.Contains(subs, args[0]) {
		return args[0], strings.TrimSpace(strings.Join(args[1:], " "))
	}
	return "", strings.TrimSpace(strings.Join(args, " "))
}

// outputFlags are the global flags every subcommand takes.
var outputFlags = map[string]bool{"text": true, "compact": true, "jsonl": true, "ids": true, "out": true}

func flagLabel(name string) string {
	if len(name) == 1 {
		return "-" + name
	}
	return "--" + name
}

// onlyFlags refuses any flag given that the subcommand (what) does not take.
func onlyFlags(fs *flag.FlagSet, what string, allowed ...string) error {
	var bad []string
	fs.Visit(func(f *flag.Flag) {
		if !outputFlags[f.Name] && !slices.Contains(allowed, f.Name) {
			bad = append(bad, flagLabel(f.Name))
		}
	})
	if len(bad) > 0 {
		return invalid(strings.Join(bad, ", ") + " does not apply to " + what + " (see 'line help " + strings.Fields(what)[0] + " --json')")
	}
	return nil
}

// writeFlags are what every notes and albums write takes.
type writeFlags struct {
	to                    *string
	dry, yes, y, allowDup *bool
}

func newWriteFlags(fs *flag.FlagSet) writeFlags {
	return writeFlags{
		to:       fs.String("to", "", ""),
		dry:      fs.Bool("dry-run", false, ""),
		yes:      fs.Bool("yes", false, ""),
		y:        fs.Bool("y", false, ""),
		allowDup: fs.Bool("allow-duplicate", false, ""),
	}
}

// strictBools are the bool flags of a notes or albums command that take no value.
var strictBools = []string{"dry-run", "yes", "y", "allow-duplicate", "stdin"}

// start checks what every write needs: no positional words, --to, and (last,
// like send) --dry-run or -y.
func (w writeFlags) start(what, rest string) error {
	switch {
	case rest != "":
		return invalid(fmt.Sprintf("unexpected argument(s) %q; give the group with --to, and quote multi-word values", strings.Fields(rest)))
	case strings.TrimSpace(*w.to) == "":
		return invalid(what + " requires --to <chat_id|exact-name>")
	}
	return nil
}

func (w writeFlags) confirm(verb string) error {
	if !*w.dry && !(*w.yes || *w.y) {
		return invalid("refusing to " + verb + " without confirmation; use --dry-run to preview or -y to do it")
	}
	return nil
}

// limitOf is v as the command uses it: def when 0, at most max.
func limitOf(v, def, max int) int {
	if v <= 0 {
		return def
	}
	return min(v, max)
}

func givenFlags(fs *flag.FlagSet) map[string]bool {
	g := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { g[f.Name] = true })
	return g
}

func (rc *runCtx) notes(args []string) int {
	fs := newFlags("notes")
	limit := fs.Int("limit", 0, "")
	comments := fs.Int("comments", 0, "")
	since := fs.String("since", "", "")
	until := fs.String("until", "", "")
	cursor := fs.String("cursor", "", "")
	id := fs.String("id", "", "")
	commentID := fs.String("comment", "", "")
	text := fs.String("message", "", "")
	file := fs.String("message-file", "", "")
	stdin := fs.Bool("stdin", false, "")
	w := newWriteFlags(fs)
	if err := parseArgs(fs, args, strictBools...); err != nil {
		return rc.fail(err)
	}
	sub, chat := timelineSub(fs.Args(), notesSubs)
	g := givenFlags(fs)
	switch sub {
	case "":
		return rc.notesRead(fs, chat, g, notesReadArgs{*limit, *comments, *since, *until, *cursor, *id})
	case "download":
		if err := onlyFlags(fs, "notes download", "id"); err != nil {
			return rc.fail(err)
		}
		if chat == "" {
			return rc.fail(invalid("notes download requires a group: a chat_id or name"))
		}
		if err := needID("notes download", "--id", *id, "note"); err != nil {
			return rc.fail(err)
		}
		var res []line.DownloadResult
		err := rc.withReader(func(r *line.Reader) (e error) {
			res, e = r.DownloadNoteMedia(line.TimelineDownloadOptions{Chat: chat, ID: *id, Notify: rc.notify})
			return
		})
		if err != nil {
			return rc.fail(err)
		}
		return rc.emitDownload(res)
	}
	what := "notes " + sub
	allowed := []string{"to", "dry-run", "yes", "y"}
	switch sub {
	case "add":
		allowed = append(allowed, "message", "message-file", "stdin", "allow-duplicate")
	case "comment":
		allowed = append(allowed, "id", "message", "message-file", "stdin", "allow-duplicate")
	case "delete":
		allowed = append(allowed, "id", "comment")
	}
	if err := onlyFlags(fs, what, allowed...); err != nil {
		return rc.fail(err)
	}
	if err := w.start(what, chat); err != nil {
		return rc.fail(err)
	}
	opt := line.NoteWriteOptions{To: *w.to, ID: *id, CommentID: *commentID, DryRun: *w.dry, AllowDuplicate: *w.allowDup, Notify: rc.notify}
	if sub != "add" {
		if err := needID(what, "--id", *id, "note"); err != nil {
			return rc.fail(err)
		}
	}
	verb := "delete the note"
	switch sub {
	case "add", "comment":
		sources := 0
		for _, n := range []string{"message", "message-file", "stdin"} {
			if g[n] {
				sources++
			}
		}
		switch {
		case sources == 0:
			return rc.fail(invalid(what + " requires the text: --message, --message-file or --stdin"))
		case sources > 1:
			return rc.fail(invalid("give only one of --message, --message-file and --stdin"))
		}
		msg, _, err := rc.messageText(g, *text, *file, *stdin)
		if err != nil {
			return rc.fail(err)
		}
		opt.Text = msg
		verb = map[string]string{"add": "add the note", "comment": "comment"}[sub]
	case "delete":
		if g["comment"] {
			if err := needID(what, "--comment", *commentID, "comment"); err != nil {
				return rc.fail(err)
			}
			verb = "delete the comment"
		}
	}
	if err := w.confirm(verb); err != nil {
		return rc.fail(err)
	}
	var plan *line.TimelinePlan
	err := rc.withReader(func(r *line.Reader) (e error) {
		switch {
		case sub == "add":
			plan, e = r.AddNote(opt)
		case sub == "comment":
			plan, e = r.CommentNote(opt)
		case g["comment"]:
			plan, e = r.DeleteComment(opt)
		default:
			plan, e = r.DeleteNote(opt)
		}
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	return rc.emitTimeline(plan)
}

// needID checks a notes or albums id flag that the subcommand needs.
func needID(what, flagName, id, kind string) error {
	if id == "" {
		return invalid(what + " requires " + flagName + " <" + kind + " id>")
	}
	return line.CheckTimelineID(flagName, id)
}

// notesReadArgs are the flags of notes <chat>.
type notesReadArgs struct {
	limit, comments          int
	since, until, cursor, id string
}

// notesRead is notes <chat>: the list, or one note with --id.
func (rc *runCtx) notesRead(fs *flag.FlagSet, chat string, g map[string]bool, a notesReadArgs) int {
	limit, comments, since, until, id := a.limit, a.comments, a.since, a.until, a.id
	if err := onlyFlags(fs, "notes <chat>", "limit", "comments", "since", "until", "cursor", "id"); err != nil {
		return rc.fail(err)
	}
	if chat == "" {
		return rc.fail(invalid("notes requires a group: a chat_id or name (or a subcommand: " + strings.Join(notesSubs, ", ") + ")"))
	}
	if err := checkLimit("--limit", limit); err != nil {
		return rc.fail(err)
	}
	if err := checkLimit("--comments", comments); err != nil {
		return rc.fail(err)
	}
	if id != "" {
		if g["limit"] || g["since"] || g["until"] || g["cursor"] {
			return rc.fail(invalid("--id reads one note; --limit, --since, --until and --cursor apply to the list of notes"))
		}
		if err := line.CheckTimelineID("--id", id); err != nil {
			return rc.fail(err)
		}
		if g["comments"] && comments == 0 {
			comments = -1 // --comments 0: none
		}
		var n *line.Note
		err := rc.withReader(func(r *line.Reader) (e error) {
			n, e = r.GetNote(line.NotesOptions{Chat: chat, ID: id, Comments: comments, ShowIDs: rc.ids, Notify: rc.notify})
			return
		})
		if err != nil {
			return rc.fail(err)
		}
		if want := limitOf(comments, line.DefaultNoteComments, line.MaxNoteComments); comments >= 0 && int64(len(n.Comments)) < n.CommentCount && len(n.Comments) >= want {
			rc.notify(fmt.Sprintf("note: the note has %d comments and %d are shown (the newest); raise --comments (max %d)", n.CommentCount, len(n.Comments), line.MaxNoteComments))
		}
		return rc.emitNote(n)
	}
	st, ut, err := parseRange(since, until)
	if err != nil {
		return rc.fail(err)
	}
	if a.cursor != "" {
		if _, _, err := line.SplitNoteCursor(a.cursor); err != nil {
			return rc.fail(err)
		}
	}
	var res []line.Note
	err = rc.withReader(func(r *line.Reader) (e error) {
		res, e = r.ListNotes(line.NotesOptions{Chat: chat, Limit: limit, Comments: comments, SinceTs: st, UntilTs: ut, Cursor: a.cursor, ShowIDs: rc.ids, Notify: rc.notify})
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	if n, lim := len(res), limitOf(limit, line.DefaultNotesLimit, line.MaxNotesLimit); n >= lim {
		// The list is the newest created first, so the last is the oldest.
		oldest := res[n-1].CreatedAt
		next := "older: rerun with --until " + oldest
		if res[0].CreatedAt == oldest && lim < line.MaxNotesLimit {
			// --until includes the whole boundary second. At the same limit
			// the next run would return this page again.
			next += " --limit " + strconv.Itoa(min(2*lim, line.MaxNotesLimit))
		}
		if a.cursor != "" {
			next += " --cursor " + a.cursor
		}
		next += " (notes created in that second repeat; dedupe by id)"
		if res[0].CreatedAt == oldest && lim == line.MaxNotesLimit {
			next = "all notes on this page were created in " + oldest + "; at the maximum --limit, --until cannot show whether that second contains more"
		}
		rc.sum.FirstSentAt, rc.sum.LastSentAt = res[0].CreatedAt, oldest
		rc.noteLimit(n, lim, next)
	}
	return rc.emitNotes(res)
}

func (rc *runCtx) albums(args []string) int {
	fs := newFlags("albums")
	limit := fs.Int("limit", 0, "")
	id := fs.String("id", "", "")
	title := fs.String("title", "", "")
	var photos, files pathList
	fs.Var(&photos, "photo", "")
	fs.Var(&files, "file", "")
	w := newWriteFlags(fs)
	if err := parseArgs(fs, args, strictBools...); err != nil {
		return rc.fail(err)
	}
	sub, chat := timelineSub(fs.Args(), albumsSubs)
	g := givenFlags(fs)
	checkPhotos := func() error {
		for _, p := range photos {
			if err := line.CheckTimelineID("--photo", p); err != nil {
				return err
			}
		}
		return nil
	}
	switch sub {
	case "":
		return rc.albumsRead(fs, chat, g, *limit, *id)
	case "download":
		if err := onlyFlags(fs, "albums download", "id", "photo", "limit"); err != nil {
			return rc.fail(err)
		}
		if chat == "" {
			return rc.fail(invalid("albums download requires a group: a chat_id or name"))
		}
		if err := needID("albums download", "--id", *id, "album"); err != nil {
			return rc.fail(err)
		}
		if err := checkLimit("--limit", *limit); err != nil {
			return rc.fail(err)
		}
		if len(photos) > 0 && g["limit"] {
			return rc.fail(invalid("--photo names the photos to download; --limit applies only without it"))
		}
		if err := checkPhotos(); err != nil {
			return rc.fail(err)
		}
		var res []line.DownloadResult
		err := rc.withReader(func(r *line.Reader) (e error) {
			res, e = r.DownloadAlbumPhotos(line.TimelineDownloadOptions{Chat: chat, ID: *id, Photos: photos, Limit: *limit, Notify: rc.notify})
			return
		})
		if err != nil {
			return rc.fail(err)
		}
		if len(photos) == 0 {
			rc.noteLimit(len(res), limitOf(*limit, line.DefaultAlbumPhotosLimit, line.MaxAlbumPhotosLimit), fmt.Sprintf("raise --limit (max %d), or name photos with --photo", line.MaxAlbumPhotosLimit))
		}
		return rc.emitDownload(res)
	}
	what := "albums " + sub
	allowed := []string{"to", "id", "dry-run", "yes", "y"}
	if sub == "add" {
		allowed = append(allowed, "title", "file", "allow-duplicate")
	} else {
		allowed = append(allowed, "photo")
	}
	if err := onlyFlags(fs, what, allowed...); err != nil {
		return rc.fail(err)
	}
	if err := w.start(what, chat); err != nil {
		return rc.fail(err)
	}
	opt := line.AlbumWriteOptions{To: *w.to, ID: *id, Title: *title, Photos: photos, DryRun: *w.dry, AllowDuplicate: *w.allowDup, Notify: rc.notify}
	verb := "delete the album"
	if sub == "add" {
		switch {
		case g["title"] && g["id"]:
			return rc.fail(invalid("give --title for a new album or --id for an existing one, not both"))
		case g["title"]:
			if err := line.CheckAlbumTitle(*title); err != nil {
				return rc.fail(err)
			}
			verb = "create the album"
		case g["id"]:
			if err := needID(what, "--id", *id, "album"); err != nil {
				return rc.fail(err)
			}
			if len(files) == 0 {
				return rc.fail(invalid("albums add --id needs the photos to add: --file PATH (repeat for more)"))
			}
			verb = "add the photos"
		default:
			return rc.fail(invalid("albums add requires --title <title> for a new album or --id <album id>"))
		}
		paths, err := expandFiles(files)
		if err != nil {
			return rc.fail(err)
		}
		if len(paths) > line.MaxAlbumUploadFiles {
			return rc.fail(invalid("too many photos: " + strconv.Itoa(len(paths)) + " (limit " + strconv.Itoa(line.MaxAlbumUploadFiles) + " at a time); add them in several runs"))
		}
		opt.Files = paths
	} else {
		if err := needID(what, "--id", *id, "album"); err != nil {
			return rc.fail(err)
		}
		if err := checkPhotos(); err != nil {
			return rc.fail(err)
		}
		if len(photos) > 0 {
			verb = "delete the photos"
		}
	}
	if err := w.confirm(verb); err != nil {
		return rc.fail(err)
	}
	var plan *line.TimelinePlan
	err := rc.withReader(func(r *line.Reader) (e error) {
		switch {
		case sub == "add" && g["title"]:
			plan, e = r.CreateAlbum(opt)
		case sub == "add":
			plan, e = r.AddAlbumPhotos(opt)
		case len(photos) > 0:
			plan, e = r.DeletePhotos(opt)
		default:
			plan, e = r.DeleteAlbum(opt)
		}
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	return rc.emitTimeline(plan)
}

// albumsRead is albums <chat>: the albums, or one album's photos with --id.
func (rc *runCtx) albumsRead(fs *flag.FlagSet, chat string, g map[string]bool, limit int, id string) int {
	if err := onlyFlags(fs, "albums <chat>", "id", "limit"); err != nil {
		return rc.fail(err)
	}
	if chat == "" {
		return rc.fail(invalid("albums requires a group: a chat_id or name (or a subcommand: " + strings.Join(albumsSubs, ", ") + ")"))
	}
	if err := checkLimit("--limit", limit); err != nil {
		return rc.fail(err)
	}
	if id == "" {
		if g["limit"] {
			return rc.fail(invalid("--limit applies to the photos of one album; add --id <album id>"))
		}
		var res []line.GroupAlbum
		err := rc.withReader(func(r *line.Reader) (e error) {
			res, e = r.ListAlbums(line.AlbumsOptions{Chat: chat, ShowIDs: rc.ids, Notify: rc.notify})
			return
		})
		if err != nil {
			return rc.fail(err)
		}
		return rc.emitAlbums(res)
	}
	if err := line.CheckTimelineID("--id", id); err != nil {
		return rc.fail(err)
	}
	var res []line.AlbumPhoto
	err := rc.withReader(func(r *line.Reader) (e error) {
		res, e = r.ListAlbumPhotos(line.AlbumsOptions{Chat: chat, ID: id, Limit: limit, ShowIDs: rc.ids, Notify: rc.notify})
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	rc.noteLimit(len(res), limitOf(limit, line.DefaultAlbumPhotosLimit, line.MaxAlbumPhotosLimit), fmt.Sprintf("raise --limit (max %d)", line.MaxAlbumPhotosLimit))
	return rc.emitAlbumPhotos(res)
}

// --- output ---

func meFlag(fromMe bool) string {
	if fromMe {
		return "me"
	}
	return ""
}

// noteSummary is a note's text column: its text, then its link's title and
// URL when the text does not have them, then how many photos it has.
func noteSummary(n line.Note) string {
	s := n.Text
	if l := n.Link; l != nil && l.URL != "" && !strings.Contains(s, l.URL) {
		s = strings.TrimSpace(s + " " + strings.TrimSpace(l.Title+" "+l.URL))
	}
	if k := len(n.Media); k > 0 {
		s = strings.TrimSpace(s + " [" + strconv.Itoa(k) + " photos/videos]")
	}
	return s
}

// printNote prints a note and then its comments, one per line.
func (rc *runCtx) printNote(n line.Note) {
	fmt.Fprintln(rc.out, cols(n.CreatedAt, n.ID, n.Author, meFlag(n.FromMe), n.Kind, "comments="+strconv.FormatInt(n.CommentCount, 10), noteSummary(n)))
	for _, c := range n.Comments {
		fmt.Fprintln(rc.out, cols(c.CreatedAt, c.ID, c.Author, meFlag(c.FromMe), "comment", "", c.Text))
	}
}

func (rc *runCtx) emitNotes(v []line.Note) int {
	if !rc.text {
		return rc.emit(list(v))
	}
	for _, n := range v {
		rc.printNote(n)
	}
	return 0
}

func (rc *runCtx) emitNote(n *line.Note) int {
	if !rc.text {
		return rc.emit(n)
	}
	rc.printNote(*n)
	return 0
}

func (rc *runCtx) emitAlbums(v []line.GroupAlbum) int {
	if !rc.text {
		return rc.emit(list(v))
	}
	for _, a := range v {
		fmt.Fprintln(rc.out, cols(a.ID, a.Title, "photos="+strconv.FormatInt(a.PhotoCount, 10), a.CreatedAt, a.UpdatedAt))
	}
	return 0
}

func (rc *runCtx) emitAlbumPhotos(v []line.AlbumPhoto) int {
	if !rc.text {
		return rc.emit(list(v))
	}
	for _, p := range v {
		size := ""
		if p.Width > 0 && p.Height > 0 {
			size = fmt.Sprintf("%dx%d", p.Width, p.Height)
		}
		fmt.Fprintln(rc.out, cols(p.CreatedAt, p.ID, p.Uploader, meFlag(p.FromMe), p.Type, size))
	}
	return 0
}

// emitTimeline prints what a notes or albums write did, or would do. A write
// that stopped part way (an album made, but not every photo added) still
// prints the plan, with error and message at the top level, and exits 1.
func (rc *runCtx) emitTimeline(p *line.TimelinePlan) int {
	code := 0
	if !rc.text {
		code = rc.emit(p)
	} else {
		status := "DONE"
		switch {
		case p.DryRun:
			status = "DRY-RUN (nothing done)"
		case !p.Sent:
			status = "NOT DONE"
		}
		fmt.Fprintf(rc.out, "%s\naction: %s\ngroup: %s (%s)\n", status, p.Action, p.ToName, p.To)
		for _, f := range [][2]string{{"note", p.PostID}, {"comment", p.CommentID}, {"album", p.AlbumID}, {"title", p.Title}, {"text", p.Text}, {"photos", strings.Join(p.Photos, ", ")}} {
			if f[1] != "" {
				fmt.Fprintf(rc.out, "%s: %s\n", f[0], f[1])
			}
		}
		for _, f := range p.Files {
			gps := ""
			if f.GPSRemoved {
				gps = ", location removed"
			}
			fmt.Fprintf(rc.out, "%s %s (%d bytes%s): %s\n", f.Kind, f.Name, f.Bytes, gps, f.Status)
			if f.Error != "" {
				fmt.Fprintf(rc.out, "  error: %s\n", f.Error)
			}
		}
		if p.Deleted != nil {
			fmt.Fprintf(rc.out, "deleted: %d\n", *p.Deleted)
		}
		if p.URL != "" {
			fmt.Fprintf(rc.out, "url: %s\n", p.URL)
		}
		if p.Error != "" {
			fmt.Fprintf(rc.out, "stopped: %s: %s\n", p.Error, p.ErrorMessage)
		}
	}
	if p.Error != "" {
		return rc.partialFailure()
	}
	return code
}
