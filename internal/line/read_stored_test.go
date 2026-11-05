package line

import "testing"

// A read from this PC alone counts the threads whose replies it leaves out:
// those with replies that may fall in the span, on the page --limit cut.
func TestReadStoredCountsThreadsNotRead(t *testing.T) {
	d := outTestDB(t)
	d.insert("_chat", map[string]any{"_id": "m1", "_midType": 4, "_lastUpdatedTime": int64(90_000), "_unreadCount": 0})
	d.msg("a", "m1", 10_000, 0, nil)
	d.msg("b", "m1", 20_000, 0, nil)
	d.msg("c", "m1", 30_000, 0, nil)
	thread := func(id, root string, replies, last int64) {
		d.insert("_squareThread", map[string]any{"_threadMid": id, "_chatMid": "m1", "_messageId": root, "_totalMessageCount": replies, "_lastMessageAt": last})
	}
	thread("t-old", "a", 4, 11_000)       // replies end before the newest messages
	thread("t-mid", "b", 3, 40_000)       // started on the page, replies after it
	thread("t-new", "c", 2, 35_000)       // started on the page
	thread("t-empty", "c", 0, 0)          // no replies at all
	thread("t-after", "later", 5, 50_000) // its root is not stored
	chat := ChatSummary{ChatID: "m1", Type: "open"}

	for _, c := range []struct {
		name             string
		opt              ReadOptions
		threads, replies int64
	}{
		{"every message", ReadOptions{}, 4, 14},
		{"--since after the old thread ended", ReadOptions{SinceTs: 15}, 3, 10},
		{"--until before the later threads started", ReadOptions{UntilTs: 15}, 2, 9},
		{"--limit keeps the newest two", ReadOptions{Limit: 2}, 3, 10},
		{"--since --limit keeps the first", ReadOptions{SinceTs: 1, Limit: 1}, 2, 9},
	} {
		res, err := d.r.ReadChat(chat, c.opt)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if res.ThreadsNotRead != c.threads || res.RepliesNotRead != c.replies {
			t.Errorf("%s: %d thread(s), %d replies; want %d, %d", c.name, res.ThreadsNotRead, res.RepliesNotRead, c.threads, c.replies)
		}
	}
	// Only an open chat has threads.
	d.chat("c1group")
	d.msg("g", "c1group", 10_000, 0, nil)
	if res, err := d.r.ReadChat(ChatSummary{ChatID: "c1group", Type: "group"}, ReadOptions{}); err != nil || res.ThreadsNotRead != 0 {
		t.Errorf("group: %+v, %v", res, err)
	}
}
