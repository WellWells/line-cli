package line

import "strings"

// procVerdict is what a process named LINE.exe turned out to be. line-cli's
// own command, line.exe, has the same name as far as Windows is concerned
// (names ignore case), so the name alone does not find LINE.
type procVerdict int

const (
	procUnknown procVerdict = iota // its program file could not be checked
	procLine                       // LINE itself: its version information names the product LINE
	procNotLine                    // another program, such as line-cli's line.exe, which has no version information
)

// pickLineProcess returns the first candidate that is LINE, else the first
// one that could not be checked (trying it beats reporting LINE as not
// running), and false when every candidate is another program.
func pickLineProcess(pids []uint32, check func(uint32) procVerdict) (uint32, bool) {
	var fallback uint32
	found := false
	for _, pid := range pids {
		switch check(pid) {
		case procLine:
			return pid, true
		case procUnknown:
			if !found {
				fallback, found = pid, true
			}
		}
	}
	return fallback, found
}

// isLineProduct reports whether a program's ProductName is LINE's.
func isLineProduct(product string) bool {
	return strings.EqualFold(strings.TrimSpace(product), "LINE")
}
