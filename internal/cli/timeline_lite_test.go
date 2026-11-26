package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// The open build has no notes or albums: every subcommand says so, after the
// same argument checks as the full build.
func TestOpenBuildNotesAndAlbums(t *testing.T) {
	useSyntheticDB(t, aroundScript)
	for _, args := range [][]string{
		{"notes", aroundChatX},
		{"notes", "add", "--to", aroundChatX, "--message", "hi", "--dry-run"},
		{"albums", aroundChatX},
		{"albums", "delete", "--to", aroundChatX, "--id", "1", "--dry-run"},
	} {
		rc, out, _ := runCLIErr(args...)
		var e errorOutput
		if rc != 1 || json.Unmarshal(out, &e) != nil || e.Error != "ERROR" || !strings.Contains(e.Message, "open build") {
			t.Errorf("%q: rc=%d out=%s", args, rc, out)
		}
	}
}
