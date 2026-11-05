package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/WellWells/line-cli/internal/line"
)

// useEdition makes the notes read as in that build, so both wordings are
// tested in both builds.
func useEdition(t *testing.T, e string) {
	t.Helper()
	old := edition
	edition = e
	t.Cleanup(func() { edition = old })
}

const (
	editionOpen   = "m0000000000000000000000000000000b"
	editionThread = "t0000000000000000000000000000000b"
)

// Synthetic rows only: an open chat whose one thread has replies this PC does
// not store.
const editionScript = `
INSERT INTO _chat (_id, _midType, _lastUpdatedTime, _unreadCount, _status) VALUES ('` + editionOpen + `', 4, 5000, 0, 0);
INSERT INTO _square (_mid, _name, _myMemberId) VALUES ('s0000000000000000000000000000000b', 'Club', 'p0000000000000000000000000000000e');
INSERT INTO _squareChat (_squareChatMid, _squareMid, _type, _name) VALUES ('` + editionOpen + `', 's0000000000000000000000000000000b', 1, 'Open Chat');
INSERT INTO _squareMember (_squareMemberMid, _squareMid, _displayName) VALUES ('p0000000000000000000000000000000a', 's0000000000000000000000000000000b', 'Alice');
INSERT INTO _profile (_mid, _displayName) VALUES ('u0000000000000000000000000000000e', 'Me');
INSERT INTO _squareThread (_threadMid, _chatMid, _messageId, _totalMessageCount, _lastMessageAt) VALUES ('` + editionThread + `', '` + editionOpen + `', 'o1', 4, 9000);
INSERT INTO _message (_id, _chatId, _from, _toType, _createdTime, _text, _contentType, _type, _status, _attribute) VALUES
 ('o1', '` + editionOpen + `', 'p0000000000000000000000000000000a', 4, 1000, 'starts a thread', 0, 1, 1, 0);
`

// The note must say what is missing and how to get it, in the words of the
// build that prints it; the stored messages still come out.
func TestReadNotesThreadRepliesNotRead(t *testing.T) {
	useSyntheticDB(t, editionScript)
	for _, c := range []struct {
		edition string
		want    string
	}{
		{"open", "the official prebuilt line-cli reads"},
		{"full", "without --offline, line-cli reads"},
	} {
		useEdition(t, c.edition)
		rc, out, errs := runCLIErr("read", editionOpen, "--since", "1970-01-01T00:00:00Z", "--offline")
		var msgs []line.Message
		if rc != 0 || json.Unmarshal(out, &msgs) != nil || len(msgs) != 1 {
			t.Fatalf("%s: rc=%d out=%s err=%s", c.edition, rc, out, errs)
		}
		for _, want := range []string{"1 thread(s)", "4 replies", "LINE for PC does not store thread replies", c.want} {
			if !strings.Contains(errs, want) {
				t.Errorf("%s: stderr lacks %q: %s", c.edition, want, errs)
			}
		}
	}
}

// An open build never contacts the server, so its read of an open chat always
// carries the note, with or without --offline.
func TestOpenBuildReadNotesThreadReplies(t *testing.T) {
	if line.Edition != "open" {
		t.Skip("the full build reads these replies from LINE's server")
	}
	useSyntheticDB(t, editionScript)
	rc, _, errs := runCLIErr("read", editionOpen, "--since", "1970-01-01T00:00:00Z")
	if rc != 0 || !strings.Contains(errs, "4 replies") || !strings.Contains(errs, "official prebuilt") {
		t.Fatalf("rc=%d stderr: %s", rc, errs)
	}
}

// Advice to run read or drop --offline cannot help an open build.
func TestServerAdviceFollowsTheEdition(t *testing.T) {
	for _, chat := range []string{"t1", "m1"} {
		for _, h := range []serverHint{hintRead, hintOffline, hintNone} {
			useEdition(t, "open")
			a := serverAdvice(chat, h)
			if !strings.HasPrefix(a, "the official prebuilt line-cli reads ") || strings.Contains(a, "--offline") || strings.Contains(a, "'line read") {
				t.Errorf("open, %s, hint %d: %q", chat, h, a)
			}
		}
	}
	useEdition(t, "open")
	if a := serverAdvice("c1", hintRead); a != "" {
		t.Errorf("a group has nothing to fetch: %q", a)
	}
}

// help must not promise the open build what only the official build does.
func TestHelpSaysWhatTheOpenBuildLacks(t *testing.T) {
	useEdition(t, "open")
	if !strings.Contains(helpDoc().About, "open build") {
		t.Errorf("open help: %s", helpDoc().About)
	}
	useEdition(t, "full")
	if strings.Contains(helpDoc().About, "open build") {
		t.Errorf("full help: %s", helpDoc().About)
	}
}
