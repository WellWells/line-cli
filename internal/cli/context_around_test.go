package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/WellWells/line-cli/internal/line"
)

// Synthetic rows only: fake chat ids, mids and texts.
const (
	aroundChatX = "c0000000000000000000000000000000a"
	aroundChatY = "c0000000000000000000000000000000b"
	aroundMidA  = "u00000000000000000000000000000001"
	aroundMidMe = "u00000000000000000000000000000002"
)

const aroundScript = `
INSERT INTO _chat (_id, _midType, _lastUpdatedTime, _unreadCount, _status) VALUES ('` + aroundChatX + `', 2, 6000, 0, 0), ('` + aroundChatY + `', 2, 6000, 0, 0);
INSERT INTO _groupChat (_type, _chatMid, _chatName, _status) VALUES (0, '` + aroundChatX + `', 'Group X', 0), (0, '` + aroundChatY + `', 'Group Y', 0);
INSERT INTO _contact (_mid, _type, _status, _displayName, _displayNameOverridden) VALUES ('` + aroundMidA + `', 0, 1, 'Alice', '');
INSERT INTO _profile (_mid, _displayName) VALUES ('` + aroundMidMe + `', 'Me');
INSERT INTO _message (_id, _chatId, _from, _toType, _createdTime, _text, _contentType, _type, _status, _attribute) VALUES
 ('m1', '` + aroundChatX + `', '` + aroundMidA + `', 2, 1000, 'one', 0, 1, 1, 0),
 ('m2', '` + aroundChatX + `', '` + aroundMidMe + `', 2, 2000, 'two', 0, 1, 1, 0),
 ('m3', '` + aroundChatX + `', '` + aroundMidA + `', 2, 3000, 'three' || char(10) || 'lines', 0, 1, 1, 0),
 ('m4', '` + aroundChatX + `', '` + aroundMidA + `', 2, 4000, 'four', 0, 1, 1, 0),
 ('m5', '` + aroundChatX + `', '` + aroundMidA + `', 2, 5000, 'five', 0, 1, 1, 0),
 ('y1', '` + aroundChatY + `', '` + aroundMidA + `', 2, 3000, 'other chat', 0, 1, 1, 0);
`

// useSyntheticDB makes commands read a synthetic database instead of LINE's.
func useSyntheticDB(t *testing.T, script string) {
	t.Helper()
	t.Setenv("LOCALAPPDATA", t.TempDir())
	oldOpen, oldStopped := openDB, stoppedDataAsOf
	openDB = func(func(string), bool) (*line.DB, error) { return line.OpenSynthetic(script) }
	stoppedDataAsOf = func() string { return "" }
	t.Cleanup(func() { openDB, stoppedDataAsOf = oldOpen, oldStopped })
}

// A run on a synthetic database must not depend on this machine's LINE: the
// stopped-LINE note would come and go as the real LINE closes and opens.
func TestSyntheticDBIgnoresThisMachinesLINE(t *testing.T) {
	old := stoppedDataAsOf
	stoppedDataAsOf = func() string { return "2026-01-02T03:04:05+08:00" }
	t.Cleanup(func() { stoppedDataAsOf = old })
	useSyntheticDB(t, finalScript)
	if rc, _, errs := runCLIErr("stats", finalChatFresh); rc != 0 || strings.Contains(errs, "LINE is not running") {
		t.Fatalf("rc=%d stderr: %s", rc, errs)
	}
}

// read --around must work without a chat: whatever read does with the chat
// (resolving it, freshness notes) has to use the message's chat.
func TestReadAroundCommand(t *testing.T) {
	useSyntheticDB(t, aroundScript)
	for _, args := range [][]string{
		{"read", "--around", "m3", "--limit", "3"},
		{"read", "--around", "m3", "--limit", "3", aroundChatX},
		{"read", "--around", "m3", "--limit", "3", "Group X"},
		{"read", aroundChatX, "--around", "m3", "--limit", "3"},
	} {
		rc, out, errs := runCLIErr(args...)
		if rc != 0 {
			t.Fatalf("%v: rc=%d %s %s", args, rc, out, errs)
		}
		var msgs []line.Message
		if err := json.Unmarshal(out, &msgs); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		var got []string
		for _, m := range msgs {
			got = append(got, m.ID)
		}
		if !reflect.DeepEqual(got, []string{"m2", "m3", "m4"}) {
			t.Errorf("%v = %v", args, got)
		}
	}
	rc, out, _ := runCLIErr("read", "--around", "m3", "--limit", "3", "--text")
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if rc != 0 || len(lines) != 3 || len(strings.Split(lines[1], "\t")) != 6 || !strings.HasSuffix(lines[1], `three\nlines`) {
		t.Errorf("--text: rc=%d %q", rc, out)
	}
	for _, args := range [][]string{
		{"read", "--around", "nope"},
		{"read", "--around", "m3", aroundChatY},
		{"read", "--around", "m3", "--since", "2020-01-01"},
	} {
		rc, out, _ := runCLIErr(args...)
		var e struct{ Error string }
		if json.Unmarshal(out, &e); rc != 1 || e.Error != string(line.ErrInvalidArgument) {
			t.Errorf("%v: rc=%d %s", args, rc, out)
		}
	}
}
