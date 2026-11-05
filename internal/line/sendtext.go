package line

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxTextRunes is LINE's limit on a text message's length in Unicode characters.
const maxTextRunes = 10000

// ReplyTarget is the stored message a reply quotes, for the user to check. It
// is plain local-message metadata (no key material), so it lives on the
// always-built read side; a send fills it in from the local database.
type ReplyTarget struct {
	ID      string `json:"id"`
	Sender  string `json:"sender"`
	SentAt  string `json:"sent_at"`
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
}

// NormalizeSendText validates message text and normalizes it: a leading BOM is
// removed, CRLF and lone CR become LF and trailing line breaks are trimmed.
// normalized is true when the BOM or line-break rewrite changed the text. It is
// pure input handling, so it is available in every build.
func NormalizeSendText(s string) (string, bool, error) {
	if !utf8.ValidString(s) {
		return "", false, &Error{Code: ErrInvalidArgument, Message: "message text is not valid UTF-8; on Windows PowerShell 5.1 set [Console]::OutputEncoding and $OutputEncoding to UTF-8 before piping, or use --message-file with a UTF-8 file"}
	}
	out := strings.TrimPrefix(s, string(rune(0xFEFF)))
	out = strings.ReplaceAll(out, "\r\n", "\n")
	out = strings.ReplaceAll(out, "\r", "\n")
	out = strings.TrimRight(out, "\n")
	normalized := out != strings.TrimRight(s, "\r\n")
	if strings.ContainsRune(out, 0) {
		return "", false, &Error{Code: ErrInvalidArgument, Message: "message text contains NUL characters (UTF-16 input?); pass UTF-8 text"}
	}
	if i := strings.IndexFunc(out, hiddenControl); i >= 0 {
		r, _ := utf8.DecodeRuneInString(out[i:])
		return "", false, &Error{Code: ErrInvalidArgument, Message: fmt.Sprintf("message text contains the control character U+%04X, which LINE does not show; remove it (line breaks and tabs are allowed)", r)}
	}
	if strings.TrimSpace(out) == "" {
		return "", false, &Error{Code: ErrInvalidArgument, Message: "message text is empty"}
	}
	if n := utf8.RuneCountInString(out); n > maxTextRunes {
		return "", false, &Error{Code: ErrInvalidArgument, Message: "message text is too long: " + strconv.Itoa(n) + " characters (limit " + strconv.Itoa(maxTextRunes) + " Unicode characters); split it into several messages"}
	}
	return out, normalized, nil
}

// CheckReplyID checks that id looks like a LINE message id (digits).
func CheckReplyID(id string) error { return CheckMessageID("--reply-to", id) }

// CheckMessageID is CheckReplyID for any flag that names a message.
func CheckMessageID(flag, id string) error {
	if id == "" || strings.Trim(id, "0123456789") != "" {
		return &Error{Code: ErrInvalidArgument, Message: flag + " needs a LINE message id (digits), such as an id from read --ids, search or unread --ids; got " + strconv.Quote(id)}
	}
	return nil
}

// hiddenControl reports a control character other than line feed and tab.
func hiddenControl(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }
