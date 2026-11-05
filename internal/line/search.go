package line

import (
	"bytes"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ncruces/go-sqlite3"
)

// Shared row classes: an unsent row keeps its original _contentType; event rows
// are _type 3 system rows plus the code-18 chat notices. Every command that
// leaves events or unsent rows out (stats, unread counts, members, sender
// ranking) uses these.
const (
	sqlUnsent        = "_attribute=1"
	sqlEvent         = "(_type=3 OR _contentType=18)"
	sqlEventOrUnsent = "(" + sqlEvent + " OR " + sqlUnsent + ")"
	sqlPlainKind     = "NOT " + sqlEventOrUnsent
)

func jsonText(col, key string) string {
	return "CASE WHEN json_valid(" + col + ") THEN nullif(json_extract(" + col + ",'$." + key + "'),'') END"
}

func metaText(keys ...string) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = jsonText("_contentMetadata", k)
	}
	return strings.Join(parts, ",")
}

// renamedSQL is the new chat name a renamed event shows as content: the second
// LOC_ARGS field (fields are separated by char 30) of a C_PN notice, or the param
// of _eventInfo type 18.
const renamedSQL = `CASE
 WHEN _contentType=18 THEN CASE WHEN json_valid(_contentMetadata) AND json_extract(_contentMetadata,'$.LOC_KEY')='C_PN' THEN
  (SELECT CASE WHEN instr(a,char(30))>0 THEN substr(a,1,instr(a,char(30))-1) ELSE a END
   FROM (SELECT substr(v,instr(v,char(30))+1) AS a FROM (SELECT json_extract(_contentMetadata,'$.LOC_ARGS') AS v) WHERE instr(v,char(30))>0)) END
 WHEN json_valid(_eventInfo) AND CAST(json_extract(_eventInfo,'$.type') AS INTEGER)=18 AND json_type(_eventInfo,'$.param')='text' THEN json_extract(_eventInfo,'$.param')
END`

// visibleSQL is the text search matches for a row: what read shows as content,
// file name, link preview, post title or description, contact card name or bot
// alt text. Metadata keys and FLEX/MARKUP JSON are never searched. Unsent rows
// show nothing; of event rows only a renamed event's new name is matched.
var visibleSQL = `CASE
 WHEN ` + sqlUnsent + ` THEN ''
 WHEN ` + sqlEvent + ` THEN coalesce(` + renamedSQL + `,'')
 WHEN _contentType=0 THEN coalesce(_text,'') || CASE WHEN json_valid(_urlPreview) THEN coalesce((SELECT group_concat(coalesce(json_extract(value,'$.title'),'') || char(10) || coalesce(json_extract(value,'$.desc'),''), char(10)) FROM json_each(_urlPreview,'$.urlPreview')),'') ELSE '' END
 WHEN _contentType IN (1,2,3,7) THEN ''
 WHEN _contentType=14 THEN coalesce(` + jsonText("_contentInfo", "fileName") + `,` + metaText("FILE_NAME", "fileName") + `,'') || char(10) || coalesce(_text,'')
 WHEN _contentType=13 THEN coalesce(` + metaText("displayName") + `,_text,'')
 WHEN _contentType=16 THEN coalesce(` + metaText("officialName", "albumName", "serviceName", "title") + `,'') || char(10) || coalesce(` + metaText("text", "desc") + `,'')
 WHEN _contentType IN (17,22) THEN coalesce(` + metaText("ALT_TEXT") + `,_text,'')
 WHEN _contentType=15 THEN coalesce(_text,'') || char(10) || coalesce(` + jsonText("_location", "address") + `,'') || char(10) || coalesce(` + jsonText("_location", "title") + `,'')
 ELSE coalesce(_text,'')
END`

// urlSQL selects text messages with a URL or a link preview that has a title or description.
const urlSQL = `_contentType=0 AND ` + sqlPlainKind + ` AND (_text LIKE '%http://%' OR _text LIKE '%https://%' OR (json_valid(_urlPreview) AND EXISTS (SELECT 1 FROM json_each(_urlPreview,'$.urlPreview') WHERE json_extract(value,'$.title')<>'' OR json_extract(value,'$.desc')<>'')))`

// SearchTypeNames lists the values search --type accepts: every output type,
// url (messages with a URL), notice (code-18 chat notices, also type_18) and
// type_N. link, the older name of post, is accepted too.
func SearchTypeNames() []string {
	return append(ContentTypeNames(), "url", "notice", "type_N")
}

// typeFilter turns a --type value into a WHERE clause.
func typeFilter(name string) (string, []any, bool) {
	switch name {
	case "unsent":
		return sqlUnsent, nil, true
	case "event":
		return sqlEvent + " AND _attribute<>1", nil, true
	case "notice", "type_18":
		return "_contentType=18 AND _attribute<>1", nil, true
	case "url":
		return urlSQL, nil, true
	case "link":
		name = "post"
	}
	code, ok := contentTypeCode(name)
	if !ok {
		n, err := strconv.ParseInt(strings.TrimPrefix(name, "type_"), 10, 64)
		if !strings.HasPrefix(name, "type_") || err != nil || n < 0 {
			return "", nil, false
		}
		code = n
	}
	return "_contentType=? AND " + sqlPlainKind, []any{code}, true
}

// foldRune maps full-width ASCII (U+FF01-U+FF5E) and the ideographic space to
// half-width, then lowercases.
func foldRune(r rune) rune {
	switch {
	case r >= 0xFF01 && r <= 0xFF5E:
		r -= 0xFEE0
	case r == 0x3000:
		r = ' '
	}
	return unicode.ToLower(r)
}

func appendFold(dst, src []byte) []byte {
	for i := 0; i < len(src); {
		c := src[i]
		if c < utf8.RuneSelf {
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			dst = append(dst, c)
			i++
			continue
		}
		r, n := utf8.DecodeRune(src[i:])
		dst = utf8.AppendRune(dst, foldRune(r))
		i += n
	}
	return dst
}

func foldString(s string) string { return string(appendFold(nil, []byte(s))) }

// searchTerms splits a query into folded terms: whitespace separates words,
// double quotes keep a phrase together.
func searchTerms(q string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for _, r := range foldString(q) {
		switch {
		case r == '"' || r == '“' || r == '”':
			flush()
			quoted = !quoted
		case !quoted && unicode.IsSpace(r):
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// registerMatch defines linecli_match(text, terms) on conn: 1 when the folded
// text contains every \x1f-separated, already folded term.
func registerMatch(conn *sqlite3.Conn) error {
	var buf, last []byte
	var terms [][]byte
	return conn.CreateFunction("linecli_match", 2, sqlite3.DETERMINISTIC|sqlite3.INNOCUOUS, func(ctx sqlite3.Context, arg ...sqlite3.Value) {
		if t := arg[1].RawText(); terms == nil || !bytes.Equal(t, last) {
			last = append(last[:0], t...)
			terms = bytes.Split(last, []byte{0x1f})
		}
		buf = appendFold(buf[:0], arg[0].RawText())
		for _, t := range terms {
			if len(t) > 0 && !bytes.Contains(buf, t) {
				ctx.ResultInt(0)
				return
			}
		}
		ctx.ResultInt(1)
	})
}
