package cli

import (
	"strings"
	"testing"
)

// A stale copy of aroundScript: LINE saw activity long after the newest stored row.
var staleAroundScript = strings.Replace(aroundScript, "_status) VALUES ('"+aroundChatX+"', 2, 6000,", "_status) VALUES ('"+aroundChatX+"', 2, 9000000,", 1)

// read resolves the chat (by name or from --around) before the freshness notes,
// so they name the resolved chat; --around gets the history note but no stale note.
func TestMergeReadFreshnessNotes(t *testing.T) {
	defer func(f func() string) { stoppedDataAsOf = f }(stoppedDataAsOf)
	stoppedDataAsOf = func() string { return "" }
	if staleAroundScript == aroundScript {
		t.Fatal("stale fixture not applied")
	}
	useSyntheticDB(t, staleAroundScript)
	oldestMsg := "reached the oldest message stored for chat " + aroundChatX
	staleMsg := "note: chat " + aroundChatX + " has activity up to"
	for _, c := range []struct {
		args          []string
		oldest, stale bool
	}{
		{[]string{"read", "Group X", "--limit", "10"}, true, true},
		{[]string{"read", "--around", "m1", "--limit", "3"}, true, false},
		{[]string{"read", "--around", "m4", "--limit", "3"}, false, false},
		{[]string{"read", "Group X", "--around", "m2", "--limit", "3"}, true, false},
	} {
		rc, out, errs := runCLIErr(c.args...)
		if rc != 0 {
			t.Fatalf("%v: rc=%d %s %s", c.args, rc, out, errs)
		}
		if strings.Contains(errs, oldestMsg) != c.oldest {
			t.Errorf("%v: oldest note = %v, stderr %q", c.args, !c.oldest, errs)
		}
		if strings.Contains(errs, staleMsg) != c.stale {
			t.Errorf("%v: stale note = %v, stderr %q", c.args, !c.stale, errs)
		}
	}
}

func TestMergeRerunnableReadOnly(t *testing.T) {
	for _, cmd := range []string{"chats", "contacts", "members", "read", "search", "find-file", "unread"} {
		if !rerunnable[cmd] {
			t.Errorf("%s should be rerunnable", cmd)
		}
	}
	for _, cmd := range []string{"send", "download", "preview", "init", "status"} {
		if rerunnable[cmd] {
			t.Errorf("%s must not be rerun", cmd)
		}
	}
}
