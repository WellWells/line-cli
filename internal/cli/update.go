package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/WellWells/line-cli/internal/line"
)

// updateRepo is the repository official releases are published to.
const updateRepo = "WellWells/line-cli"

// updateStampFile holds the local date of the last daily check.
const updateStampFile = "update-check.txt"

const (
	updateNoteTimeout  = 2 * time.Second // the daily check holds up a finished command
	updateCheckTimeout = 15 * time.Second
)

// latestRelease looks up the newest release (a test seam).
var latestRelease = line.LatestRelease

// Main is Run plus the daily update check, for the line program itself. Run
// stays free of it, so tests and other callers never reach the network.
func Main(args []string, stdout, stderr io.Writer) int {
	code, rc := run(args, stdout, stderr)
	if code == 0 {
		dailyUpdateNote(rc.cmd, rc.offline, stderr, time.Now())
	}
	return code
}

// parseRelease reads an official version, x.y.z with x >= 1.
func parseRelease(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(s, ".")
	if len(parts) != len(v) {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p != strconv.Itoa(n) {
			return v, false
		}
		v[i] = n
	}
	return v, v[0] >= 1
}

// newerRelease reports that latest is an official version after current. A
// development or beta build is never told to update.
func newerRelease(latest, current string) bool {
	l, ok1 := parseRelease(latest)
	c, ok2 := parseRelease(current)
	return ok1 && ok2 && slices.Compare(l[:], c[:]) > 0
}

// installCommand is the PowerShell command that updates the install holding
// exe; store is the default install folder.
func installCommand(exe, store string) string {
	cmd := "irm https://github.com/" + updateRepo + "/releases/latest/download/install.ps1 | iex"
	dir := filepath.Dir(exe)
	root := filepath.Dir(dir)
	if strings.EqualFold(filepath.Base(dir), "bin") && !strings.EqualFold(root, store) {
		return "$env:LINECLI_INSTALL_DIR='" + strings.ReplaceAll(root, "'", "''") + "'; " + cmd
	}
	return cmd
}

func updateCommand() string {
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	return installCommand(exe, line.StoreDir())
}

// wantsUpdateCheck says whether this finished command may check. An opted-out
// user, CI, --offline and non-release builds never reach the network, and
// commands that only print help or run on are left alone.
func wantsUpdateCheck(cmd string, offline bool) bool {
	if edition != "full" || offline || os.Getenv("LINECLI_NO_UPDATE_NOTIFIER") != "" || os.Getenv("CI") != "" {
		return false
	}
	switch cmd {
	case "", "help", "version", "skill", "uninstall", "update", "watch":
		return false
	}
	_, release := parseRelease(Version)
	return release
}

// dailyUpdateNote checks for a release once a day and prints one line on
// stderr when there is one. The date is written before the lookup and every
// failure is silent, so a blocked connection costs one wait a day and never
// changes what a command printed or returned.
func dailyUpdateNote(cmd string, offline bool, stderr io.Writer, now time.Time) {
	if !wantsUpdateCheck(cmd, offline) {
		return
	}
	path := filepath.Join(line.StoreDir(), updateStampFile)
	today := now.Format(time.DateOnly)
	if b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) == today {
		return
	}
	if os.MkdirAll(line.StoreDir(), 0o700) != nil || os.WriteFile(path, []byte(today), 0o600) != nil {
		return
	}
	latest, err := latestRelease(updateRepo, updateNoteTimeout)
	if err != nil || !newerRelease(latest, Version) {
		return
	}
	fmt.Fprintf(stderr, "line: note: line-cli %s is available (you have %s); to update, run this in PowerShell: %s\n", latest, Version, updateCommand())
}

// updateResult is the output of line update.
type updateResult struct {
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"update_available"`
	Command         string `json:"command,omitempty"`
}

func (rc *runCtx) update(args []string) int {
	fs := newFlags("update")
	if err := parseArgs(fs, args); err != nil {
		return rc.fail(err)
	}
	if fs.NArg() > 0 {
		return rc.fail(invalid(fmt.Sprintf("unexpected argument(s) %q", fs.Args())))
	}
	latest, err := latestRelease(updateRepo, updateCheckTimeout)
	if err != nil {
		return rc.fail(err)
	}
	res := updateResult{Current: Version, Latest: latest, UpdateAvailable: newerRelease(latest, Version)}
	if res.UpdateAvailable {
		res.Command = updateCommand()
	}
	if !rc.text {
		return rc.emit(res)
	}
	if res.UpdateAvailable {
		fmt.Fprintf(rc.out, "line-cli %s is available (you have %s)\nTo update, run this in PowerShell: %s\n", latest, Version, res.Command)
	} else {
		fmt.Fprintf(rc.out, "line-cli %s is the latest version (you have %s)\n", latest, Version)
	}
	return 0
}
