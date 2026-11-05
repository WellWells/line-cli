package cli

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/WellWells/line-cli/internal/line"
)

// Live send checks use --dry-run only: nothing is sent and nothing reaches the
// network. Failures report codes, counts and field names only.

const liveProbe = "linecli live dry-run probe"

func sendLogStat() string {
	fi, err := os.Stat(filepath.Join(os.Getenv("LOCALAPPDATA"), "linecli", "send-log.jsonl"))
	if err != nil {
		return "absent"
	}
	return fi.ModTime().Format(time.RFC3339Nano) + "/" + strconv.FormatInt(fi.Size(), 10)
}

func dryRun(t *testing.T, args ...string) (int, map[string]any, string) {
	t.Helper()
	rc, out, stderr := runCLIErr(append(append([]string{"send"}, args...), "--dry-run")...)
	m, _ := decode(out).(map[string]any)
	if m == nil {
		t.Fatalf("send: output is not a JSON object (rc=%d)", rc)
	}
	return rc, m, stderr
}

func TestLiveSendDryRun(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	logBefore := sendLogStat()

	chats := mustRows(t, "chats", "--limit", "5000")
	var ok map[string]any
	okID := ""
	codes := map[string]int{}
	for _, c := range chats {
		id := str(c, "chat_id")
		if !strings.HasPrefix(id, "u") {
			continue
		}
		rc, m, _ := dryRun(t, "--to", id, "--message", liveProbe)
		if rc != 0 {
			code := str(m, "error")
			codes[code]++
			if code != "E2EE_DISABLED" && code != "E2EE_KEY_MISSING" {
				t.Fatalf("1:1 dry run failed with %s", code)
			}
			if code == "E2EE_DISABLED" && !strings.Contains(str(m, "message"), "Letter Sealing") {
				t.Error("E2EE_DISABLED message does not mention Letter Sealing")
			}
			continue
		}
		if ok == nil {
			ok, okID = m, id
		}
	}
	t.Logf("1:1 dry-run failures by code: %v", codes)
	if ok == nil {
		t.Fatal("no one-to-one chat could be dry-run")
	}
	sum := sha256.Sum256([]byte(liveProbe))
	for k, want := range map[string]any{
		"to": okID, "dry_run": true, "sent": false, "verified": false, "normalized": false, "e2ee_version": 1.0,
		"text": liveProbe, "text_length": float64(len([]rune(liveProbe))), "text_sha256": hex.EncodeToString(sum[:]),
	} {
		if ok[k] != want {
			t.Errorf("dry run field %s has the wrong value", k)
		}
	}
	for _, k := range []string{"to_name", "chat_type", "from", "endpoint"} {
		if str(ok, k) == "" {
			t.Errorf("dry run field %s is missing or empty", k)
		}
	}
	for _, k := range []string{"sender_key_id", "receiver_key_id", "body_bytes"} {
		if _, isNum := ok[k].(float64); !isNum {
			t.Errorf("dry run field %s is not a number", k)
		}
	}
	if cs, _ := ok["chunk_sizes"].([]any); len(cs) != 5 {
		t.Errorf("chunk_sizes has %d entries, want 5", len(cs))
	}
	for _, k := range []string{"server_message_id", "sent_at"} {
		if _, has := ok[k]; has {
			t.Errorf("dry run has %s", k)
		}
	}

	rc, m, _ := dryRun(t, "--to", okID, "--message", "line1\r\nline2\r\n")
	if rc != 0 || m["text"] != "line1\nline2" || m["normalized"] != true {
		t.Errorf("CRLF text: rc=%d normalized=%v", rc, m["normalized"])
	}

	dir := t.TempDir()
	u16 := filepath.Join(dir, "probe-utf16.txt")
	os.WriteFile(u16, utf16File(liveProbe+"\r\n", binary.LittleEndian), 0o600)
	rc, m, _ = dryRun(t, "--to", okID, "--message-file", u16)
	if rc != 0 || m["text"] != liveProbe || m["normalized"] != true {
		t.Errorf("UTF-16 --message-file: rc=%d error=%v normalized=%v", rc, m["error"], m["normalized"])
	}
	ansi := filepath.Join(dir, "probe-ansi.txt")
	os.WriteFile(ansi, []byte{0x61, 0xa4, 0xa4}, 0o600)
	rc, m, _ = dryRun(t, "--to", okID, "--message-file", ansi)
	if rc != 1 || m["error"] != "INVALID_ARGUMENT" || !strings.Contains(str(m, "message"), "Set-Content -Encoding UTF8") {
		t.Errorf("ANSI --message-file: rc=%d error=%v", rc, m["error"])
	}

	for _, c := range mustRows(t, "contacts") {
		if c["is_me"] == true {
			if rc, m, _ := dryRun(t, "--to", str(c, "contact_id"), "--message", liveProbe); rc == 0 && m["chat_type"] != "self" {
				t.Errorf("own chat has chat_type %v, want self", m["chat_type"])
			}
			break
		}
	}

	// Groups: --dry-run builds a Letter Sealing v2 message to the group key.
	// Open chats and threads are refused even as a dry run.
	names := map[string]int{}
	for _, c := range chats {
		names[str(c, "name")]++
	}
	plainLen := len(`{"text":"` + liveProbe + `"}`)
	wantShape := fmt.Sprint([]any{16.0, float64(plainLen + 16), 12.0, 4.0, 4.0})
	groupOK, groupCodes, byName := 0, map[string]int{}, false
	refused := map[string]bool{}
	for _, c := range chats {
		id, name, typ := str(c, "chat_id"), str(c, "name"), str(c, "type")
		switch typ {
		case "group":
			rc, m, _ := dryRun(t, "--to", id, "--message", liveProbe)
			if rc != 0 {
				code := str(m, "error")
				groupCodes[code]++
				if code != "E2EE_DISABLED" && code != "E2EE_KEY_MISSING" {
					t.Fatalf("group dry run failed with %s", code)
				}
				continue
			}
			groupOK++
			if m["chat_type"] != "group" || m["e2ee_version"] != 2.0 || m["dry_run"] != true || m["sent"] != false ||
				fmt.Sprint(m["chunk_sizes"]) != wantShape {
				t.Errorf("group dry run: chat_type=%v e2ee_version=%v chunk_sizes=%v", m["chat_type"], m["e2ee_version"], m["chunk_sizes"])
			}
			if !byName && name != id && names[name] == 1 {
				byName = true
				if rc, m, _ := dryRun(t, "--to", name, "--message", liveProbe); rc != 0 || m["to"] != id {
					t.Errorf("group dry run by exact name: rc=%d", rc)
				}
			}
		case "open", "thread":
			if refused[typ] {
				continue
			}
			refused[typ] = true
			rc, m, _ := dryRun(t, "--to", id, "--message", liveProbe)
			cands, _ := m["candidates"].([]any)
			var first map[string]any
			if len(cands) > 0 {
				first, _ = cands[0].(map[string]any)
			}
			if rc != 1 || m["error"] != "INVALID_ARGUMENT" || len(cands) != 1 || str(first, "chat_id") != id || str(first, "type") != typ {
				t.Errorf("%s target: rc=%d error=%v candidates=%d", typ, rc, m["error"], len(cands))
			}
		}
	}
	t.Logf("group dry runs: %d built, failures by code %v; refused types %v", groupOK, groupCodes, refused)
	if groupOK == 0 {
		t.Error("no group could be dry-run")
	}

	if logBefore != sendLogStat() {
		t.Error("a dry run changed the send log")
	}
}

// Verification after a real send reopens the database per poll and once just
// before the POST; check that reopening works on the real database.
func TestLiveSendReopen(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	code := func(err error) string {
		if le, ok := line.AsError(err); ok {
			return string(le.Code)
		}
		return "ERROR"
	}
	db, err := line.Open(func(string) {}, false)
	if err != nil {
		t.Fatalf("open failed: %s", code(err))
	}
	defer db.Close()
	for range 3 {
		fresh, err := db.Reopen()
		if err != nil {
			t.Fatalf("reopen failed: %s", code(err))
		}
		fresh.Close()
	}
}
