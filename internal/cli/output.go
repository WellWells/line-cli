package cli

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"

	"github.com/WellWells/line-cli/internal/line"
)

// cols joins fields with tabs, escaping each with escapeText.
func cols(fields ...string) string {
	for i, f := range fields {
		fields[i] = escapeText(f)
	}
	return strings.Join(fields, "\t")
}

// escapeText keeps a --text value on one line, reversibly: backslash, newline,
// tab and CR become \\, \n, \t and \r; other control characters \xNN; NEL and
// the Unicode line and paragraph separators U+0085, U+2028 and U+2029 become
// \u0085, \u2028 and \u2029.
func escapeText(s string) string {
	if !strings.ContainsFunc(s, needsEscape) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case needsEscape(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func needsEscape(r rune) bool {
	return r == '\\' || r < 0x20 || r == 0x7f || r == 0x85 || r == 0x2028 || r == 0x2029
}

func (rc *runCtx) emitChats(v []line.ChatSummary) int {
	if !rc.text {
		return rc.emit(list(v))
	}
	for _, c := range v {
		fmt.Fprintln(rc.out, cols(c.Type, c.Name, c.LastMessageAt, c.ChatID))
	}
	return 0
}

func (rc *runCtx) emitContacts(v []line.ContactSummary) int {
	if !rc.text {
		return rc.emit(list(v))
	}
	for _, c := range v {
		me := ""
		if c.IsMe {
			me = " (me)"
		}
		fmt.Fprintln(rc.out, cols(c.DisplayName, c.ContactID+me))
	}
	return 0
}

// msgLine renders a message as sent_at, id, sender, me-flag ("me" or empty),
// type and content.
func msgLine(m line.Message) string {
	me := ""
	if m.FromMe {
		me = "me"
	}
	return cols(m.SentAt, m.ID, m.Sender, me, m.Type, threadMark(m)+msgText(m))
}

// threadMark prefixes a thread reply, or a message that starts a thread, in
// --text output.
func threadMark(m line.Message) string {
	switch {
	case m.ThreadReplies > 0:
		unit := " replies] "
		if m.ThreadReplies == 1 {
			unit = " reply] "
		}
		return "[thread " + m.ThreadID + ", " + strconv.FormatInt(m.ThreadReplies, 10) + unit
	case m.ThreadID != "":
		return "[in thread " + m.ThreadID + "] "
	}
	return ""
}

func personNames(ps []line.Person) string {
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = cmp.Or(p.Name, p.ID)
	}
	return strings.Join(names, ", ")
}

// msgText is the content column: the message content, or a short summary for
// types that have none.
func msgText(m line.Message) string {
	content := ""
	if m.Content != nil {
		content = *m.Content
	}
	switch {
	case m.Type == "unsent":
		return "[unsent " + m.OriginalType + "]"
	case m.Type == "event":
		return strings.TrimSpace(m.Event+" "+personNames(m.Targets)) + prefixed(": ", content)
	case m.Undecryptable:
		return "[undecryptable]"
	case m.Call != nil:
		s := "[" + m.Call.Kind + " call " + m.Call.Result
		if m.Call.DurationSec > 0 {
			s += fmt.Sprintf(" %.0fs", m.Call.DurationSec)
		}
		return s + "]"
	case m.Location != nil:
		return strings.TrimSpace(content + " " + m.Location.Address + fmt.Sprintf(" (%g,%g)", m.Location.Latitude, m.Location.Longitude))
	case m.Filename != "":
		return m.Filename + prefixed(" ", content)
	case m.URL != "":
		return strings.TrimSpace(m.Title + " " + m.URL)
	}
	return content
}

func prefixed(prefix, s string) string {
	if s == "" {
		return ""
	}
	return prefix + s
}

// senderRef is one output row's sender name and a pointer to its sender_id.
type senderRef struct {
	name string
	id   *string
}

// dropSenderIDs clears sender_id on every row whose sender name belongs to a
// single sender in this output: the name says who it is, and a 33-character id
// on every message is a large share of the output. Rows whose name two senders
// share keep their ids to tell them apart; --ids keeps every id.
func (rc *runCtx) dropSenderIDs(refs []senderRef) {
	if rc.ids {
		return
	}
	first := map[string]string{}
	shared := map[string]bool{}
	for _, r := range refs {
		if id, ok := first[r.name]; !ok {
			first[r.name] = *r.id
		} else if id != *r.id {
			shared[r.name] = true
		}
	}
	for _, r := range refs {
		if !shared[r.name] {
			*r.id = ""
		}
	}
}

func msgRefs(refs []senderRef, v []line.Message) []senderRef {
	for i := range v {
		refs = append(refs, senderRef{v[i].Sender, &v[i].SenderID})
	}
	return refs
}

// dropMessageIDs clears the id of every message that no message in this
// output replies to: reading a conversation takes sender, sent_at and content,
// and an 18-digit id on each message is mostly noise. A replied-to message
// keeps its id so reply_to points at it; --ids keeps every id.
func (rc *runCtx) dropMessageIDs(groups ...[]line.Message) {
	if rc.ids {
		return
	}
	replied := map[string]bool{}
	for _, v := range groups {
		for _, m := range v {
			if m.ReplyTo != "" {
				replied[m.ReplyTo] = true
			}
		}
	}
	for _, v := range groups {
		for i := range v {
			if !replied[v[i].ID] {
				v[i].ID = ""
			}
		}
	}
}

// emitMessages prints read's messages; withIDs keeps every message id, for
// read --around, whose output is found by id.
func (rc *runCtx) emitMessages(v []line.Message, withIDs bool) int {
	if !rc.text {
		rc.dropSenderIDs(msgRefs(nil, v))
		if !withIDs {
			rc.dropMessageIDs(v)
		}
		return rc.emit(list(v))
	}
	for _, m := range v {
		fmt.Fprintln(rc.out, msgLine(m))
	}
	return 0
}

func (rc *runCtx) emitSearch(v []line.SearchHit) int {
	if !rc.text {
		refs := make([]senderRef, len(v))
		for i := range v {
			refs[i] = senderRef{v[i].Sender, &v[i].SenderID}
		}
		rc.dropSenderIDs(refs)
		return rc.emit(list(v))
	}
	for _, h := range v {
		fmt.Fprintln(rc.out, cols(h.ChatID)+"\t"+msgLine(h.Message))
	}
	return 0
}

func (rc *runCtx) emitFiles(v []line.FileHit) int {
	if !rc.text {
		refs := make([]senderRef, len(v))
		for i := range v {
			refs[i] = senderRef{v[i].Sender, &v[i].SenderID}
		}
		rc.dropSenderIDs(refs)
		return rc.emit(list(v))
	}
	for _, f := range v {
		name := f.Filename
		if f.Expired {
			name += " EXPIRED"
		}
		fields := []string{f.SentAt, f.ID, f.ChatName, name}
		switch {
		case f.LocalPath != "":
			fields = append(fields, f.LocalPath)
		case f.ProbableLocalPath != "":
			fields = append(fields, "~"+f.ProbableLocalPath)
		}
		fmt.Fprintln(rc.out, cols(fields...))
	}
	return 0
}

func (rc *runCtx) emitUnread(v []line.UnreadChat) int {
	if !rc.text {
		var refs []senderRef
		groups := make([][]line.Message, len(v))
		for i := range v {
			refs = msgRefs(refs, v[i].Messages)
			groups[i] = v[i].Messages
		}
		rc.dropSenderIDs(refs)
		rc.dropMessageIDs(groups...)
		return rc.emit(list(v))
	}
	for _, u := range v {
		fmt.Fprintln(rc.out, cols("#", u.ChatID, u.Type, u.Name, fmt.Sprintf("unread=%d", u.UnreadCount), fmt.Sprintf("available=%d", u.AvailableCount)))
		for _, m := range u.Messages {
			fmt.Fprintln(rc.out, cols(u.ChatID)+"\t"+msgLine(m))
		}
	}
	return 0
}

func (rc *runCtx) emitPreview(v []line.PreviewResult) int {
	if !rc.text {
		refs := make([]senderRef, len(v))
		for i := range v {
			refs[i] = senderRef{v[i].Sender, &v[i].SenderID}
		}
		rc.dropSenderIDs(refs)
		return rc.emit(list(v))
	}
	for _, f := range v {
		status := f.Status
		if f.Status == "saved" {
			status = fmt.Sprintf("saved %s (%d bytes)", f.SavedAs, f.Bytes)
		}
		fields := []string{f.SentAt, f.ID, f.Sender, f.Type, status}
		if f.Status != "saved" && f.Note != "" {
			fields = append(fields, f.Note)
		}
		fmt.Fprintln(rc.out, cols(fields...))
	}
	return 0
}

func (rc *runCtx) emitDownload(v []line.DownloadResult) int {
	if !rc.text {
		refs := make([]senderRef, len(v))
		for i := range v {
			refs[i] = senderRef{v[i].Sender, &v[i].SenderID}
		}
		rc.dropSenderIDs(refs)
		return rc.emit(list(v))
	}
	for _, d := range v {
		status := d.Status
		if d.Status == "downloaded" {
			status = fmt.Sprintf("downloaded %s (%d bytes)", d.SavedAs, d.Bytes)
		}
		fields := []string{d.SentAt, d.ID, d.Sender, d.Type, status}
		if d.Status != "downloaded" && d.Note != "" {
			fields = append(fields, d.Note)
		}
		fmt.Fprintln(rc.out, cols(fields...))
	}
	return 0
}

func (rc *runCtx) emitSend(p *line.SendPlan) int {
	if !rc.text {
		return rc.emit(p)
	}
	status := "SENT"
	switch {
	case p.DryRun:
		status = "DRY-RUN (not sent)"
	case p.Sent && p.Verified:
		status = "SENT (verified in DB)"
	case p.Sent:
		status = "SENT (not yet confirmed in DB)"
	}
	fmt.Fprintf(rc.out, "%s\nto:   %s (%s)\nfrom: %s\ntext: %s\n", status, p.ToName, p.To, p.From, p.Text)
	if r := p.ReplyTo; r != nil {
		fmt.Fprintf(rc.out, "reply to: %s, %s, %s %s: %s\n", r.ID, r.SentAt, r.Sender, r.Type, r.Content)
	}
	for _, m := range p.Mentions {
		fmt.Fprintf(rc.out, "mention: %s -> %s\n", m.Text, orDash(m.Name, "everyone"))
	}
	fmt.Fprintf(rc.out, "keys: sender=%d receiver=%d\nbody: %d bytes -> %s\n", p.SenderKeyID, p.ReceiverKeyID, p.BodyBytes, p.Endpoint)
	return 0
}

func (rc *runCtx) emitAction(p *line.ActionPlan) int {
	if !rc.text {
		return rc.emit(p)
	}
	status := "DONE (not yet confirmed in DB)"
	switch {
	case p.DryRun:
		status = "DRY-RUN (not sent)"
	case p.Verified:
		status = "DONE (verified in DB)"
	}
	fmt.Fprintf(rc.out, "%s\naction: %s %s\nchat: %s (%s)\n", status, p.Action, p.Reaction, p.ToName, p.To)
	m := p.Message
	fmt.Fprintf(rc.out, "message: %s, %s, %s %s: %s\n", m.ID, m.SentAt, m.Sender, m.Type, m.Content)
	return 0
}

func (rc *runCtx) emitSendFiles(p *line.SendFilesPlan) int {
	if !rc.text {
		return rc.emit(p)
	}
	if p.DryRun {
		fmt.Fprintln(rc.out, "DRY-RUN (not sent)")
	}
	fmt.Fprintf(rc.out, "to:   %s (%s)\nfrom: %s\n", p.ToName, p.To, p.From)
	if m := p.Message; m != nil {
		state := "planned"
		switch {
		case m.Sent && m.Verified:
			state = "sent, verified in DB"
		case m.Sent:
			state = "sent, not yet confirmed in DB"
		}
		fmt.Fprintf(rc.out, "text (%s): %s\n", state, m.Text)
	}
	if r := p.ReplyTo; r != nil {
		fmt.Fprintf(rc.out, "reply to: %s, %s, %s %s: %s\n", r.ID, r.SentAt, r.Sender, r.Type, r.Content)
	}
	for _, f := range p.Files {
		state := f.Status
		if f.Status == "sent" {
			state = "sent, not yet confirmed in DB"
			if f.Verified {
				state = "sent, verified in DB"
			}
		}
		gps := ""
		if f.GPSRemoved {
			gps = ", location removed"
		}
		fmt.Fprintf(rc.out, "%s %s (%s, %d bytes%s): %s\n", f.Kind, f.Name, f.Path, f.Bytes, gps, state)
		if f.Error != "" {
			fmt.Fprintf(rc.out, "  error: %s\n", f.Error)
		}
	}
	if p.Error != "" {
		fmt.Fprintf(rc.out, "stopped: %s: %s\n", p.Error, p.ErrorMessage)
	}
	return 0
}

func list[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

func orDash(s, empty string) string {
	if s == "" {
		return empty
	}
	return s
}
