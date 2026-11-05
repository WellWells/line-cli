package cli

import (
	"bytes"
	"testing"

	"github.com/WellWells/line-cli/internal/line"
)

// stats --text escapes like the other list commands: one group per line.
func TestStatsTextEscaped(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, err: &bytes.Buffer{}, text: true}
	rc.emitStats(&line.StatsResult{ChatID: "c1", ChatName: "N\tM", By: "sender", Total: 3, GroupCount: 1,
		Groups: []line.StatsGroup{{SenderID: "u1", Sender: "A\nB", FromMe: true, Count: 3, FirstSentAt: "T1", LastSentAt: "T2"}}})
	want := "N\\tM\tc1\tby=sender\ttotal=3\tgroups=1\nA\\nB\tu1 (me)\t3\tT1\tT2\n"
	if got := out.String(); got != want {
		t.Errorf("stats text = %q, want %q", got, want)
	}
}

// stats only reads, so withReader may rerun it after a checkpoint.
func TestStatsRerunnable(t *testing.T) {
	if !rerunnable["stats"] {
		t.Error("stats is not rerunnable")
	}
}
