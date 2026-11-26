package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/WellWells/line-cli/internal/line"
)

// notes and albums: argument checks, help and --text, all without LINE.

const tlArgGroup = "c0000000000000000000000000000000e"

// noDB makes any command that opens the database fail the test.
func noDB(t *testing.T) {
	t.Helper()
	t.Setenv("LOCALAPPDATA", t.TempDir())
	old := openDB
	openDB = func(func(string), bool) (*line.DB, error) {
		t.Fatal("the command opened the database")
		return nil, nil
	}
	t.Cleanup(func() { openDB = old })
}

func TestTimelineArgErrorsBeforeOpeningDB(t *testing.T) {
	noDB(t)
	g := tlArgGroup
	noMatch := filepath.Join(t.TempDir(), "none-*.jpg")
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"notes"}, "notes requires a group"},
		{[]string{"notes", g, "--to", g}, "--to does not apply to notes <chat>"},
		{[]string{"notes", g, "--dry-run"}, "--dry-run does not apply to notes <chat>"},
		{[]string{"notes", g, "--limit", "-1"}, "--limit must not be negative"},
		{[]string{"notes", g, "--comments", "-2"}, "--comments must not be negative"},
		{[]string{"notes", g, "--since", "2026-10-02", "--until", "2026-10-01"}, "later than --until"},
		{[]string{"notes", g, "--since", "yesterday"}, "invalid time"},
		{[]string{"notes", g, "--id", "p1", "--limit", "5"}, "--id reads one note"},
		{[]string{"notes", g, "--id", "p 1"}, "--id needs an id"},
		{[]string{"notes", "download", g}, "notes download requires --id <note id>"},
		{[]string{"notes", "download", "--id", "p1"}, "notes download requires a group"},
		{[]string{"notes", "download", g, "--id", "p1", "--limit", "3"}, "--limit does not apply to notes download"},
		{[]string{"notes", "add", "--message", "hi", "--dry-run"}, "notes add requires --to"},
		{[]string{"notes", "add", g, "--message", "hi", "--dry-run"}, "unexpected argument"},
		{[]string{"notes", "add", "--to", g, "--dry-run"}, "requires the text"},
		{[]string{"notes", "add", "--to", g, "--message", "a", "--stdin", "--dry-run"}, "only one of"},
		{[]string{"notes", "add", "--to", g, "--message", "", "--dry-run"}, "empty"},
		{[]string{"notes", "add", "--to", g, "--message", "hi"}, "refusing to add the note without confirmation"},
		{[]string{"notes", "add", "--to", g, "--message", "hi", "--dry-run=false"}, "takes no value"},
		{[]string{"notes", "add", "--to", g, "--message", "hi", "--id", "p1", "--dry-run"}, "--id does not apply to notes add"},
		{[]string{"notes", "comment", "--to", g, "--message", "hi", "--dry-run"}, "notes comment requires --id <note id>"},
		{[]string{"notes", "comment", "--to", g, "--id", "p1", "--message", "hi", "--comment", "c1", "--dry-run"}, "--comment does not apply to notes comment"},
		{[]string{"notes", "comment", "--to", g, "--id", "p1", "--message", "hi", "-y=1"}, "takes no value"},
		{[]string{"notes", "delete", "--to", g, "--dry-run"}, "notes delete requires --id <note id>"},
		{[]string{"notes", "delete", "--to", g, "--id", "p1", "--comment", "", "--dry-run"}, "requires --comment <comment id>"},
		{[]string{"notes", "delete", "--to", g, "--id", "p1", "--message", "x", "--dry-run"}, "--message does not apply to notes delete"},
		{[]string{"notes", "delete", "--to", g, "--id", "p1", "--allow-duplicate", "--dry-run"}, "--allow-duplicate does not apply"},
		{[]string{"notes", "delete", "--to", g, "--id", "p1"}, "refusing to delete the note without confirmation"},
		{[]string{"notes", "delete", "--to", g, "--id", "p1", "--comment", "p1_2"}, "refusing to delete the comment"},
		{[]string{"albums"}, "albums requires a group"},
		{[]string{"albums", g, "--limit", "5"}, "add --id <album id>"},
		{[]string{"albums", g, "--title", "T"}, "--title does not apply to albums <chat>"},
		{[]string{"albums", "download", g}, "albums download requires --id <album id>"},
		{[]string{"albums", "download", g, "--id", "1", "--photo", "2", "--limit", "3"}, "--limit applies only without it"},
		{[]string{"albums", "download", g, "--id", "1", "--photo", "a/b"}, "--photo needs an id"},
		{[]string{"albums", "add", "--to", g, "--file", "x.jpg", "--dry-run"}, "requires --title <title> for a new album or --id"},
		{[]string{"albums", "add", "--to", g, "--title", "T", "--id", "1", "--dry-run"}, "not both"},
		{[]string{"albums", "add", "--to", g, "--id", "1", "--dry-run"}, "needs the photos to add"},
		{[]string{"albums", "add", "--to", g, "--title", "a\nb", "--dry-run"}, "one line"},
		{[]string{"albums", "add", "--to", g, "--title", " ", "--dry-run"}, "--title is empty"},
		{[]string{"albums", "add", "--to", g, "--title", "T", "--photo", "1", "--dry-run"}, "--photo does not apply to albums add"},
		{[]string{"albums", "add", "--to", g, "--title", "T", "--file", noMatch, "--dry-run"}, "no file matches"},
		{[]string{"albums", "add", "--to", g, "--title", "T"}, "refusing to create the album without confirmation"},
		{[]string{"albums", "add", "--to", g, "--id", "7", "--file", "x.jpg"}, "refusing to add the photos"},
		{[]string{"albums", "delete", "--to", g, "--dry-run"}, "albums delete requires --id <album id>"},
		{[]string{"albums", "delete", "--to", g, "--id", "1", "--file", "x.jpg", "--dry-run"}, "--file does not apply to albums delete"},
		{[]string{"albums", "delete", g, "--id", "1", "--dry-run"}, "unexpected argument"},
		{[]string{"albums", "delete", "--to", g, "--id", "1"}, "refusing to delete the album without confirmation"},
		{[]string{"albums", "delete", "--to", g, "--id", "1", "--photo", "9"}, "refusing to delete the photos"},
		// An id that is only dots would be a dot-segment in the URL path.
		{[]string{"albums", "delete", "--to", g, "--id", "..", "-y"}, "--id needs an id"},
		{[]string{"albums", "delete", "--to", g, "--id", "1", "--photo", ".", "-y"}, "--photo needs an id"},
		{[]string{"albums", g, "--id", ".hidden"}, "--id needs an id"},
		{[]string{"notes", g, "--id", "---"}, "--id needs an id"},
		{[]string{"notes", g, "--cursor", "p1"}, "--cursor needs the value"},
		{[]string{"notes", g, "--cursor", "p1:abc"}, "--cursor needs the value"},
		{[]string{"notes", g, "--cursor", "..:12"}, "--cursor needs the value"},
		{[]string{"notes", g, "--id", "p1", "--cursor", "p1:12"}, "--cursor apply to the list"},
		{[]string{"notes", "download", g, "--id", "p1", "--cursor", "p1:12"}, "--cursor does not apply"},
	} {
		code, out := runCLI(c.args...)
		var e errorOutput
		if code != 1 || json.Unmarshal(out, &e) != nil || e.Error != string(line.ErrInvalidArgument) || !strings.Contains(e.Message, c.msg) {
			t.Errorf("%q: rc=%d out=%s, want INVALID_ARGUMENT containing %q", c.args, code, out, c.msg)
		}
	}
}

// A first word that names a subcommand is the subcommand; anything else is
// the chat, even when a later word is a subcommand's name.
func TestTimelineSubcommandRule(t *testing.T) {
	for _, c := range []struct {
		args      []string
		sub, chat string
	}{
		{nil, "", ""},
		{[]string{"Family"}, "", "Family"},
		{[]string{"Family", "add"}, "", "Family add"},
		{[]string{"add"}, "add", ""},
		{[]string{"download", "Family", "Group"}, "download", "Family Group"},
		{[]string{"comment", tlArgGroup}, "comment", tlArgGroup},
		{[]string{"Add"}, "", "Add"},
	} {
		sub, chat := timelineSub(c.args, notesSubs)
		if sub != c.sub || chat != c.chat {
			t.Errorf("notes %q: sub %q chat %q, want %q %q", c.args, sub, chat, c.sub, c.chat)
		}
	}
	if sub, chat := timelineSub([]string{"comment", "x"}, albumsSubs); sub != "" || chat != "comment x" {
		t.Errorf("albums has no comment subcommand: sub %q chat %q", sub, chat)
	}
}

func TestTimelineHelp(t *testing.T) {
	code, out := runCLI("help", "--json")
	var doc helpInfo
	if code != 0 || json.Unmarshal(out, &doc) != nil {
		t.Fatalf("help --json: rc=%d", code)
	}
	for _, want := range []struct {
		name  string
		flags []string
	}{
		{"notes", []string{"--limit", "--comments", "--since", "--until", "--cursor", "--id", "--to", "--message", "--message-file", "--stdin", "--comment", "--dry-run", "-y/--yes", "--allow-duplicate"}},
		{"albums", []string{"--id", "--limit", "--photo", "--to", "--title", "--file", "--dry-run", "-y/--yes", "--allow-duplicate"}},
	} {
		i := slices.IndexFunc(doc.Commands, func(c cmdDoc) bool { return c.Name == want.name })
		if i < 0 {
			t.Errorf("help --json lacks %s", want.name)
			continue
		}
		c := doc.Commands[i]
		var got []string
		for _, f := range c.Flags {
			first, _, _ := strings.Cut(f, " ")
			got = append(got, first)
		}
		if !slices.Equal(got, want.flags) {
			t.Errorf("%s flags %v, want %v", want.name, got, want.flags)
		}
		for _, s := range []string{"LINE PC never stores", "Groups only", "CHANNEL_TOKEN_MISSING", "--dry-run", "untrusted", "Not supported", "newest", "online"} {
			if !strings.Contains(c.Summary, s) {
				t.Errorf("%s summary lacks %q", want.name, s)
			}
		}
		code, out := runCLI("help", want.name, "--json")
		var one cmdHelp
		if code != 0 || json.Unmarshal(out, &one) != nil || one.Name != want.name || len(one.GlobalFlags) == 0 {
			t.Errorf("help %s --json: rc=%d out=%.100s", want.name, code, out)
		}
	}
	notes := doc.Commands[slices.IndexFunc(doc.Commands, func(c cmdDoc) bool { return c.Name == "notes" })]
	for _, s := range []string{"photos", "polls", "poll", "only your own"} {
		if !strings.Contains(notes.Summary, s) {
			t.Errorf("notes summary lacks %q", s)
		}
	}
	steps := ""
	for _, r := range doc.Recipes {
		steps += strings.Join(r.Steps, "\n")
	}
	for _, s := range []string{"line notes", "line albums", "CHANNEL_TOKEN_MISSING"} {
		if !strings.Contains(steps, s) {
			t.Errorf("recipes lack %q", s)
		}
	}
	if !strings.Contains(doc.About, "notes and albums") || !strings.Contains(limitsDoc(), "notes 20 (max 500)") {
		t.Errorf("about or limits do not mention notes and albums")
	}
	t.Setenv("LINECLI_LANG", "en")
	code, out = runCLI("help")
	if s := string(out); code != 0 || !strings.Contains(s, "\n  line notes <chat>") || !strings.Contains(s, "\n  line albums <chat>") {
		t.Errorf("help overview lacks notes or albums: %s", out)
	}
	code, out = runCLI("notes", "--help")
	if s := string(out); code != 0 || !strings.HasPrefix(s, "Usage:\n  line notes <chat>") || !strings.Contains(s, "--comment <id>") {
		t.Errorf("notes --help: rc=%d out=%s", code, out)
	}
}

func textRun() (*runCtx, *bytes.Buffer) {
	var b bytes.Buffer
	return &runCtx{out: &b, stdout: &b, err: &bytes.Buffer{}, text: true}, &b
}

func TestTimelineTextOutput(t *testing.T) {
	rc, b := textRun()
	rc.emitNotes([]line.Note{
		{ID: "p2", Author: "Amy", Text: "see this", Kind: "link", CreatedAt: "2026-11-20T10:00:00+08:00", CommentCount: 0, Link: &line.NoteLink{Title: "A page", URL: "https://example.com/a"}, Media: []line.NoteMedia{{ID: "o1", Type: "image"}}},
		{ID: "p1", Author: "Me", Text: "two\nlines", Kind: "note", CreatedAt: "2026-11-19T09:00:00+08:00", CommentCount: 1, FromMe: true,
			Comments: []line.NoteComment{{ID: "p1_1", Author: "Amy", Text: "ok\tthanks", CreatedAt: "2026-11-19T09:05:00+08:00"}}},
	})
	want := "2026-11-20T10:00:00+08:00\tp2\tAmy\t\tlink\tcomments=0\tsee this A page https://example.com/a [1 photos/videos]\n" +
		"2026-11-19T09:00:00+08:00\tp1\tMe\tme\tnote\tcomments=1\ttwo\\nlines\n" +
		"2026-11-19T09:05:00+08:00\tp1_1\tAmy\t\tcomment\t\tok\\tthanks\n"
	if b.String() != want {
		t.Errorf("notes --text:\n%q\nwant\n%q", b.String(), want)
	}

	rc, b = textRun()
	rc.emitAlbums([]line.GroupAlbum{{ID: "123", Title: "Trip", PhotoCount: 2, CreatedAt: "2026-11-01T08:00:00+08:00", UpdatedAt: "2026-11-02T08:00:00+08:00"}})
	rc.emitAlbumPhotos([]line.AlbumPhoto{{ID: "9", Type: "image", Uploader: "Me", FromMe: true, CreatedAt: "2026-11-02T08:00:00+08:00", Width: 640, Height: 480}, {ID: "8", Type: "video", Uploader: "Amy"}})
	want = "123\tTrip\tphotos=2\t2026-11-01T08:00:00+08:00\t2026-11-02T08:00:00+08:00\n" +
		"2026-11-02T08:00:00+08:00\t9\tMe\tme\timage\t640x480\n" +
		"\t8\tAmy\t\tvideo\t\n"
	if b.String() != want {
		t.Errorf("albums --text:\n%q\nwant\n%q", b.String(), want)
	}

	rc, b = textRun()
	code := rc.emitTimeline(&line.TimelinePlan{Action: "note_add", To: tlArgGroup, ToName: "Test Group", ChatType: "group", Text: "hello", DryRun: true})
	if code != 0 || b.String() != "DRY-RUN (nothing done)\naction: note_add\ngroup: Test Group ("+tlArgGroup+")\ntext: hello\n" {
		t.Errorf("note_add --text: rc=%d %q", code, b.String())
	}

	// A write that stopped part way prints what happened and exits 1.
	rc, b = textRun()
	code = rc.emitTimeline(&line.TimelinePlan{Action: "album_create", To: tlArgGroup, ToName: "Test Group", ChatType: "group", AlbumID: "77", Title: "Trip", Sent: true,
		Files: []line.TimelineFile{{Name: "a.jpg", Kind: "image", Bytes: 10, GPSRemoved: true, Status: "sent"}, {Name: "b.png", Kind: "image", Bytes: 20, Status: "failed", Error: "refused"}},
		URL:   "line://group/home/albums/album?albumId=77", Error: "SEND_FAILED", ErrorMessage: "refused"})
	for _, s := range []string{"DONE\n", "album: 77\n", "title: Trip\n", "image a.jpg (10 bytes, location removed): sent\n", "image b.png (20 bytes): failed\n  error: refused\n", "stopped: SEND_FAILED: refused\n"} {
		if !strings.Contains(b.String(), s) {
			t.Errorf("album_create --text lacks %q:\n%s", s, b.String())
		}
	}
	if code != 1 {
		t.Errorf("a write that stopped part way: rc=%d, want 1", code)
	}

	// The JSON of a stopped write is the plan, with exit code 1 too.
	var jb bytes.Buffer
	rc = &runCtx{out: &jb, stdout: &jb, err: &bytes.Buffer{}}
	if code := rc.emitTimeline(&line.TimelinePlan{Action: "album_add", Error: "SEND_UNCERTAIN"}); code != 1 || !strings.Contains(jb.String(), `"error":"SEND_UNCERTAIN"`) {
		t.Errorf("stopped write JSON: rc=%d %s", code, jb.String())
	}
}
