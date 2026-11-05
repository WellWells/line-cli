package line

import (
	"fmt"
	"time"
)

const (
	watchBatch = 1000 // rows read in one pass
	// watchLookback is how far before the start a message may be dated and
	// still count: LINE stores some a moment late, but rows much older are
	// history it loaded when a chat was scrolled, or old rows it rewrote.
	watchLookback = 10 * time.Minute
	// watchSealGrace is how long sealed messages wait for LINE to store their text.
	watchSealGrace = 5 * time.Second
	// watchRecheck re-reads the database even if its files look unchanged.
	watchRecheck     = 30 * time.Second
	watchMaxFailures = 30 // passes in a row that may fail while LINE writes
)

// WatchOptions says what Watch follows. Emit gets each new message and returns
// false to end the watch; Stop ends it too.
type WatchOptions struct {
	ChatID          string // "" follows every chat
	IncludeOfficial bool
	Interval        time.Duration
	Stop            <-chan struct{}
	Emit            func(SearchHit) bool
}

type watcher struct {
	opt     WatchOptions
	cursor  int64 // rows up to this rowid are done
	sinceMs int64
	seen    map[string]bool
	// holdStart is when the pass first stopped at a sealed row; zero when no
	// row waits. Everything behind that row waits with it, so the wait is
	// shared: a run of sealed rows costs one grace, not one each.
	holdStart time.Time
}

// Watch follows the local database and hands over messages as LINE stores
// them. It only reads this PC's data: what LINE PC does not store (thread
// replies, open chats it has not synced) never appears.
func (r *Reader) Watch(opt WatchOptions) error {
	w, err := r.newWatcher(opt, time.Now())
	if err != nil {
		return err
	}
	pass := func(now time.Time) (bool, error) {
		db, err := r.db.Reopen()
		if err != nil {
			return true, err
		}
		defer db.Close()
		return w.poll(NewReader(db), now)
	}
	return w.loop(pass, func() string { return changeStamp(r.db.path) })
}

func (r *Reader) newWatcher(opt WatchOptions, now time.Time) (*watcher, error) {
	cursor, err := r.lastRowID()
	if err != nil {
		return nil, err
	}
	return &watcher{
		opt: opt, cursor: cursor, sinceMs: now.Add(-watchLookback).UnixMilli(),
		seen: map[string]bool{},
	}, nil
}

func (r *Reader) lastRowID() (int64, error) {
	st, err := r.query("SELECT COALESCE(MAX(rowid), 0) FROM _message")
	if err != nil || st == nil {
		return 0, err
	}
	defer st.Close()
	var id int64
	if st.Step() {
		id = st.ColumnInt64(0)
	}
	return id, stepErr(st)
}

// changeStamp changes whenever LINE commits: the -shm header holds the WAL's
// change counter and frame count, which readStamp does not cover. Reading it
// costs far less than opening the database.
func changeStamp(mainEdb string) string {
	return fmt.Sprint(readStamp(mainEdb), readHead(mainEdb+"-shm", 48))
}

func (w *watcher) loop(pass func(time.Time) (bool, error), stamp func() string) error {
	last, lastPass := "", time.Time{}
	failures := 0
	for {
		select {
		case <-w.opt.Stop:
			return nil
		case <-time.After(w.opt.Interval):
		}
		now := time.Now()
		cur := stamp()
		if cur == last && now.Sub(lastPass) < watchRecheck && w.holdStart.IsZero() {
			continue
		}
		more, err := pass(now)
		if err != nil {
			if failures++; failures >= watchMaxFailures {
				return err
			}
			continue
		}
		failures, last, lastPass = 0, cur, now
		if !more {
			return nil
		}
	}
}

// poll hands over the rows after the cursor and reports whether to go on.
func (w *watcher) poll(r *Reader, now time.Time) (bool, error) {
	last, err := r.lastRowID()
	if err != nil || last <= w.cursor {
		return true, err
	}
	if err := r.buildMaps(); err != nil {
		return true, err
	}
	if err := r.buildContacts(); err != nil {
		return true, err
	}
	if err := r.buildOwnMids(); err != nil {
		return true, err
	}
	sql := "SELECT rowid AS _rowid, * FROM _message WHERE rowid > ?"
	args := []any{w.cursor}
	if w.opt.ChatID != "" {
		sql += " AND _chatId = ?"
		args = append(args, w.opt.ChatID)
	}
	st, err := r.query(sql+" ORDER BY rowid LIMIT "+fmt.Sprint(watchBatch), args...)
	if err != nil || st == nil {
		return true, err
	}
	defer st.Close()
	idx := colIndex(st)
	col := func(name string) int {
		if i, ok := idx[name]; ok {
			return i
		}
		return -1
	}
	rows := 0
	for st.Step() {
		rows++
		rowid, id, chatID := st.ColumnInt64(col("_rowid")), st.ColumnText(col("_id")), st.ColumnText(col("_chatId"))
		created := st.ColumnInt64(col("_createdTime"))
		if w.seen[id] || created < w.sinceMs {
			w.cursor = rowid
			continue
		}
		info := r.maps.resolveChat(chatID)
		if info.typ == "official" && !w.opt.IncludeOfficial && w.opt.ChatID == "" {
			w.cursor = rowid
			continue
		}
		m := r.readMessageRow(st, idx)
		if m.Undecryptable {
			if w.holdStart.IsZero() {
				w.holdStart = now
			}
			// A message newer than the wait gets a whole grace of its own.
			if now.Before(laterOf(w.holdStart, time.UnixMilli(created)).Add(watchSealGrace)) {
				return true, stepErr(st) // the next pass starts at this row
			}
		}
		w.cursor = rowid
		w.seen[id] = true
		if !w.opt.Emit(SearchHit{ChatID: chatID, ChatName: info.name, ChatType: info.typ, Message: m}) {
			return false, nil
		}
	}
	if err := stepErr(st); err != nil {
		return true, err
	}
	w.holdStart = time.Time{}
	if rows < watchBatch {
		w.cursor = last // rows of other chats need no second look
	}
	return true, nil
}

func laterOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
