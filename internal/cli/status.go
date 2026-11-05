package cli

import "github.com/WellWells/line-cli/internal/line"

func (rc *runCtx) status(args []string) int {
	fs := newFlags("status")
	if err := parseArgs(fs, args); err != nil {
		return rc.fail(err)
	}
	if fs.NArg() > 0 {
		return rc.fail(invalid("status takes no arguments"))
	}
	s, err := line.GetStatus()
	if err != nil {
		return rc.fail(err)
	}
	return rc.emit(s)
}

var stoppedDataAsOf = line.StoppedDataAsOf

// noteLineStopped warns that the local data stops at LINE's last run and
// remembers it in rc.dataAsOf.
func (rc *runCtx) noteLineStopped() {
	if rc.dataAsOf = stoppedDataAsOf(); rc.dataAsOf != "" {
		rc.notify("note: LINE is not running; data is as of " + rc.dataAsOf)
	}
}
