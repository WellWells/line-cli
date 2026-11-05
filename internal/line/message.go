package line

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf16"
)

// rawMessage holds the _message columns parseMessage reads. RelationType is -1
// when _messageRelationType is NULL.
type rawMessage struct {
	From, Text, Meta, Info, EventInfo, Location, URLPreview string
	Reactions, ReactionStatus, RelatedID                    string
	Created, ContentType, Type, Attribute, RelationType     int64
	HasChunks                                               bool
}

// parseMessage builds the output for one row. sender is the display name (the
// raw mid when no name table has it); name returns a known display name for a
// mid, or "".
func parseMessage(r rawMessage, sender string, fromMe bool, name func(string) string) Message {
	kind := contentTypeName(r.ContentType)
	meta := parseMeta(r.Meta)
	ci := parseMeta(r.Info)
	var ev eventInfo
	if r.EventInfo != "" {
		_ = json.Unmarshal([]byte(r.EventInfo), &ev)
	}
	if sender == r.From {
		sender = orElse(storedName(r.From, ci, ev), sender)
	}
	msg := Message{Type: kind, Sender: sender, SenderID: r.From, SentAt: tsToIso(r.Created), FromMe: fromMe}
	if r.Attribute == 1 {
		msg.Type, msg.OriginalType = "unsent", kind
		return msg
	}
	if r.Type == 3 || r.ContentType == 18 {
		msg.Type = "event"
		parseEvent(&msg, r, meta, ev, name)
		return msg
	}
	text := r.Text
	switch kind {
	case "text":
		if text != "" {
			msg.Content = strPtr(text)
		} else if r.Type == 1 && r.Attribute == 0 && r.HasChunks {
			msg.Undecryptable = true
		}
		msg.LinkPreviews = linkPreviews(r.URLPreview)
	case "image":
		if id := metaString(meta, "GID"); id != "" {
			msg.Album = &Album{ID: id, Index: deref(metaNumber(meta, "GSEQ")), Total: deref(metaNumber(meta, "GTOTAL"))}
		}
	case "video", "audio":
		if d := orNil(metaNumber(meta, "DURATION"), metaNumber(ci, "duration")); d != nil && *d > 0 {
			msg.DurationSec = msToSec(*d)
		}
	case "sticker":
		msg.Content = strPtr("[貼圖]")
	case "file":
		msg.Filename = fileNameOf(r.Info, r.Meta, text)
		if text != "" && text != msg.Filename {
			msg.Content = strPtr(text)
		}
		msg.FileSize = metaNumber(meta, "FILE_SIZE")
	case "post":
		msg.URL = metaString(meta, "postEndUrl", "url", "linkUrl")
		msg.Title = metaString(meta, "officialName", "albumName", "serviceName", "title")
		msg.Description = metaString(meta, "text", "desc")
	case "template", "flex":
		if s := orElse(metaString(meta, "ALT_TEXT"), text); s != "" {
			msg.Content = strPtr(s)
		}
	case "contact":
		if s := orElse(metaString(meta, "displayName"), text); s != "" {
			msg.Content = strPtr(s)
		}
	case "call":
		msg.Call = parseCall(meta)
		if text != "" {
			msg.Content = strPtr(text)
		}
	case "location":
		if text != "" {
			msg.Content = strPtr(text)
		}
		msg.Location = parseLocation(r.Location)
	default:
		if text != "" {
			msg.Content = strPtr(text)
		}
	}
	if r.RelatedID != "" {
		switch r.RelationType {
		case 3:
			msg.ReplyTo = r.RelatedID
		case 0:
			msg.Forwarded = true
		}
	}
	msg.Mentions, msg.MentionsAll = parseMentions(meta, text, name)
	msg.Reactions = parseReactions(r.ReactionStatus, r.Reactions)
	msg.Edited = metaBool(meta, "EDITED")
	return msg
}

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func orNil(ps ...*int64) *int64 {
	for _, p := range ps {
		if p != nil {
			return p
		}
	}
	return nil
}

func msToSec(ms int64) float64 { return float64(ms) / 1000 }

func metaBool(meta map[string]any, key string) bool {
	switch v := meta[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	}
	return false
}

// anyInt reads a JSON number or a numeric string.
func anyInt(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		return int64(x), true
	case string:
		n, err := strconv.ParseInt(x, 10, 64)
		return n, err == nil
	}
	return 0, false
}

// isMid reports whether s looks like a LINE mid: a type letter and 32 hex digits.
func isMid(s string) bool {
	if len(s) != 33 {
		return false
	}
	for _, c := range s[1:] {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}

// Open-chat member events by _eventInfo.type, checked against _squareMember:
// members whose last event is 14 are still joined and those at 13 have left;
// in 15 an admin removes the listed member; 18 carries the new chat name.
var eventTypes = map[int64]string{13: "left", 14: "joined", 15: "kicked", 18: "renamed", 20: "unsent"}

// Chat notices (_contentType 18) by LOC_KEY. LOC_ARGS holds the actor, then
// \x1f-separated target mids (the new name for C_PN), separated by \x1e.
// The keys and their meanings are LINE's own (the LOC_KEY enum in LINE's
// Chrome extension); A_ keys are for chats created with several people at
// once. Other keys, such as C_MA, are given raw.
var noticeKeys = map[string]string{
	"C_MJ": "joined", "C_ML": "left", "C_MI": "invited", "C_GI": "invited", "C_MR": "removed", "C_PN": "renamed",
	"A_MJ": "joined", "A_ML": "left", "A_MC": "invited", "A_MI": "invited", "A_MR": "removed",
	"C_IC": "invite_canceled", "A_IC": "invite_canceled", "C_OL": "invite_failed",
	"C_PI": "photo_changed", "C_SN": "join_by_link_on", "C_SP": "join_by_link_off", "C_BG": "became_group",
	"BB": "album_renamed", "BD": "album_deleted", "BO": "album_photo_deleted",
}

// eventInfo is the _eventInfo JSON of a _type 3 row. displayNameMap names the
// members involved, including the sender of a joined or left event.
// storedName is the sender name LINE stored with a row: _contentInfo
// fromName, or an event's _eventInfo names; "" when there is none.
func storedName(from string, ci map[string]any, ev eventInfo) string {
	return orElse(metaString(ci, "fromName"), orElse(ev.FromName, ev.Names[from]))
}

type eventInfo struct {
	Type     any               `json:"type"`
	MidList  []string          `json:"midList"`
	Names    map[string]string `json:"displayNameMap"`
	Param    any               `json:"param"`
	FromName string            `json:"fromName"`
}

func parseEvent(msg *Message, r rawMessage, meta map[string]any, ev eventInfo, name func(string) string) {
	person := func(id, known string) Person {
		return Person{ID: id, Name: orElse(known, name(id))}
	}
	if r.ContentType == 18 {
		key := metaString(meta, "LOC_KEY")
		msg.Event = orElse(noticeKeys[key], orElse(key, "notice"))
		args := strings.Split(metaString(meta, "LOC_ARGS"), "\x1e")
		if len(args) < 2 {
			return
		}
		if msg.Event == "renamed" {
			msg.Content = strPtr(args[1])
			return
		}
		for _, a := range args[1:] {
			for _, id := range strings.Split(a, "\x1f") {
				if isMid(id) {
					msg.Targets = append(msg.Targets, person(id, ""))
				}
			}
		}
		return
	}
	code, ok := anyInt(ev.Type)
	switch {
	case !ok:
		msg.Event = "unknown"
	case eventTypes[code] != "":
		msg.Event = eventTypes[code]
	default:
		msg.Event = "type_" + strconv.FormatInt(code, 10)
	}
	for _, id := range ev.MidList {
		if id != "" {
			msg.Targets = append(msg.Targets, person(id, ev.Names[id]))
		}
	}
	if p, _ := ev.Param.(string); p != "" && msg.Event == "renamed" {
		msg.Content = strPtr(p)
	}
}

// parseMentions reads MENTION.MENTIONEES. S and E are UTF-16 offsets into the
// text; that span names a mentioned mid no name table knows.
func parseMentions(meta map[string]any, text string, name func(string) string) ([]Person, bool) {
	var raw []byte
	switch v := meta["MENTION"].(type) {
	case string:
		raw = []byte(v)
	case map[string]any:
		raw, _ = json.Marshal(v)
	default:
		return nil, false
	}
	var m struct {
		Mentionees []struct {
			S, E, A any
			M       string
		} `json:"MENTIONEES"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return nil, false
	}
	var out []Person
	var u16 []uint16
	all := false
	seen := map[string]bool{}
	for _, e := range m.Mentionees {
		if a, ok := anyInt(e.A); ok && a == 1 {
			all = true
			continue
		}
		if e.M == "" || seen[e.M] {
			continue
		}
		seen[e.M] = true
		p := Person{ID: e.M, Name: name(e.M)}
		if p.Name == "" {
			if u16 == nil {
				u16 = utf16.Encode([]rune(text))
			}
			s, ok1 := anyInt(e.S)
			end, ok2 := anyInt(e.E)
			if ok1 && ok2 && 0 <= s && s < end && end <= int64(len(u16)) {
				p.Name = strings.TrimSpace(strings.TrimPrefix(string(utf16.Decode(u16[s:end])), "@"))
			}
		}
		out = append(out, p)
	}
	return out, all
}

// Predefined reaction codes, as used by countByType keys, PredefinedType and
// predefinedReactionType.
var reactionNames = map[int64]string{2: "nice", 3: "love", 4: "fun", 5: "amazing", 6: "sad", 7: "omg"}

func reactionName(code int64) string {
	if n, ok := reactionNames[code]; ok {
		return n
	}
	return "type_" + strconv.FormatInt(code, 10)
}

// parseReactions counts reactions. Open chats keep totals in
// _reactionStatus.countByType; other chats list each reaction in
// _reactionStatus.reactionList (type 1 predefined, 2 paid) or in _reactions.
func parseReactions(status, legacy string) *Reactions {
	by := map[string]int64{}
	var total int64
	add := func(k string, n int64) {
		if n > 0 {
			by[k] += n
			total += n
		}
	}
	if status != "" {
		var rs struct {
			TotalCount  int64            `json:"totalCount"`
			CountByType map[string]int64 `json:"countByType"`
			List        []struct {
				Type       int64 `json:"type"`
				Predefined int64 `json:"PredefinedType"`
				Cancel     any   `json:"cancel"`
			} `json:"reactionList"`
		}
		if json.Unmarshal([]byte(status), &rs) == nil {
			for k, n := range rs.CountByType {
				if code, err := strconv.ParseInt(k, 10, 64); err == nil {
					add(reactionName(code), n)
				}
			}
			if total == 0 {
				for _, e := range rs.List {
					if c, ok := e.Cancel.(bool); ok && c {
						continue
					}
					if c, ok := anyInt(e.Cancel); ok && c != 0 {
						continue
					}
					switch e.Type {
					case 1:
						add(reactionName(e.Predefined), 1)
					case 2:
						add("paid", 1)
					default:
						add("kind_"+strconv.FormatInt(e.Type, 10), 1)
					}
				}
			}
			if rs.TotalCount > total && total > 0 {
				total = rs.TotalCount
			}
		}
	}
	if total == 0 && legacy != "" {
		var rm map[string]struct {
			ReactionType struct {
				Predefined *int64          `json:"predefinedReactionType"`
				Paid       json.RawMessage `json:"paidReactionType"`
			} `json:"reactionType"`
		}
		if json.Unmarshal([]byte(legacy), &rm) == nil {
			for _, e := range rm {
				switch {
				case e.ReactionType.Predefined != nil:
					add(reactionName(*e.ReactionType.Predefined), 1)
				case len(e.ReactionType.Paid) > 0:
					add("paid", 1)
				}
			}
		}
	}
	if total == 0 {
		return nil
	}
	return &Reactions{Total: total, ByType: by}
}

// Call kinds from ct6 metadata TYPE. A group call (G) always has RESULT INFO,
// so its GC_EVT_TYPE S/E becomes started/ended.
var callKinds = map[string]string{"A": "audio", "V": "video", "G": "group"}

func parseCall(meta map[string]any) *Call {
	typ := metaString(meta, "TYPE")
	if typ == "" {
		return nil
	}
	c := &Call{Kind: orElse(callKinds[typ], strings.ToLower(typ)), Result: strings.ToLower(metaString(meta, "RESULT"))}
	if typ == "G" {
		switch metaString(meta, "GC_EVT_TYPE") {
		case "S":
			c.Result = "started"
		case "E":
			c.Result = "ended"
		}
	}
	if d := metaNumber(meta, "DURATION"); d != nil && *d > 0 {
		c.DurationSec = msToSec(*d)
	}
	return c
}

func parseLocation(s string) *GeoLocation {
	if s == "" {
		return nil
	}
	var l struct {
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
		Address   string   `json:"address"`
		Title     string   `json:"title"`
	}
	if json.Unmarshal([]byte(s), &l) != nil || l.Latitude == nil || l.Longitude == nil {
		return nil
	}
	return &GeoLocation{Latitude: *l.Latitude, Longitude: *l.Longitude, Address: l.Address, Title: l.Title}
}

// linkPreviews returns the _urlPreview entries that have a title or description.
func linkPreviews(s string) []LinkPreview {
	if s == "" {
		return nil
	}
	var v struct {
		List []struct {
			URL   string `json:"url"`
			Title string `json:"title"`
			Desc  string `json:"desc"`
		} `json:"urlPreview"`
	}
	if json.Unmarshal([]byte(s), &v) != nil {
		return nil
	}
	var out []LinkPreview
	for _, p := range v.List {
		if p.Title != "" || p.Desc != "" {
			out = append(out, LinkPreview{URL: p.URL, Title: p.Title, Description: p.Desc})
		}
	}
	return out
}
