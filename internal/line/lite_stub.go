// This file is what stands in for the code the open build leaves out: everything
// that talks to LINE's server. It supplies just enough for the read side to
// compile and for every such feature to fail with a clear, uniform error.
// Reading, searching and summarizing the local LINE database work unchanged,
// and so does `preview`, which only decrypts media already cached on this PC.
//
// It defines no protocol logic or secrets on purpose: it is the one file that
// shows exactly what the open build does NOT contain.

package line

import (
	"errors"
	"time"
)

// Edition is "open": this build was made from the public source, without the
// protocol core. The full build reports "full" from edition_full.go.
const Edition = "open"

// errOpenBuild is returned by every feature that needs the private protocol core.
var errOpenBuild = errors.New("not available in this open build of line-cli: sending, reactions, unsending, media download, update checks and live server fetch (square/online) need the official prebuilt line-cli")

// --- online / live server fetch (full build: online.go) ---

// OnlineOptions is accepted but ignored by the open build (no server fetch).
type OnlineOptions struct {
	Enabled    bool
	MaxThreads int
	Notify     func(string)
	Session    *Session
}

// OnlineReport is always zero in the open build.
type OnlineReport struct {
	Calls                      int
	Threads                    int
	ThreadsLeft                int
	Missing                    int
	Failed                     []string
	Cut                        []string
	Unavailable                string
	ShortThreads, ShortReplies int64
}

// Session and server are opaque placeholders in the open build.
type Session struct{}
type server struct{}

// NewSession returns an unused session placeholder.
func NewSession() *Session { return &Session{} }

// UseFakeSquare is a no-op in the open build (there is no server to fake); it
// returns a no-op restore so shared test helpers compile.
func UseFakeSquare(reply func(body []byte) ([]byte, error)) (restore func()) { return func() {} }

// UseServer is a no-op in the open build: there is no server fetch.
func (r *Reader) UseServer(opt OnlineOptions) {}

// OnlineReport reports nothing in the open build.
func (r *Reader) OnlineReport() OnlineReport { return OnlineReport{} }

func (r *Reader) online() bool { return false }

// unreadGap returns no server-side gap in the open build.
func (r *Reader) unreadGap(chatID string, f *Freshness) (gap []Message, gapPlain []bool, gapCount int64, complete bool, err error) {
	return nil, nil, 0, false, nil
}

// ReadChat in the open build reads only what this PC stores: no open-chat or
// thread fill from LINE's server. The result says what that leaves out.
func (r *Reader) ReadChat(chat ChatSummary, opt ReadOptions) (ReadResult, error) {
	opt.ChatArg, opt.Around = chat.ChatID, ""
	return r.readStored(chat, opt)
}

// --- send text (full build: send.go) ---

type SendOptions struct {
	To             string
	Text           string
	DryRun         bool
	AllowDuplicate bool
	ReplyTo        string
	Mentions       []string
	MentionAll     bool
	Normalized     bool
	Notify         func(string)
}

// MentionSpan mirrors the full build's shape; the open build never fills one.
type MentionSpan struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Text  string `json:"text"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// ReactOptions, UnsendOptions and ActionPlan mirror the full build's shapes.
type ReactOptions struct {
	To       string
	ID       string
	Reaction string
	Remove   bool
	DryRun   bool
	Notify   func(string)
}

type UnsendOptions struct {
	To     string
	ID     string
	DryRun bool
	Notify func(string)
}

type ActionPlan struct {
	Action    string       `json:"action"`
	To        string       `json:"to"`
	ToName    string       `json:"to_name"`
	ChatType  string       `json:"chat_type"`
	MessageID string       `json:"message_id"`
	Message   *ReplyTarget `json:"message"`
	Reaction  string       `json:"reaction,omitempty"`
	Endpoint  string       `json:"endpoint"`
	DryRun    bool         `json:"dry_run"`
	Sent      bool         `json:"sent"`
	Verified  bool         `json:"verified"`
}

// LatestRelease is not available in the open build, which never goes online
// by itself; update it from its source.
func LatestRelease(repo string, timeout time.Duration) (string, error) { return "", errOpenBuild }

// React is not available in the open build.
func (r *Reader) React(opt ReactOptions) (*ActionPlan, error) { return nil, errOpenBuild }

// Unsend is not available in the open build.
func (r *Reader) Unsend(opt UnsendOptions) (*ActionPlan, error) { return nil, errOpenBuild }

// SendPlan mirrors the full build's shape so the CLI's output code compiles;
// the open build never fills one (Send returns errOpenBuild).
type SendPlan struct {
	To              string        `json:"to"`
	ToName          string        `json:"to_name"`
	ChatType        string        `json:"chat_type"`
	From            string        `json:"from"`
	Text            string        `json:"text"`
	TextLength      int           `json:"text_length"`
	TextSHA256      string        `json:"text_sha256"`
	Normalized      bool          `json:"normalized"`
	ReplyTo         *ReplyTarget  `json:"reply_to,omitempty"`
	Mentions        []MentionSpan `json:"mentions,omitempty"`
	MentionsAll     bool          `json:"mentions_all,omitempty"`
	SenderKeyID     int           `json:"sender_key_id"`
	ReceiverKeyID   int           `json:"receiver_key_id"`
	E2EEVersion     int           `json:"e2ee_version"`
	ChunkSizes      []int         `json:"chunk_sizes"`
	BodyBytes       int           `json:"body_bytes"`
	Endpoint        string        `json:"endpoint"`
	DryRun          bool          `json:"dry_run"`
	Sent            bool          `json:"sent"`
	Verified        bool          `json:"verified"`
	ServerMessageID string        `json:"server_message_id,omitempty"`
	SentAt          string        `json:"sent_at,omitempty"`
}

func (r *Reader) Send(opt SendOptions) (*SendPlan, error) { return nil, errOpenBuild }

// --- send files/media (full build: sendfile.go) ---

type SendFilesOptions struct {
	To             string
	Files          []PreparedFile
	DryRun         bool
	AllowDuplicate bool
	Text           string
	Normalized     bool
	ReplyTo        string
	Notify         func(string)
}

type SendFilesPlan struct {
	To            string       `json:"to"`
	ToName        string       `json:"to_name"`
	ChatType      string       `json:"chat_type"`
	From          string       `json:"from"`
	Message       *SendPlan    `json:"text_message,omitempty"`
	ReplyTo       *ReplyTarget `json:"reply_to,omitempty"`
	Files         []FileSend   `json:"files"`
	SenderKeyID   int          `json:"sender_key_id"`
	ReceiverKeyID int          `json:"receiver_key_id"`
	E2EEVersion   int          `json:"e2ee_version"`
	DryRun        bool         `json:"dry_run"`
	SentCount     int          `json:"sent_count"`
	Error         string       `json:"error,omitempty"`
	ErrorMessage  string       `json:"message,omitempty"`
}

type FileSend struct {
	Path            string `json:"path"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	Bytes           int64  `json:"bytes"`
	SHA256          string `json:"sha256"`
	DurationMs      int64  `json:"duration_ms,omitempty"`
	GPSRemoved      bool   `json:"gps_removed,omitempty"`
	Width           int    `json:"width,omitempty"`
	Height          int    `json:"height,omitempty"`
	Status          string `json:"status"`
	Verified        bool   `json:"verified,omitempty"`
	ServerMessageID string `json:"server_message_id,omitempty"`
	SentAt          string `json:"sent_at,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	Error           string `json:"error,omitempty"`
}

type PreparedFile struct{}

func (r *Reader) SendFiles(opt SendFilesOptions) (*SendFilesPlan, error) { return nil, errOpenBuild }

func PrepareFiles(paths []string, asFile, allowDuplicate bool, notify func(string)) ([]PreparedFile, error) {
	return nil, errOpenBuild
}

// --- attachment download (full build: download.go) ---

type DownloadOptions struct {
	ChatArg string
	ID      string
	SinceTs int64
	UntilTs int64
	Limit   int
	OutDir  string
	Media   bool
	Notify  func(string)
}

type DownloadResult struct {
	ID       string `json:"id"`
	ChatID   string `json:"chat_id"`
	ChatName string `json:"chat_name"`
	Type     string `json:"type"`
	Sender   string `json:"sender"`
	SenderID string `json:"sender_id,omitempty"`
	Filename string `json:"filename,omitempty"`
	SentAt   string `json:"sent_at,omitempty"`
	FromMe   bool   `json:"from_me,omitempty"`
	Status   string `json:"status"`
	SavedAs  string `json:"saved_as,omitempty"`
	Bytes    int    `json:"bytes,omitempty"`
	Note     string `json:"note,omitempty"`
}

func (r *Reader) Download(opt DownloadOptions) ([]DownloadResult, error) { return nil, errOpenBuild }
