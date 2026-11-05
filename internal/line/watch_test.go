package line

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

// All data here is synthetic.

const watchNow = int64(1700000000000)

func watchRow(id, chat, from string, created int64, text string) string {
	return "INSERT INTO _message VALUES ('" + id + "', '" + chat + "', '" + from + "', " + itoa64(created) + ", '" + text + "', 0, '', '', 1, 0)"
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// watchFixture starts a watcher at the end of the stored messages and collects
// what it emits.
func watchFixture(t *testing.T, opt WatchOptions) (*Reader, *watcher, *[]SearchHit) {
	t.Helper()
	r := testReader(t)
	var got []SearchHit
	opt.Emit = func(h SearchHit) bool {
		got = append(got, h)
		return true
	}
	w, err := r.newWatcher(opt, time.UnixMilli(watchNow))
	if err != nil {
		t.Fatal(err)
	}
	return r, w, &got
}

func execSQL(t *testing.T, r *Reader, sql string) {
	t.Helper()
	if err := r.db.conn.Exec(sql); err != nil {
		t.Fatal(err)
	}
}

func poll(t *testing.T, w *watcher, r *Reader) {
	t.Helper()
	if _, err := w.poll(r, time.UnixMilli(watchNow)); err != nil {
		t.Fatal(err)
	}
}

func TestWatchIgnoresWhatWasAlreadyThere(t *testing.T) {
	r, w, got := watchFixture(t, WatchOptions{})
	poll(t, w, r)
	if len(*got) != 0 {
		t.Fatalf("got %+v", *got)
	}
}

func TestWatchEmitsNewMessagesInOrder(t *testing.T) {
	r, w, got := watchFixture(t, WatchOptions{})
	execSQL(t, r, watchRow("501", group, alice, watchNow+1, "first"))
	execSQL(t, r, watchRow("502", alice, alice, watchNow+2, "second"))
	poll(t, w, r)
	if len(*got) != 2 {
		t.Fatalf("got %+v", *got)
	}
	h := (*got)[0]
	if h.ID != "501" || h.ChatID != group || h.ChatName != "Family" || h.ChatType != "group" || h.Content == nil || *h.Content != "first" || h.Sender != "Ali" {
		t.Errorf("hit %+v", h)
	}
	if (*got)[1].ID != "502" {
		t.Errorf("order: %+v", (*got)[1])
	}
	poll(t, w, r)
	if len(*got) != 2 {
		t.Errorf("repeated: %d", len(*got))
	}
}

// LINE rewrites a row (a new rowid, same id) when it changes it.
func TestWatchEmitsARewrittenRowOnce(t *testing.T) {
	r, w, got := watchFixture(t, WatchOptions{})
	execSQL(t, r, watchRow("501", group, alice, watchNow+1, "first"))
	poll(t, w, r)
	execSQL(t, r, "DELETE FROM _message WHERE _id='501'")
	execSQL(t, r, watchRow("501", group, alice, watchNow+1, "first"))
	poll(t, w, r)
	if len(*got) != 1 {
		t.Fatalf("got %d", len(*got))
	}
}

// Scrolling back in LINE loads old history into new rows, and a reaction can
// rewrite an old row; neither is a new message.
func TestWatchSkipsOldHistory(t *testing.T) {
	r, w, got := watchFixture(t, WatchOptions{})
	execSQL(t, r, watchRow("501", group, alice, watchNow-int64(watchLookback/time.Millisecond)-1, "old"))
	execSQL(t, r, watchRow("502", group, alice, watchNow-1000, "just before the start"))
	poll(t, w, r)
	if len(*got) != 1 || (*got)[0].ID != "502" {
		t.Fatalf("got %+v", *got)
	}
}

func TestWatchOneChat(t *testing.T) {
	r, w, got := watchFixture(t, WatchOptions{ChatID: group})
	execSQL(t, r, watchRow("501", alice, alice, watchNow+1, "other chat"))
	execSQL(t, r, watchRow("502", group, alice, watchNow+2, "this chat"))
	poll(t, w, r)
	if len(*got) != 1 || (*got)[0].ID != "502" {
		t.Fatalf("got %+v", *got)
	}
}

func TestWatchLeavesOutOfficialAccounts(t *testing.T) {
	for _, c := range []struct {
		opt  WatchOptions
		want int
	}{
		{WatchOptions{}, 0},
		{WatchOptions{IncludeOfficial: true}, 1},
		{WatchOptions{ChatID: oa8}, 1},
	} {
		r, w, got := watchFixture(t, c.opt)
		execSQL(t, r, watchRow("501", oa8, oa8, watchNow+1, "ad"))
		poll(t, w, r)
		if len(*got) != c.want {
			t.Errorf("%+v: got %d", c.opt, len(*got))
		}
	}
}

// LINE stores a sealed row first and its text a moment later: wait briefly so
// the text comes out, keeping the order, but never forever.
func TestWatchWaitsBrieflyForSealedText(t *testing.T) {
	r, w, got := watchFixture(t, WatchOptions{})
	execSQL(t, r, "ALTER TABLE _message ADD COLUMN _chunks TEXT")
	execSQL(t, r, "INSERT INTO _message VALUES ('501', '"+group+"', '"+alice+"', "+itoa64(watchNow+1)+", '', 0, '', '', 1, 0, 'sealed')")
	execSQL(t, r, "INSERT INTO _message VALUES ('502', '"+group+"', '"+alice+"', "+itoa64(watchNow+2)+", 'plain', 0, '', '', 1, 0, NULL)")
	poll(t, w, r)
	if len(*got) != 0 {
		t.Fatalf("emitted before the grace ended: %+v", *got)
	}
	execSQL(t, r, "UPDATE _message SET _text='opened' WHERE _id='501'")
	poll(t, w, r)
	if len(*got) != 2 || *(*got)[0].Content != "opened" || (*got)[0].Undecryptable {
		t.Fatalf("got %+v", *got)
	}
}

func TestWatchGivesUpOnSealedText(t *testing.T) {
	r, w, got := watchFixture(t, WatchOptions{})
	execSQL(t, r, "ALTER TABLE _message ADD COLUMN _chunks TEXT")
	execSQL(t, r, "INSERT INTO _message VALUES ('501', '"+group+"', '"+alice+"', "+itoa64(watchNow+1)+", '', 0, '', '', 1, 0, 'sealed')")
	poll(t, w, r)
	if _, err := w.poll(r, time.UnixMilli(watchNow).Add(watchSealGrace+time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 || !(*got)[0].Undecryptable {
		t.Fatalf("got %+v", *got)
	}
}

// A run of messages LINE cannot open waits one grace in all, not one each.
func TestWatchSealedBacklogWaitsOnce(t *testing.T) {
	r, w, got := watchFixture(t, WatchOptions{})
	execSQL(t, r, "ALTER TABLE _message ADD COLUMN _chunks TEXT")
	for _, id := range []string{"501", "502", "503"} {
		execSQL(t, r, "INSERT INTO _message VALUES ('"+id+"', '"+group+"', '"+alice+"', "+itoa64(watchNow-1000)+", '', 0, '', '', 1, 0, 'sealed')")
	}
	poll(t, w, r)
	if _, err := w.poll(r, time.UnixMilli(watchNow).Add(watchSealGrace+time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 3 {
		t.Fatalf("got %d", len(*got))
	}
}

// A sealed message that arrives right after a wait ended gets its own.
func TestWatchNewSealedMessageGetsItsOwnWait(t *testing.T) {
	r, w, got := watchFixture(t, WatchOptions{})
	execSQL(t, r, "ALTER TABLE _message ADD COLUMN _chunks TEXT")
	later := time.UnixMilli(watchNow).Add(watchSealGrace + time.Second)
	execSQL(t, r, "INSERT INTO _message VALUES ('501', '"+group+"', '"+alice+"', "+itoa64(watchNow+1)+", '', 0, '', '', 1, 0, 'sealed')")
	poll(t, w, r)
	execSQL(t, r, "INSERT INTO _message VALUES ('502', '"+group+"', '"+alice+"', "+itoa64(later.UnixMilli())+", '', 0, '', '', 1, 0, 'sealed')")
	if _, err := w.poll(r, later); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 || (*got)[0].ID != "501" {
		t.Fatalf("got %+v", *got)
	}
}

func TestWatchEmitCanEndIt(t *testing.T) {
	r := testReader(t)
	n := 0
	w, err := r.newWatcher(WatchOptions{Emit: func(SearchHit) bool { n++; return false }}, time.UnixMilli(watchNow))
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, r, watchRow("501", group, alice, watchNow+1, "a"))
	execSQL(t, r, watchRow("502", group, alice, watchNow+2, "b"))
	more, err := w.poll(r, time.UnixMilli(watchNow))
	if err != nil || more || n != 1 {
		t.Fatalf("more %v err %v n %d", more, err, n)
	}
}

func TestWatchLoopStops(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	w := &watcher{opt: WatchOptions{Interval: time.Millisecond, Stop: stop}}
	pass := func(time.Time) (bool, error) { return true, errors.New("not reached") }
	if err := w.loop(pass, func() string { return "" }); err != nil {
		t.Fatal(err)
	}
}

func TestWatchLoopEndsWhenAPassSaysSo(t *testing.T) {
	w := &watcher{opt: WatchOptions{Interval: time.Millisecond}}
	calls := 0
	pass := func(time.Time) (bool, error) { calls++; return false, nil }
	if err := w.loop(pass, func() string { return "s" }); err != nil || calls != 1 {
		t.Fatalf("err %v after %d", err, calls)
	}
}

// A briefly unreadable database is normal while LINE writes; one that stays
// unreadable ends the watch with its error.
func TestWatchLoopGivesUpOnADeadDatabase(t *testing.T) {
	w := &watcher{opt: WatchOptions{Interval: time.Millisecond}}
	calls := 0
	dead := errors.New("gone")
	pass := func(time.Time) (bool, error) { calls++; return true, dead }
	err := w.loop(pass, func() string { return "same" })
	if !errors.Is(err, dead) || calls != watchMaxFailures {
		t.Fatalf("err %v after %d", err, calls)
	}
}

func TestWatchLoopRecoversFromAFailedPass(t *testing.T) {
	w := &watcher{opt: WatchOptions{Interval: time.Millisecond}}
	calls := 0
	pass := func(time.Time) (bool, error) {
		calls++
		switch {
		case calls > 2*watchMaxFailures:
			return false, nil
		case calls%2 == 0:
			return true, nil
		}
		return true, errors.New("busy")
	}
	stamp := 0
	if err := w.loop(pass, func() string { stamp++; return itoa64(int64(stamp)) }); err != nil {
		t.Fatal(err)
	}
}

// While a sealed message waits, the loop looks again even if the disk is quiet.
func TestWatchLoopKeepsLookingDuringAWait(t *testing.T) {
	w := &watcher{opt: WatchOptions{Interval: time.Millisecond}}
	calls := 0
	pass := func(now time.Time) (bool, error) {
		calls++
		w.holdStart = now
		return calls < 3, nil
	}
	if err := w.loop(pass, func() string { return "same" }); err != nil || calls != 3 {
		t.Fatalf("err %v after %d", err, calls)
	}
}

// Once a pass read the current state, an unchanged disk needs no other.
func TestWatchLoopPassesOnlyOnChange(t *testing.T) {
	stop := make(chan struct{})
	w := &watcher{opt: WatchOptions{Interval: time.Millisecond, Stop: stop}}
	checks, calls := 0, 0
	pass := func(time.Time) (bool, error) { calls++; return true, nil }
	err := w.loop(pass, func() string {
		if checks++; checks == 20 {
			close(stop)
		}
		if checks >= 10 {
			return "changed"
		}
		return "same"
	})
	if err != nil || calls != 2 {
		t.Fatalf("err %v, %d passes", err, calls)
	}
}
