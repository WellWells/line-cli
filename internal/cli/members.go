package cli

import (
	"fmt"
	"strings"

	"github.com/WellWells/line-cli/internal/line"
)

func invalidChatType(v string) error {
	return invalid("invalid --chat-type: " + v + " (valid: " + strings.Join(line.ChatTypeNames(), ", ") + ")")
}

// noteResolved tells which chat a name resolved to, so a substring match that
// picked an unexpected chat is visible.
func (rc *runCtx) noteResolved(arg string, c line.ChatSummary) {
	if arg = strings.TrimSpace(arg); arg != c.ChatID {
		rc.notify(fmt.Sprintf("note: resolved \"%s\" -> %s (%s, %s)", arg, c.ChatID, c.Type, c.Name))
	}
}

func (rc *runCtx) members(args []string) int {
	fs := newFlags("members")
	limit := fs.Int("limit", 0, "")
	if err := parseArgs(fs, args); err != nil {
		return rc.fail(err)
	}
	chat := positional(fs)
	if chat == "" {
		return rc.fail(invalid("members requires a chat_id or name"))
	}
	if err := checkLimit("--limit", *limit); err != nil {
		return rc.fail(err)
	}
	var res *line.MemberList
	err := rc.withReader(func(r *line.Reader) (e error) {
		res, e = r.Members(line.MembersOptions{ChatArg: chat, Limit: *limit})
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	rc.noteResolved(chat, res.Chat)
	if res.Note != "" {
		rc.notify("note: " + res.Note)
	}
	if res.Total > len(res.Members) {
		rc.sum.LimitReached, rc.sum.Next = true, "raise --limit (max 5000)"
		rc.notify(fmt.Sprintf("note: returned %d of %d members; raise --limit (max 5000)", len(res.Members), res.Total))
	}
	return rc.emitMembers(res.Members)
}

func (rc *runCtx) emitMembers(v []line.Member) int {
	if !rc.text {
		return rc.emit(list(v))
	}
	for _, m := range v {
		me := ""
		if m.IsMe {
			me = " (me)"
		}
		fmt.Fprintln(rc.out, cols(m.Name+me, m.ID, m.Role, m.State, fmt.Sprintf("messages=%d", m.MessageCount), m.LastSentAt))
	}
	return 0
}
