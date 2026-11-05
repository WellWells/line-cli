package cli

import (
	"fmt"
	"runtime/debug"
	"strings"
	"time"
)

// release is the version of an official release, 1.x.y: x for new features
// or other changes, y for bug fixes only. It stays empty while line-cli is in
// beta; go run ./tools/release sets it.
const release = "1.0.0"

// Version is release, or during the beta the time of the commit the binary
// was built from (BetaVersion), so each build installed during development
// tells which code it has. It is "dev" when no commit was recorded (go run,
// go test).
var Version = buildVersion()

// Taipei is UTC+8, Taiwan time, which has no daylight saving time; a fixed
// zone needs no time zone database.
var Taipei = time.FixedZone("UTC+8", 8*60*60)

// BetaVersion is the beta version for a commit time: 0.YYYYMMDD.HHMM in
// Taiwan time. Semantic versions forbid leading zeros, so 09:30 is 930;
// versions still sort by time, and every 1.x.y sorts after them.
func BetaVersion(t time.Time) string {
	t = t.In(Taipei)
	return fmt.Sprintf("0.%s.%d", t.Format("20060102"), t.Hour()*100+t.Minute())
}

func buildVersion() string {
	if release != "" {
		return release
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.time" {
			if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
				return BetaVersion(t)
			}
		}
	}
	// go install ...@v0.20261031.1530 records the tag instead of the commit.
	if v := info.Main.Version; strings.HasPrefix(v, "v0.") && !strings.ContainsAny(v, "-+") {
		return strings.TrimPrefix(v, "v")
	}
	return "dev"
}
