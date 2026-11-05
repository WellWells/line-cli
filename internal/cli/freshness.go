package cli

import (
	"fmt"
	"strings"

	"github.com/WellWells/line-cli/internal/line"
)

// edition is the build the notes speak for (a test seam).
var edition = line.Edition

// serverHint says how to get, from LINE's server, what a note says this PC
// does not store (open chats and threads only).
type serverHint int

const (
	hintRead    serverHint = iota // another command: line read gets it
	hintOffline                   // --offline was given: without it, line-cli gets it
	hintNone                      // line-cli tried LINE's server; its own note says why it failed
)

// serverAdvice is the clause after a note about what this PC does not store.
func serverAdvice(chatID string, h serverHint) string {
	if !line.IsThreadChat(chatID) && !line.IsOpenChat(chatID) {
		return ""
	}
	what := "them"
	if line.IsOpenChat(chatID) {
		what = "the missing messages"
	}
	if edition == "open" { // this build cannot, whatever the hint
		return "the official prebuilt line-cli reads " + what + " from LINE's server"
	}
	switch h {
	case hintRead:
		return "'line read " + chatID + "' reads " + what + " from LINE's server"
	case hintOffline:
		return "without --offline, line-cli reads " + what + " from LINE's server"
	}
	return ""
}

func staleNote(chatID, activity, newest string, h serverHint) string {
	if activity == "" {
		activity = "a message that is not stored"
	}
	stored := "no stored messages"
	if newest != "" {
		stored = "the newest stored message is " + newest
	}
	advice := serverAdvice(chatID, h)
	if line.IsThreadChat(chatID) {
		n := fmt.Sprintf("note: thread chat %s has activity up to %s but %s; thread replies are not stored on this PC", chatID, activity, stored)
		if advice != "" {
			n += "; " + advice
		}
		return n
	}
	n := fmt.Sprintf("note: chat %s has activity up to %s but %s; open the chat in LINE to sync", chatID, activity, stored)
	if advice != "" {
		n += ", or " + advice
	}
	return n
}

// noteStale warns when a range ending at untilTs (0 = now) reaches into
// activity LINE knows of but has not stored.
func (rc *runCtx) noteStale(f *line.Freshness, untilTs int64, h serverHint) {
	if f != nil && f.StaleFor(untilTs) {
		rc.notify(staleNote(f.ChatID, line.TimeISO(f.LastActivity), line.TimeISO(f.NewestStored), h))
	}
}

// historyNote says what span of a chat is stored when read came back empty,
// or that read reached the oldest stored message from an earlier start. For a
// thread chat it adds that LINE for PC does not store thread replies.
func historyNote(f *line.Freshness, msgs []line.Message, sinceTs, untilTs int64, h serverHint) string {
	n := storedSpanNote(f, msgs, sinceTs, untilTs)
	if n != "" && line.IsThreadChat(f.ChatID) {
		n += "; LINE for PC does not store thread replies"
		if advice := serverAdvice(f.ChatID, h); advice != "" {
			n += ": " + advice
		}
	}
	return n
}

func storedSpanNote(f *line.Freshness, msgs []line.Message, sinceTs, untilTs int64) string {
	if f == nil {
		return ""
	}
	if f.OldestStored == 0 {
		if f.StaleFor(untilTs) {
			return "" // the stale note already says nothing is stored
		}
		return fmt.Sprintf("note: chat %s has no messages stored on this PC", f.ChatID)
	}
	oldest := line.TimeISO(f.OldestStored)
	if len(msgs) == 0 {
		n := fmt.Sprintf("note: chat %s has no stored messages in this range; its stored messages span %s to %s", f.ChatID, oldest, line.TimeISO(f.NewestStored))
		if untilTs != 0 && untilTs*1000+999 < f.OldestStored {
			n += "; older history is not on this PC"
		}
		return n
	}
	if sinceTs != 0 && sinceTs >= f.OldestStored/1000 {
		return ""
	}
	for _, m := range msgs {
		if m.ID == f.OldestID {
			return fmt.Sprintf("note: reached the oldest message stored for chat %s (%s); older history is not on this PC", f.ChatID, oldest)
		}
	}
	return ""
}

func (rc *runCtx) noteHistory(f *line.Freshness, msgs []line.Message, sinceTs, untilTs int64, h serverHint) {
	if n := historyNote(f, msgs, sinceTs, untilTs, h); n != "" {
		rc.notify(n)
	}
}

// noteThreadsNotRead says what a read of an open chat from this PC alone left
// out: stdout looks complete, so whoever reads it would not know.
func (rc *runCtx) noteThreadsNotRead(chatID string, res line.ReadResult, h serverHint) {
	if res.ThreadsNotRead == 0 {
		return
	}
	n := fmt.Sprintf("note: the output has no replies from %d thread(s) active in this span (%d replies by LINE's count): LINE for PC does not store thread replies", res.ThreadsNotRead, res.RepliesNotRead)
	if advice := serverAdvice(chatID, h); advice != "" {
		n += "; " + advice
	}
	rc.notify(n)
}

// noteOnline says what read or unread got from LINE's server, and what it
// could not get.
func (rc *runCtx) noteOnline(rep line.OnlineReport) {
	if rep.Unavailable != "" {
		rc.notify("note: LINE's server could not be reached, so the output has only what this PC stores: " + rep.Unavailable)
	}
	for _, f := range rep.Failed {
		rc.notify("note: LINE's server did not return " + f)
	}
	for _, c := range rep.Cut {
		if line.IsThreadChat(c) {
			rc.notify("note: not every reply was read from LINE's server for thread " + c + "; narrow --since")
		} else {
			rc.notify("note: not every message this PC lacks was read from LINE's server for " + c + "; open the chat in LINE to sync it")
		}
	}
	if rep.ShortThreads > 0 {
		rc.notify(fmt.Sprintf("note: LINE's server has %d fewer replies than LINE counts in %d thread(s): it no longer keeps replies older than about 5 months, and LINE's count can include replies that were removed", rep.ShortReplies, rep.ShortThreads))
	}
	if rep.ThreadsLeft > 0 {
		rc.notify(fmt.Sprintf("note: %d more thread(s) may have replies in this span and were not read; narrow --since/--until or raise --max-threads (at most %d)", rep.ThreadsLeft, line.MaxReadThreads))
	}
	if rep.Calls == 0 {
		return
	}
	var got []string
	if rep.Missing > 0 {
		got = append(got, fmt.Sprintf("%d message(s) this PC does not store", rep.Missing))
	}
	if rep.Threads > 0 {
		got = append(got, fmt.Sprintf("the replies of %d thread(s)", rep.Threads))
	}
	if len(got) == 0 {
		got = append(got, "nothing new")
	}
	rc.notify(fmt.Sprintf("note: read %s from LINE's server in %d call(s); search and stats do not see them", strings.Join(got, " and "), rep.Calls))
}
