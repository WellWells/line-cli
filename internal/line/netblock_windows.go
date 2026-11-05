package line

import (
	"errors"
	"os"
	"syscall"
)

// blockedByFirewall reports a connection Windows refused before it was made
// (WSAEACCES, "forbidden by its access permissions"). That is a firewall or
// security program blocking this program, not a LINE or network problem.
func blockedByFirewall(err error) bool { return errors.Is(err, syscall.WSAEACCES) }

// firewallHint says what to do when blockedByFirewall. Firewall rules are
// usually per program path, so a linecli.exe built or copied elsewhere is
// blocked even when another copy is allowed.
func firewallHint() string {
	exe, err := os.Executable()
	if err != nil {
		exe = "linecli.exe"
	}
	return "Windows blocked the connection before it was made: a firewall or security program (such as Bitdefender or Windows Defender Firewall) does not allow " + exe + " (the program the line command runs) to connect. Allow that program in the firewall, or run the linecli.exe it already allows (rules follow the program's path), then retry; this is not a LINE sign-in problem"
}
