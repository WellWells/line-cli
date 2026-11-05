package cli

import (
	"bytes"
	"testing"

	"github.com/WellWells/line-cli/internal/line"
)

// members --text escapes like the other list commands: one member per line.
func TestMembersTextEscaped(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, err: &bytes.Buffer{}, text: true}
	rc.emitMembers([]line.Member{{ID: "u1", Name: "A\nB\tC", IsMe: true, Role: "member", State: "joined", MessageCount: 3, LastSentAt: "T"}})
	if got := out.String(); got != "A\\nB\\tC (me)\tu1\tmember\tjoined\tmessages=3\tT\n" {
		t.Errorf("members text = %q", got)
	}
}
