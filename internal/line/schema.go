package line

import (
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MaxLimit is the largest result limit any command applies; larger values are lowered to it.
const MaxLimit = 5000

// itoa formats a uint32 without strconv or allocation beyond the result; it is
// shared by the read side and (in the full build) the token and download code.
func itoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	var d [10]byte
	n := len(d)
	for v > 0 {
		n--
		d[n] = byte('0' + v%10)
		v /= 10
	}
	return string(d[n:])
}

// Default result limits; a zero --limit falls back to these.
const (
	DefaultChatsLimit     = 50
	DefaultReadLimit      = 200
	DefaultReadRangeLimit = 500
	DefaultAroundLimit    = 20
	DefaultSearchLimit    = 50
	DefaultFindFileLimit  = 200
	DefaultPreviewLimit   = 50
	DefaultDownloadLimit  = 200
	DefaultUnreadChats    = 50
	DefaultUnreadPerChat  = 200
)

// Open-chat read-thread limits. These bound a --max-threads value and are used
// in help and validation whether or not the server-fetch feature is built, so
// they live on the always-built read side.
const (
	// DefaultReadThreads caps the threads one read of an open chat fetches.
	DefaultReadThreads = 20
	// MaxReadThreads is the largest --max-threads accepted.
	MaxReadThreads = 100
	// MaxGapPages caps the calls spent on an open chat's messages after the
	// newest stored one.
	MaxGapPages = 50
)

// contentType names _contentType codes. Unsent rows are typed "unsent" and
// event rows (_type 3, and the code-18 chat notices) "event" whatever their code.
var contentType = map[int64]string{
	0: "text", 1: "image", 2: "video", 3: "audio", 6: "call", 7: "sticker",
	13: "contact", 14: "file", 15: "location", 16: "post", 17: "template", 22: "flex",
}

// contentTypeName names a _contentType code: its type, or type_N for an unknown code.
func contentTypeName(ct int64) string {
	if n, ok := contentType[ct]; ok {
		return n
	}
	return "type_" + strconv.FormatInt(ct, 10)
}

func contentTypeCode(name string) (int64, bool) {
	for code, n := range contentType {
		if n == name {
			return code, true
		}
	}
	return 0, false
}

// ContentTypeNames lists every message type read, search and unread output.
func ContentTypeNames() []string {
	out := []string{"event", "unsent"}
	for _, n := range contentType {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// EffectiveLimit is the limit a command applies when given v (0 = fallback, capped at MaxLimit).
func EffectiveLimit(v, fallback int) int { return saneLimit(v, fallback) }

// untilMs turns an inclusive --until in unix seconds into a millisecond bound
// covering that whole second, since sent_at is only second-precise.
func untilMs(ts int64) int64 { return ts*1000 + 999 }

func saneLimit(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	if v > MaxLimit {
		return MaxLimit
	}
	return v
}

func escapeLike(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(v)
}

// Load the local time zone before any query runs. Without this, the first
// tsToIso loads it from the registry while a statement is open, and read
// --around on a large chat then crashes the Go runtime in most runs started by
// python, PowerShell 7 or cmd (never by Git Bash or a Go parent; go1.27.0,
// windows/amd64, decrypting VFS): "found pointer to free object" from the
// background sweeper, or "unknown pc" while scanning the main stack, which sits
// in Stmt.Step, a VFS read or readMessageRow. checkptr builds, asyncpreemptoff=1
// and the non-Green Tea GC crash too; the root cause is unknown and this only
// avoids it. TestLiveAroundStress guards it.
func init() { _ = time.Local.String() }

func tsToIso(ms int64) string {
	if ms == 0 {
		return ""
	}
	t := ms
	if ms <= 1_000_000_000_000 {
		t = ms * 1000
	}
	return time.UnixMilli(t).Format("2006-01-02T15:04:05-07:00")
}

func displayName(name, overridden string) string {
	if overridden != "" {
		return overridden
	}
	return name
}

type nameMaps struct {
	group    map[string]string
	square   map[string]string
	contact  map[string]string
	ownName  map[string]string // contact's own display name, only when you set a nickname
	official map[string]bool
	room     map[string]bool
	thread   map[string]string // thread chat id -> parent open chat id
	midType  map[string]int64
	members  map[string]int // group chat id -> member count
	self     string
	selfName string
}

type chatInfo struct {
	name   string
	typ    string
	alt    string // contact's own name when name is your nickname for them
	parent string
}

func (m *nameMaps) resolveChat(chatID string) chatInfo {
	if chatID != "" && chatID == m.self {
		return chatInfo{name: orElse(m.selfName, chatID), typ: "self"}
	}
	if v, ok := m.group[chatID]; ok {
		return chatInfo{name: orElse(v, chatID), typ: "group"}
	}
	if v, ok := m.square[chatID]; ok {
		return chatInfo{name: orElse(v, chatID), typ: "open"}
	}
	if p, ok := m.thread[chatID]; ok {
		name := chatID
		if pn := m.square[p]; pn != "" {
			name = pn + " (thread)"
		}
		return chatInfo{name: name, typ: "thread", parent: p}
	}
	if m.room[chatID] {
		return chatInfo{name: "多人聊天", typ: "multi"}
	}
	mt, known := m.midType[chatID]
	typ := chatKind(chatID, mt, known)
	if typ != "personal" {
		return chatInfo{name: chatID, typ: typ}
	}
	if m.official[chatID] {
		typ = "official"
	}
	return chatInfo{name: orElse(m.contact[chatID], chatID), typ: typ, alt: m.ownName[chatID]}
}

// chatKind classifies a chat with no name-table row by _chat._midType, else by id prefix.
func chatKind(chatID string, midType int64, known bool) string {
	if known {
		switch midType {
		case 0:
			return "personal"
		case 1:
			return "multi"
		case 2:
			return "group"
		case 4:
			return "open"
		case 7:
			return "thread"
		}
	}
	if chatID != "" {
		switch chatID[0] {
		case 'u':
			return "personal"
		case 'r':
			return "multi"
		case 'c':
			return "group"
		case 'm':
			return "open"
		case 't':
			return "thread"
		}
	}
	return "unknown"
}

func orElse(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

var chatTypes = []string{"personal", "official", "group", "open", "thread", "self", "multi", "unknown"}

func isChatTypeFilter(s string) bool { return slices.Contains(chatTypes, s) }

func IsChatTypeFilter(s string) bool { return isChatTypeFilter(s) }

// ChatTypeNames lists the chat types, in the order help shows them.
func ChatTypeNames() []string { return slices.Clone(chatTypes) }

type ChatSummary struct {
	ChatID        string `json:"chat_id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	LastMessageAt string `json:"last_message_at,omitempty"`
	ParentChatID  string `json:"parent_chat_id,omitempty"`
	MemberCount   int    `json:"member_count,omitempty"`
	Hidden        bool   `json:"hidden,omitempty"`
	Muted         bool   `json:"muted,omitempty"`
	Pinned        bool   `json:"pinned,omitempty"`
}

type ContactSummary struct {
	ContactID   string `json:"contact_id"`
	DisplayName string `json:"display_name"`
	IsMe        bool   `json:"is_me,omitempty"`
	ProfileName string `json:"profile_name,omitempty"`
	Nickname    string `json:"nickname,omitempty"`
	ChatID      string `json:"chat_id,omitempty"`
	Type        string `json:"type,omitempty"`
	Source      string `json:"source"`
	Community   string `json:"community,omitempty"`
}

type Message struct {
	ID            string        `json:"id,omitempty"`
	Type          string        `json:"type"`
	Sender        string        `json:"sender"`
	SenderID      string        `json:"sender_id,omitempty"`
	Content       *string       `json:"content,omitempty"`
	Filename      string        `json:"filename,omitempty"`
	FileSize      *int64        `json:"file_size,omitempty"`
	URL           string        `json:"url,omitempty"`
	Title         string        `json:"title,omitempty"`
	Description   string        `json:"description,omitempty"`
	SentAt        string        `json:"sent_at,omitempty"`
	FromMe        bool          `json:"from_me,omitempty"`
	OriginalType  string        `json:"original_type,omitempty"`
	Event         string        `json:"event,omitempty"`
	Targets       []Person      `json:"targets,omitempty"`
	Undecryptable bool          `json:"undecryptable,omitempty"`
	ReplyTo       string        `json:"reply_to,omitempty"`
	ThreadID      string        `json:"thread_id,omitempty"`
	ThreadReplies int64         `json:"thread_replies,omitempty"`
	Forwarded     bool          `json:"forwarded,omitempty"`
	Mentions      []Person      `json:"mentions,omitempty"`
	MentionsAll   bool          `json:"mentions_all,omitempty"`
	Reactions     *Reactions    `json:"reactions,omitempty"`
	Edited        bool          `json:"edited,omitempty"`
	Location      *GeoLocation  `json:"location,omitempty"`
	Call          *Call         `json:"call,omitempty"`
	LinkPreviews  []LinkPreview `json:"link_previews,omitempty"`
	Album         *Album        `json:"album,omitempty"`
	DurationSec   float64       `json:"duration_sec,omitempty"`
}

// Person is a member an event or mention refers to; name is omitted when unknown.
type Person struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type Reactions struct {
	Total  int64            `json:"total"`
	ByType map[string]int64 `json:"by_type"`
}

type GeoLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Address   string  `json:"address,omitempty"`
	Title     string  `json:"title,omitempty"`
}

type Call struct {
	Kind        string  `json:"kind"`
	Result      string  `json:"result,omitempty"`
	DurationSec float64 `json:"duration_sec,omitempty"`
}

type LinkPreview struct {
	URL         string `json:"url,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type Album struct {
	ID    string `json:"id"`
	Index int64  `json:"index,omitempty"`
	Total int64  `json:"total,omitempty"`
}

type SearchHit struct {
	ChatID   string `json:"chat_id"`
	ChatName string `json:"chat_name"`
	ChatType string `json:"chat_type"`
	Message
}

type UnreadChat struct {
	ChatID         string    `json:"chat_id"`
	Name           string    `json:"name"`
	Type           string    `json:"type"`
	UnreadCount    int64     `json:"unread_count"`
	AvailableCount int64     `json:"available_count"`
	MissingCount   int64     `json:"missing_count"`
	FullySynced    bool      `json:"fully_synced"`
	Boundary       string    `json:"boundary"`
	Stale          bool      `json:"stale"`
	LastActivityAt string    `json:"last_activity_at,omitempty"`
	NewestStoredAt string    `json:"newest_stored_at,omitempty"`
	Muted          bool      `json:"muted"`
	Hidden         bool      `json:"hidden,omitempty"`
	Messages       []Message `json:"messages"`
	MoreUnread     bool      `json:"-"`
}

func strPtr(s string) *string { return &s }

func metaString(meta map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := meta[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func metaNumber(meta map[string]any, keys ...string) *int64 {
	for _, k := range keys {
		v, ok := meta[k]
		if !ok {
			continue
		}
		switch x := v.(type) {
		case float64:
			n := int64(x)
			return &n
		case string:
			if x == "" {
				continue
			}
			if n, err := strconv.ParseInt(x, 10, 64); err == nil {
				return &n
			}
		}
	}
	return nil
}

func parseMeta(s string) map[string]any {
	if s == "" {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) != nil || m == nil {
		return map[string]any{}
	}
	return m
}
