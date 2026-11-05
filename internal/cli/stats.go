package cli

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/WellWells/line-cli/internal/line"
)

func (rc *runCtx) stats(args []string) int {
	fs := newFlags("stats")
	since := fs.String("since", "", "")
	until := fs.String("until", "", "")
	by := fs.String("by", "sender", "")
	limit := fs.Int("limit", 0, "")
	events := fs.Bool("include-events", false, "")
	if err := parseArgs(fs, args); err != nil {
		return rc.fail(err)
	}
	chat := positional(fs)
	if chat == "" {
		return rc.fail(invalid("stats requires a chat_id or name"))
	}
	if !slices.Contains(line.StatsGroupings, *by) {
		return rc.fail(invalid("invalid --by: " + *by + " (valid: " + strings.Join(line.StatsGroupings, ", ") + ")"))
	}
	if err := checkLimit("--limit", *limit); err != nil {
		return rc.fail(err)
	}
	st, ut, err := parseRange(*since, *until)
	if err != nil {
		return rc.fail(err)
	}
	var res *line.StatsResult
	var fr *line.Freshness
	err = rc.withReader(func(r *line.Reader) (e error) {
		if res, e = r.Stats(line.StatsOptions{ChatArg: chat, SinceTs: st, UntilTs: ut, By: *by, Limit: *limit, IncludeEvents: *events}); e != nil {
			return
		}
		fr, e = r.ChatFreshness(res.ChatID)
		return
	})
	if err != nil {
		return rc.fail(err)
	}
	rc.noteResolved(chat, line.ChatSummary{ChatID: res.ChatID, Name: res.ChatName, Type: res.ChatType})
	rc.noteStale(fr, ut, hintRead)
	if res.Total == 0 {
		rc.noteHistory(fr, nil, st, ut, hintRead)
	}
	rc.sum.Count, rc.sum.counted = len(res.Groups), true
	rc.sum.FirstSentAt, rc.sum.LastSentAt = res.FirstSentAt, res.LastSentAt
	if res.GroupCount > len(res.Groups) {
		rc.sum.LimitReached = true
		rc.sum.Next = "rerun with --limit " + strconv.Itoa(min(res.GroupCount, 5000)) + " to get every group"
		rc.notify(fmt.Sprintf("note: returned %d of %d groups; raise --limit (max 5000) to get them all", len(res.Groups), res.GroupCount))
	}
	return rc.emitStats(res)
}

func (rc *runCtx) emitStats(s *line.StatsResult) int {
	if !rc.text {
		return rc.emit(s)
	}
	fmt.Fprintln(rc.out, cols(s.ChatName, s.ChatID, "by="+s.By, fmt.Sprintf("total=%d", s.Total), fmt.Sprintf("groups=%d", s.GroupCount)))
	for _, g := range s.Groups {
		key := []string{g.Day + g.Type}
		switch {
		case g.Hour != nil:
			key = []string{fmt.Sprintf("%02d", *g.Hour)}
		case s.By == "sender":
			id := g.SenderID
			if g.FromMe {
				id += " (me)"
			}
			key = []string{g.Sender, id}
		}
		fmt.Fprintln(rc.out, cols(append(key, strconv.FormatInt(g.Count, 10), g.FirstSentAt, g.LastSentAt)...))
	}
	return 0
}
