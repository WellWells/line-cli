package line

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// duplicateWindow is how long an identical send to the same chat is refused.
const duplicateWindow = 10 * time.Minute

// resultPending is logged just before the POST; a later line with the same
// client_message_id records the outcome. A PENDING line with no outcome means
// the process stopped while the message may have been on its way.
const resultPending = "PENDING"

// sendLogEntry is one line of send-log.jsonl. It records who and when, and a
// hash of the text; never the text itself.
type sendLogEntry struct {
	Time string `json:"time"`
	To   string `json:"to"`
	// Action and MessageID name a reaction or unsend, which has no text.
	Action          string `json:"action,omitempty"`
	MessageID       string `json:"message_id,omitempty"`
	SHA256          string `json:"sha256"`
	Length          int    `json:"length"`
	Result          string `json:"result"`
	MaybeDelivered  bool   `json:"maybe_delivered,omitempty"`
	ServerMessageID string `json:"server_message_id,omitempty"`
	ClientMessageID string `json:"client_message_id,omitempty"`
}

// mayHaveSent reports whether the logged attempt was, or may have been,
// delivered.
func (e sendLogEntry) mayHaveSent() bool {
	return e.Result == "OK" || e.Result == string(ErrSendUncertain) || e.Result == resultPending || e.MaybeDelivered
}

func (e sendLogEntry) outcome() string {
	switch {
	case e.Result == "OK":
		return "result OK"
	case e.Result == resultPending:
		return "interrupted before LINE answered, so it may have been delivered"
	}
	return "result " + e.Result + ", so it may have been delivered"
}

func duplicateError(dup sendLogEntry) error {
	return &Error{Code: ErrDuplicateSend, Message: "the same text was already sent, or possibly sent, to " + dup.To + " at " + dup.Time + " (" + dup.outcome() + "); check with 'line read " + dup.To + "' and add --allow-duplicate only if you really mean to send it again"}
}

func sendLogPath() string { return filepath.Join(storeDir(), "send-log.jsonl") }

func appendSendLog(e sendLogEntry) error {
	if e.Time == "" {
		e.Time = time.Now().Format(time.RFC3339)
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(storeDir(), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(sendLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// recentDuplicate returns the newest logged send of the same text to the same
// chat within duplicateWindow that was, or may have been, delivered. A line
// with an outcome replaces the PENDING line of the same client_message_id.
func recentDuplicate(to, sum string, now time.Time) (sendLogEntry, bool) {
	f, err := os.Open(sendLogPath())
	if err != nil {
		return sendLogEntry{}, false
	}
	defer f.Close()
	var entries []sendLogEntry
	byClient := map[string]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		var e sendLogEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.To != to || e.SHA256 != sum {
			continue
		}
		if i, ok := byClient[e.ClientMessageID]; ok && e.ClientMessageID != "" {
			entries[i] = e
			continue
		}
		byClient[e.ClientMessageID] = len(entries)
		entries = append(entries, e)
	}
	var hit sendLogEntry
	found := false
	for _, e := range entries {
		if !e.mayHaveSent() {
			continue
		}
		t, err := time.Parse(time.RFC3339, e.Time)
		if err != nil || now.Sub(t) > duplicateWindow {
			continue
		}
		hit, found = e, true
	}
	return hit, found
}

// claimSend re-checks the duplicate guard and logs e as PENDING under a lock,
// so a concurrent identical send is refused, and so is a later one if this
// process dies before LINE answers. On error nothing has been sent.
func claimSend(e sendLogEntry, allowDuplicate bool) error {
	unlock, err := lockSendLog()
	if err != nil {
		return &Error{Code: ErrSendFailed, Message: "not sent: could not lock the send log: " + err.Error()}
	}
	defer unlock()
	if !allowDuplicate {
		if dup, ok := recentDuplicate(e.To, e.SHA256, time.Now()); ok {
			return duplicateError(dup)
		}
	}
	e.Result, e.Time = resultPending, ""
	if err := appendSendLog(e); err != nil {
		return &Error{Code: ErrSendFailed, Message: "not sent: could not write the send log: " + err.Error()}
	}
	return nil
}
