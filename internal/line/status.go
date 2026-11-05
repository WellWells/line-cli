package line

import (
	"os"
	"strings"
	"time"
)

const maxStaleIDs = 5

type StaleChats struct {
	Count   int      `json:"count"`
	ChatIDs []string `json:"chat_ids"`
}

type Status struct {
	LineRunning     bool        `json:"line_running"`
	DBPath          string      `json:"db_path"`
	DBSize          int64       `json:"db_size"`
	DBModifiedAt    string      `json:"db_modified_at,omitempty"`
	WALModifiedAt   string      `json:"wal_modified_at,omitempty"`
	KeyCached       bool        `json:"key_cached"`
	NewestMessageAt string      `json:"newest_message_at,omitempty"`
	OldestMessageAt string      `json:"oldest_message_at,omitempty"`
	StaleChats      *StaleChats `json:"stale_chats,omitempty"`
	Note            string      `json:"note,omitempty"`
}

// GetStatus reports whether LINE runs and how fresh the local database is.
// It uses the cached key only; it never reads LINE's memory or the network.
func GetStatus() (*Status, error) {
	loc, err := Locate()
	if err != nil {
		return nil, err
	}
	s := &Status{DBPath: loc.MainEdb}
	d := statDBFiles(loc.MainEdb)
	s.DBSize, s.DBModifiedAt, s.WALModifiedAt = d.size, fileTimeISO(d.modTime), fileTimeISO(d.walMod)
	running, perr := LineRunning()
	switch {
	case perr != nil:
		s.Note = "Could not list processes to check for LINE."
	case !running:
		s.Note = "LINE is not running; the database only updates while LINE runs and syncs."
	}
	s.LineRunning = running
	if err := s.fromDB(OpenCached); err != nil {
		return nil, err
	}
	return s, nil
}

// fromDB fills the database fields through open, which yields a nil DB when no
// cached key works. Like withReader it tries once more after a checkpoint or
// a WAL caught mid-write.
func (s *Status) fromDB(open func() (*DB, error)) error {
	for attempt := 0; ; attempt++ {
		db, err := open()
		if err != nil {
			return err
		}
		if db == nil {
			s.Note = strings.TrimSpace(s.Note + " No usable cached key; the next read command scans LINE's memory for it (LINE must be running).")
			return nil
		}
		s.KeyCached = true
		err = NewReader(db).fillStatus(s)
		changed := db.Changed()
		db.Close()
		if le, _ := AsError(err); attempt == 0 && (changed || le != nil && le.Code == ErrDBUnreadable && !le.schema) {
			continue
		}
		return err
	}
}

func (r *Reader) fillStatus(s *Status) error {
	states, err := r.chatStates("")
	if err != nil {
		return err
	}
	opts, err := r.chatOptions()
	if err != nil {
		return err
	}
	var newest, oldest int64
	stale := &StaleChats{ChatIDs: []string{}}
	for _, f := range states {
		newest = max(newest, f.NewestStored)
		if f.OldestStored != 0 && (oldest == 0 || f.OldestStored < oldest) {
			oldest = f.OldestStored
		}
		// Thread chats keep their replies on the server and hidden chats are
		// often cleared, so neither says anything about sync health.
		if !f.Stale() || opts[f.ChatID].hidden || IsThreadChat(f.ChatID) {
			continue
		}
		stale.Count++
		if len(stale.ChatIDs) < maxStaleIDs {
			stale.ChatIDs = append(stale.ChatIDs, f.ChatID)
		}
	}
	s.NewestMessageAt, s.OldestMessageAt, s.StaleChats = tsToIso(newest), tsToIso(oldest), stale
	return nil
}

func fileTimeISO(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02T15:04:05-07:00")
}

// LineRunning reports whether LINE is running: a LINE.exe whose program file
// says it is LINE, so line-cli's own line.exe does not count. It lists
// processes and reads program files' version information, never LINE's memory.
func LineRunning() (bool, error) {
	_, err := findLineProcess()
	if err == nil {
		return true, nil
	}
	if le, ok := AsError(err); ok && le.Code == ErrLineNotRunning {
		return false, nil
	}
	return false, err
}

type dbFiles struct {
	size            int64
	modTime, walMod time.Time
}

func statDBFiles(mainEdb string) dbFiles {
	var d dbFiles
	if fi, err := os.Stat(mainEdb); err == nil {
		d.size, d.modTime = fi.Size(), fi.ModTime()
	}
	if fi, err := os.Stat(mainEdb + "-wal"); err == nil {
		d.walMod = fi.ModTime()
	}
	return d
}

func (d dbFiles) asOf() time.Time {
	if d.walMod.After(d.modTime) {
		return d.walMod
	}
	return d.modTime
}

// StoppedDataAsOf returns the newest database or WAL modification time when
// LINE.exe is not running, since the local data only changes while it runs.
// It returns "" when LINE is running or its state cannot be told.
func StoppedDataAsOf() string {
	if running, err := LineRunning(); err != nil || running {
		return ""
	}
	loc, err := Locate()
	if err != nil {
		return ""
	}
	return orElse(fileTimeISO(statDBFiles(loc.MainEdb).asOf()), "unknown")
}
