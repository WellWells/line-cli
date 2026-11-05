package cli

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBetaVersionIsTaiwanCommitTime(t *testing.T) {
	for utc, want := range map[string]string{
		"2026-10-31T07:30:59Z": "0.20261031.1530",
		"2026-10-31T01:30:00Z": "0.20261031.930", // 09:30, without the leading zero
		"2026-10-30T16:05:00Z": "0.20261031.5",   // 00:05 the next day in Taiwan
		"2026-12-31T15:59:00Z": "0.20261231.2359",
	} {
		tm, err := time.Parse(time.RFC3339, utc)
		if err != nil {
			t.Fatal(err)
		}
		if got := BetaVersion(tm); got != want {
			t.Errorf("BetaVersion(%s) = %s, want %s", utc, got, want)
		}
	}
}

func TestBetaVersionsSortByTime(t *testing.T) {
	semver := regexp.MustCompile(`^0\.[1-9][0-9]{7}\.(0|[1-9][0-9]*)$`)
	key := func(v string) [2]int {
		p := strings.Split(v, ".")
		a, _ := strconv.Atoi(p[1])
		b, _ := strconv.Atoi(p[2])
		return [2]int{a, b}
	}
	start := time.Date(2026, 10, 30, 0, 0, 0, 0, Taipei)
	prev := ""
	for m := 0; m < 3*24*60; m++ {
		v := BetaVersion(start.Add(time.Duration(m) * time.Minute))
		if !semver.MatchString(v) {
			t.Fatalf("%s is not a semantic version", v)
		}
		if prev != "" {
			if a, b := key(prev), key(v); a[0] > b[0] || a[0] == b[0] && a[1] >= b[1] {
				t.Fatalf("%s sorts before %s", v, prev)
			}
		}
		prev = v
	}
}

func TestVersionIsSet(t *testing.T) {
	if release == "" && Version != "dev" && !regexp.MustCompile(`^0\.\d{8}\.\d+$`).MatchString(Version) {
		t.Fatalf("beta Version %q", Version)
	}
	if release != "" && Version != release {
		t.Fatalf("Version %q, release %q", Version, release)
	}
}
