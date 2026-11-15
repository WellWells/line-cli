package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// askYesNo asks question on the console and reports a yes. Like winget or apt,
// only y or yes goes ahead; anything else, or no answer, is a no, and -y skips
// the question. A variable so tests can answer for a person.
var askYesNo = func(w io.Writer, question string) bool {
	fmt.Fprint(w, question+" [y/N] ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}
