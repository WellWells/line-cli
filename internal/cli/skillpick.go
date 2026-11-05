package cli

import (
	"strings"
	"unicode/utf8"
)

// The checklist 'line skill install' shows a person at a console, after the
// one in npx skills add: the agents found on this PC (or the saved choice)
// ticked, the shared folder always in, then a summary to confirm.

// pickItem is one line of the checklist.
type pickItem struct {
	id     string
	label  string
	path   string // where the skill goes, as shown
	note   string // lines under the item, such as the agents it covers
	locked bool   // always installed; the cursor skips it
	on     bool
	found  bool // detected on this PC
}

type pickStage int

const (
	stageList    pickStage = iota // ticking agents
	stageConfirm                  // the summary, waiting for Enter
	stageDone                     // confirmed
	stageSkipped                  // cancelled
)

type picker struct {
	items []pickItem
	cur   int
	stage pickStage
	// summary lists what installing the selected agents will do, for the
	// confirm stage.
	summary func(selected []string) []string
}

func newPicker(items []pickItem, summary func([]string) []string) *picker {
	p := &picker{items: items, cur: -1, summary: summary}
	p.move(1)
	return p
}

// move puts the cursor on the next unlocked item in direction d, wrapping.
func (p *picker) move(d int) {
	n := len(p.items)
	for i, c := 0, p.cur; i < n; i++ {
		c = (c + d + n) % n
		if !p.items[c].locked {
			p.cur = c
			return
		}
	}
}

// pickKey is a key the picker acts on.
type pickKey int

const (
	keyNone   pickKey = iota
	keyUp             // Up, k
	keyDown           // Down, j
	keyToggle         // Space, x
	keyAll            // a
	keyEnter          // Enter
	keyEsc            // Esc
	keyYes            // y
	keyNo             // n, Backspace
	keyQuit           // q, Ctrl+C
)

// handle applies a key and reports whether the picker is done, and if so
// whether the person confirmed. In the list Esc skips; in the summary it goes
// back to the list.
func (p *picker) handle(k pickKey) (done, ok bool) {
	if p.stage == stageConfirm {
		switch k {
		case keyEnter, keyYes:
			p.stage = stageDone
			return true, true
		case keyEsc, keyNo:
			p.stage = stageList
		case keyQuit:
			p.stage = stageSkipped
			return true, false
		}
		return false, false
	}
	switch k {
	case keyUp:
		p.move(-1)
	case keyDown:
		p.move(1)
	case keyToggle:
		if p.cur >= 0 {
			p.items[p.cur].on = !p.items[p.cur].on
		}
	case keyAll:
		all := true
		for _, it := range p.items {
			if !it.locked && !it.on {
				all = false
			}
		}
		for i := range p.items {
			if !p.items[i].locked {
				p.items[i].on = !all
			}
		}
	case keyEnter:
		p.stage = stageConfirm
	case keyEsc, keyQuit:
		p.stage = stageSkipped
		return true, false
	}
	return false, false
}

// selected lists the ids of the unlocked items that are on.
func (p *picker) selected() []string {
	var ids []string
	for _, it := range p.items {
		if it.on && !it.locked {
			ids = append(ids, it.id)
		}
	}
	return ids
}

// parseKeys turns what one console read returned (with virtual terminal input
// on) into keys: arrows arrive as ESC [ A/B, Esc alone as a lone ESC.
func parseKeys(s string) []pickKey {
	if s == "\x1b" {
		return []pickKey{keyEsc}
	}
	var keys []pickKey
	for len(s) > 0 {
		switch {
		case strings.HasPrefix(s, "\x1b[A"), strings.HasPrefix(s, "\x1bOA"):
			keys, s = append(keys, keyUp), s[3:]
			continue
		case strings.HasPrefix(s, "\x1b[B"), strings.HasPrefix(s, "\x1bOB"):
			keys, s = append(keys, keyDown), s[3:]
			continue
		case strings.HasPrefix(s, "\x1b["), strings.HasPrefix(s, "\x1bO"):
			// another key (Home, F1, ...): skip its sequence
			i := 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			s = s[min(i+1, len(s)):]
			continue
		}
		var k pickKey
		switch s[0] {
		case 'k', 'K':
			k = keyUp
		case 'j', 'J':
			k = keyDown
		case ' ', 'x', 'X':
			k = keyToggle
		case 'a', 'A':
			k = keyAll
		case '\r', '\n':
			k = keyEnter
		case 0x1b:
			k = keyEsc
		case 'y', 'Y':
			k = keyYes
		case 'n', 'N', 0x08, 0x7f:
			k = keyNo
		case 'q', 'Q', 0x03:
			k = keyQuit
		}
		if k != keyNone {
			keys = append(keys, k)
		}
		_, size := utf8.DecodeRuneInString(s)
		s = s[size:]
	}
	return keys
}

var pickText = struct {
	title, hint, always, detected, confirmTitle, confirmHint, skipped [2]string
}{
	title:        [2]string{"Install the line-cli skill: choose the AI agents that should learn to use line.", "安裝 line-cli skill：選擇要學會使用 line 的 AI agent。"},
	hint:         [2]string{"Up/Down move, Space select, A all, Enter next, Esc skip.", "方向鍵 移動，空白鍵 勾選，A 全選，Enter 下一步，Esc 略過。"},
	always:       [2]string{"always", "固定安裝"},
	detected:     [2]string{"detected", "已偵測到"},
	confirmTitle: [2]string{"The line-cli skill will be installed like this:", "line-cli skill 將這樣安裝："},
	confirmHint:  [2]string{"Enter install, Esc go back, Q cancel.", "Enter 安裝，Esc 返回修改，Q 取消。"},
	skipped:      [2]string{"Skipped: no agent added or removed. Run 'line skill install' to choose later.", "已略過：沒有新增或移除任何 agent。之後可執行 line skill install 再選。"},
}

// render draws the picker as lines, each cut to fit width display columns
// (0: no limit) so none wraps and a redraw can move back over exactly as many
// rows.
func (p *picker) render(l lang, width int) []string {
	var lines []string
	switch p.stage {
	case stageList:
		lines = p.renderList(l)
	case stageConfirm, stageDone:
		lines = append([]string{l.pick(pickText.confirmTitle), ""}, p.summary(p.selected())...)
		if p.stage == stageConfirm {
			lines = append(lines, "", l.pick(pickText.confirmHint))
		}
	case stageSkipped:
		lines = []string{l.pick(pickText.skipped)}
	}
	if width > 0 {
		for i, s := range lines {
			lines[i] = truncate(s, width-1)
		}
	}
	return lines
}

func (p *picker) renderList(l lang) []string {
	labelW, pathW := 0, 0
	for _, it := range p.items {
		labelW = max(labelW, displayWidth(it.label))
		pathW = max(pathW, displayWidth(it.path))
	}
	lines := []string{l.pick(pickText.title), ""}
	for i, it := range p.items {
		cursor := "  "
		if i == p.cur {
			cursor = "> "
		}
		box := "[ ]"
		switch {
		case it.locked:
			box = "[*]"
		case it.on:
			box = "[x]"
		}
		tag := ""
		switch {
		case it.locked:
			tag = l.pick(pickText.always)
		case it.found:
			tag = l.pick(pickText.detected)
		}
		lines = append(lines, strings.TrimRight(cursor+box+" "+pad(it.label, labelW)+"  "+pad(it.path, pathW)+"  "+tag, " "))
		if it.note != "" {
			for _, n := range strings.Split(it.note, "\n") {
				lines = append(lines, "      "+n)
			}
		}
	}
	return append(lines, "", l.pick(pickText.hint))
}

// table lays rows of two columns out with the first padded to one width.
func table(rows [][2]string) []string {
	w := 0
	for _, r := range rows {
		w = max(w, displayWidth(r[0]))
	}
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = "  " + pad(r[0], w) + "  " + r[1]
	}
	return lines
}

func pad(s string, w int) string {
	return s + strings.Repeat(" ", max(0, w-displayWidth(s)))
}

// displayWidth counts the console columns s takes: two for East Asian wide
// characters, one for the rest.
func displayWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

func runeWidth(r rune) int {
	switch {
	case r >= 0x1100 && r <= 0x115f, r >= 0x2e80 && r <= 0xa4cf, r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe4f, r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6, r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}

// truncate cuts s to at most w display columns, ending in "..." when cut.
func truncate(s string, w int) string {
	if displayWidth(s) <= w {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n+runeWidth(r) > w-3 {
			break
		}
		b.WriteRune(r)
		n += runeWidth(r)
	}
	return b.String() + "..."
}
