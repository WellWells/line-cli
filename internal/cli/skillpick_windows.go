package cli

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// noPromptEnv are set when CI or an AI agent runs a command: nobody is there
// to answer a checklist.
var noPromptEnv = []string{"CI", "CLAUDECODE", "AI_AGENT", "CURSOR_AGENT", "GEMINI_CLI"}

// canPrompt reports whether a person can answer the checklist: standard input
// is a console, and neither CI nor an AI agent runs the command. A variable so
// tests can choose.
var canPrompt = func() bool {
	for _, k := range noPromptEnv {
		if os.Getenv(k) != "" {
			return false
		}
	}
	var m uint32
	return windows.GetConsoleMode(windows.Handle(os.Stdin.Fd()), &m) == nil
}

// runPicker shows p on the console until the person confirms or skips, and
// reports which. It draws on CONOUT$, the console itself, so it works while
// standard output goes to a pipe (the install script reads the JSON there). A
// variable so tests can answer for a person.
var runPicker = func(p *picker, l lang) (bool, error) {
	in := windows.Handle(os.Stdin.Fd())
	var inMode uint32
	if err := windows.GetConsoleMode(in, &inMode); err != nil {
		return false, err
	}
	name, _ := windows.UTF16PtrFromString("CONOUT$")
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return false, err
	}
	out := os.NewFile(uintptr(h), "CONOUT$") // a console file: Go writes UTF-16 to it
	defer out.Close()
	var outMode uint32
	if err := windows.GetConsoleMode(h, &outMode); err != nil {
		return false, err
	}
	if err := windows.SetConsoleMode(h, outMode|windows.ENABLE_PROCESSED_OUTPUT|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
		return false, err
	}
	defer windows.SetConsoleMode(h, outMode)
	// Keys one at a time, not echoed, arrows as escape sequences, and Ctrl+C
	// as a key rather than a signal.
	raw := inMode&^(windows.ENABLE_LINE_INPUT|windows.ENABLE_ECHO_INPUT|windows.ENABLE_PROCESSED_INPUT|
		windows.ENABLE_MOUSE_INPUT|windows.ENABLE_WINDOW_INPUT) | windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	if err := windows.SetConsoleMode(in, raw); err != nil {
		return false, err
	}
	defer windows.SetConsoleMode(in, inMode)
	windows.FlushConsoleInputBuffer(in) // keys typed before the checklist showed

	fmt.Fprint(out, "\x1b[?25l") // hide the cursor
	defer fmt.Fprint(out, "\x1b[?25h")
	drawn := 0
	draw := func() {
		var b strings.Builder
		if drawn > 0 {
			fmt.Fprintf(&b, "\x1b[%dF", drawn) // back to the first line drawn
		}
		b.WriteString("\x1b[J") // and clear from there down
		lines := p.render(l, consoleWidth(h))
		for _, s := range lines {
			b.WriteString(s + "\r\n")
		}
		drawn = len(lines)
		fmt.Fprint(out, b.String())
	}
	draw()
	buf := make([]uint16, 64)
	for {
		var n uint32
		if err := windows.ReadConsole(in, &buf[0], uint32(len(buf)), &n, nil); err != nil {
			return false, err
		}
		for _, k := range parseKeys(string(utf16.Decode(buf[:n]))) {
			if done, ok := p.handle(k); done {
				draw()
				return ok, nil
			}
		}
		draw()
	}
}

// consoleWidth is the width of the console window, or 0 when unknown.
func consoleWidth(h windows.Handle) int {
	var info windows.ConsoleScreenBufferInfo
	if windows.GetConsoleScreenBufferInfo(h, &info) != nil {
		return 0
	}
	return int(info.Window.Right-info.Window.Left) + 1
}
