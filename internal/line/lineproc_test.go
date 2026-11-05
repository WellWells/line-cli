package line

import "testing"

func TestPickLineProcess(t *testing.T) {
	verdicts := map[uint32]procVerdict{1: procNotLine, 2: procUnknown, 3: procLine, 4: procUnknown, 5: procNotLine}
	check := func(pid uint32) procVerdict { return verdicts[pid] }
	for _, c := range []struct {
		name string
		pids []uint32
		want uint32
		ok   bool
	}{
		{"LINE wins over everything", []uint32{1, 2, 3, 4}, 3, true},
		{"unchecked when no LINE", []uint32{1, 2, 4}, 2, true},
		{"only other programs: not running", []uint32{1, 5}, 0, false},
		{"none", nil, 0, false},
	} {
		got, ok := pickLineProcess(c.pids, check)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got %d, %v; want %d, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestIsLineProduct(t *testing.T) {
	for p, want := range map[string]bool{"LINE": true, "line": true, " LINE ": true, "": false, "line-cli": false, "LINE WORKS": false, "Microsoft® Windows® Operating System": false} {
		if got := isLineProduct(p); got != want {
			t.Errorf("isLineProduct(%q) = %v, want %v", p, got, want)
		}
	}
}
