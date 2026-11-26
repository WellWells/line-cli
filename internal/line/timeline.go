package line

// Group notes and albums: the output shapes, the options of each operation and
// the checks that need no server. LINE PC never stores a group's notes or
// albums; the full build reads and writes them on LINE's server
// (timeline_*.go), and the open build stubs them (lite_stub.go).

import (
	"strconv"
	"strings"

	"github.com/ncruces/go-sqlite3"
)

// Limits of the notes and albums commands.
const (
	DefaultNotesLimit       = 20
	MaxNotesLimit           = 500
	MaxNoteInlineComments   = 50 // comments shown with each note of a list
	DefaultNoteComments     = 50 // comments read with one note
	MaxNoteComments         = 500
	DefaultAlbumPhotosLimit = 100
	MaxAlbumPhotosLimit     = 1000
	// MaxAlbumUploadBytes is the largest photo an album takes, as for a photo
	// sent in a chat.
	MaxAlbumUploadBytes = 20 << 20
	// MaxAlbumUploadFiles is how many photos one albums add takes.
	MaxAlbumUploadFiles = 50
)

// Note is one note (post) of a group's 記事本. Text and comments are chat
// content: untrusted.
type Note struct {
	ID           string        `json:"id"`
	ChatID       string        `json:"chat_id"`
	ChatName     string        `json:"chat_name"`
	Author       string        `json:"author"`
	AuthorID     string        `json:"author_id,omitempty"`
	Text         string        `json:"text"`
	Kind         string        `json:"kind"` // note | poll | link
	CreatedAt    string        `json:"created_at"`
	UpdatedAt    string        `json:"updated_at,omitempty"`
	CommentCount int64         `json:"comment_count"`
	LikeCount    int64         `json:"like_count"`
	Media        []NoteMedia   `json:"media,omitempty"`
	Link         *NoteLink     `json:"link,omitempty"`
	URL          string        `json:"url,omitempty"`
	Comments     []NoteComment `json:"comments,omitempty"`
	FromMe       bool          `json:"from_me,omitempty"`
}

// NoteMedia is a photo or video of a note; notes download saves it.
type NoteMedia struct {
	ID   string `json:"id"`
	Type string `json:"type"` // image | video
}

// NoteLink is the link preview of a note.
type NoteLink struct {
	Title string `json:"title,omitempty"`
	URL   string `json:"url,omitempty"`
}

// NoteComment is one comment on a note.
type NoteComment struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	AuthorID  string `json:"author_id,omitempty"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
	FromMe    bool   `json:"from_me,omitempty"`
}

// GroupAlbum is one album of a group's 相簿.
type GroupAlbum struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	PhotoCount int64  `json:"photo_count"`
	ImageCount int64  `json:"image_count"`
	VideoCount int64  `json:"video_count"`
	CreatedAt  string `json:"created_at,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
	URL        string `json:"url,omitempty"`
}

// AlbumPhoto is one photo or video of an album.
type AlbumPhoto struct {
	ID         string `json:"id"`
	Type       string `json:"type"` // image | video
	Uploader   string `json:"uploader"`
	UploaderID string `json:"uploader_id,omitempty"`
	CreatedAt  string `json:"created_at,omitempty"`
	ShotAt     string `json:"shot_at,omitempty"`
	Width      int64  `json:"width,omitempty"`
	Height     int64  `json:"height,omitempty"`
	FromMe     bool   `json:"from_me,omitempty"`
}

// TimelinePlan is what a notes or albums write does, or (dry run) would do.
// Action is note_add, note_comment, note_delete, comment_delete,
// album_create, album_add, album_delete or photo_delete.
type TimelinePlan struct {
	Action    string `json:"action"`
	To        string `json:"to"`
	ToName    string `json:"to_name"`
	ChatType  string `json:"chat_type"`
	Text      string `json:"text,omitempty"`
	PostID    string `json:"post_id,omitempty"`
	CommentID string `json:"comment_id,omitempty"`
	AlbumID   string `json:"album_id,omitempty"`
	Title     string `json:"title,omitempty"`
	// PhotoCount is how many photos the album holds (album_delete only).
	PhotoCount *int64         `json:"photo_count,omitempty"`
	Files      []TimelineFile `json:"files,omitempty"`
	Photos     []string       `json:"photos,omitempty"`
	URL        string         `json:"url,omitempty"`
	DryRun     bool           `json:"dry_run"`
	Sent       bool           `json:"sent"`
	// Deleted is how many photos LINE says it deleted (photo_delete only,
	// always present there, 0 included).
	Deleted *int64 `json:"deleted,omitempty"`
	// Error and ErrorMessage are set when a write went part way: the album
	// was created but adding its photos failed, some photos did not upload,
	// or LINE deleted fewer photos than asked.
	Error        string `json:"error,omitempty"`
	ErrorMessage string `json:"message,omitempty"`
}

// TimelineFile is one photo of albums add and what happened to it: planned
// (dry run), sent, failed (certainly not added), uncertain (LINE may have
// added it: check before adding it again) or not_sent (an earlier photo
// failed).
type TimelineFile struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Bytes      int64  `json:"bytes"`
	GPSRemoved bool   `json:"gps_removed,omitempty"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

// NotesOptions selects the notes of a group (ListNotes) or one note (GetNote,
// with ID). Comments is how many comments come with each note.
type NotesOptions struct {
	Chat     string
	ID       string
	Limit    int
	Comments int
	SinceTs  int64 // unix seconds; 0 = no bound
	UntilTs  int64
	// Cursor continues a scan of the notes where a run that stopped at its
	// page cap left off (POSTID:TIME, as that run printed it).
	Cursor  string
	ShowIDs bool
	Notify  func(string)
}

// AlbumsOptions selects a group's albums (ListAlbums) or one album's photos
// (ListAlbumPhotos, with ID).
type AlbumsOptions struct {
	Chat    string
	ID      string
	Limit   int
	ShowIDs bool
	Notify  func(string)
}

// TimelineDownloadOptions selects what notes download (ID is the note) or
// albums download (ID is the album, Photos optionally some of its photos)
// saves to OutDir (default: the downloads folder).
type TimelineDownloadOptions struct {
	Chat   string
	ID     string
	Photos []string
	Limit  int
	OutDir string
	Notify func(string)
}

// NoteWriteOptions is a notes write: add (Text), comment (ID, Text) or
// delete (ID, and CommentID to delete one comment).
type NoteWriteOptions struct {
	To             string
	ID             string
	CommentID      string
	Text           string
	DryRun         bool
	AllowDuplicate bool
	Notify         func(string)
}

// AlbumWriteOptions is an albums write: add Files to album ID or to a new
// album titled Title, delete album ID, or delete its Photos.
type AlbumWriteOptions struct {
	To             string
	ID             string
	Title          string
	Files          []string
	Photos         []string
	DryRun         bool
	AllowDuplicate bool
	Notify         func(string)
}

// clampLimit is v within [1, max], fallback when v <= 0.
func clampLimit(v, fallback, max int) int {
	if v <= 0 {
		return fallback
	}
	return min(v, max)
}

// CheckTimelineID checks an id given with flag: notes, comments, albums and
// photos have ids of letters, digits, '_', '-' and '.', with at least one
// letter or digit and no leading '.'. Album and photo ids go into URL paths,
// so "." and ".." must never pass.
func CheckTimelineID(flag, id string) error {
	ok := id != "" && len(id) <= 200 && id[0] != '.'
	alnum := false
	for _, c := range id {
		switch {
		case c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			alnum = true
		case c == '_' || c == '-' || c == '.':
		default:
			ok = false
		}
	}
	ok = ok && alnum
	if !ok {
		return &Error{Code: ErrInvalidArgument, Message: flag + " needs an id as notes or albums print it; got " + strconv.Quote(id)}
	}
	return nil
}

// SplitNoteCursor reads a notes --cursor value, POSTID:TIME as a notes run
// printed it.
func SplitNoteCursor(s string) (postID, updatedTime string, err error) {
	i := strings.LastIndexByte(s, ':')
	bad := &Error{Code: ErrInvalidArgument, Message: "--cursor needs the value a notes run printed (POSTID:TIME); got " + strconv.Quote(s)}
	if i <= 0 || i == len(s)-1 || CheckTimelineID("--cursor", s[:i]) != nil {
		return "", "", bad
	}
	if _, perr := strconv.ParseUint(s[i+1:], 10, 63); perr != nil {
		return "", "", bad
	}
	return s[:i], s[i+1:], nil
}

// checkTimelineChat refuses any chat but a group: LINE keeps one-to-one notes
// under a different account mapping, and open chats use another service.
func checkTimelineChat(c ChatSummary, cmd string) error {
	if strings.HasPrefix(c.ChatID, "c") {
		return nil
	}
	why := "only groups have notes and albums"
	switch {
	case strings.HasPrefix(c.ChatID, "u"):
		why = "line-cli reads notes and albums of groups only (LINE keeps one-to-one notes under another account mapping)"
	case strings.HasPrefix(c.ChatID, "m"), strings.HasPrefix(c.ChatID, "t"):
		why = "open chats keep their notes and albums in another service, which line-cli does not read"
	}
	return &Error{
		Code:       ErrInvalidArgument,
		Message:    cmd + " works in groups only; " + strconv.Quote(orElse(c.Name, c.ChatID)) + " is a " + orElse(c.Type, "non-group") + " chat: " + why,
		Candidates: []ChatSummary{{ChatID: c.ChatID, Name: c.Name, Type: c.Type}},
	}
}

// timelineChat resolves a notes or albums chat like read does (exact for a
// write, like send) and checks that it is a group.
func (r *Reader) timelineChat(arg string, exact bool, cmd string) (ChatSummary, error) {
	if strings.TrimSpace(arg) == "" {
		return ChatSummary{}, &Error{Code: ErrInvalidArgument, Message: cmd + " needs a group: a chat_id or the group's name"}
	}
	c, err := r.ResolveChatInfo(arg, exact)
	if err != nil {
		return ChatSummary{}, err
	}
	return c, checkTimelineChat(c, cmd)
}

// profileMid is your own id, from _profile.
func (r *Reader) profileMid() (string, error) {
	var mid string
	if err := r.scan("SELECT _mid FROM _profile LIMIT 1", func(st *sqlite3.Stmt) { mid = st.ColumnText(0) }); err != nil {
		return "", err
	}
	if mid == "" {
		return "", &Error{Code: ErrDBUnreadable, Message: "own mid not found"}
	}
	return mid, nil
}

// CheckAlbumTitle refuses an empty or unprintable album title.
func CheckAlbumTitle(title string) error {
	if strings.TrimSpace(title) == "" {
		return &Error{Code: ErrInvalidArgument, Message: "--title is empty"}
	}
	if strings.ContainsFunc(title, func(r rune) bool { return r == '\n' || hiddenControl(r) }) {
		return &Error{Code: ErrInvalidArgument, Message: "--title must be one line without control characters"}
	}
	if n := len([]rune(title)); n > 100 {
		return &Error{Code: ErrInvalidArgument, Message: "--title is too long: " + strconv.Itoa(n) + " characters (limit 100)"}
	}
	return nil
}
