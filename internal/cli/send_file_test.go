package cli

import (
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/WellWells/line-cli/internal/line"
)

const testBOM = "\xef\xbb\xbf"

func utf16File(s string, order binary.ByteOrder) []byte {
	units := utf16.Encode([]rune(testBOM + s))
	b := make([]byte, 2*len(units))
	for i, u := range units {
		order.PutUint16(b[2*i:], u)
	}
	return b
}

func TestMessageFileText(t *testing.T) {
	const text = "synthetic 測試 \U0001F642\r\nline two\r\n"
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		b, err := messageFileText(utf16File(text, order))
		if err != nil || string(b) != testBOM+text {
			t.Fatalf("%v: got %q, %v", order, b, err)
		}
		msg, normalized, err := line.NormalizeSendText(string(b))
		if err != nil || msg != "synthetic 測試 \U0001F642\nline two" || !normalized {
			t.Errorf("%v: normalized to %q, %v, %v", order, msg, normalized, err)
		}
	}
	for _, in := range []string{"plain text", testBOM + "with BOM", ""} {
		if b, err := messageFileText([]byte(in)); err != nil || string(b) != in {
			t.Errorf("UTF-8 %q: got %q, %v", in, b, err)
		}
	}
	le := utf16File("ab", binary.LittleEndian)
	for _, c := range []struct {
		name string
		in   []byte
		want string
	}{
		{"odd length", le[:len(le)-1], "not valid UTF-16"},
		{"lone high surrogate at end", append(append([]byte{}, le...), 0x3d, 0xd8), "not valid UTF-16"},
		{"lone low surrogate", append(append([]byte{}, le...), 0x00, 0xdc, 0x61, 0x00), "not valid UTF-16"},
		{"ANSI code page", []byte{0x61, 0xa4, 0xa4}, "Set-Content -Encoding UTF8"},
	} {
		_, err := messageFileText(c.in)
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "INVALID_ARGUMENT") {
			t.Errorf("%s: err = %v, want INVALID_ARGUMENT containing %q", c.name, err, c.want)
		}
	}
}
