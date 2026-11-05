package line

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// LINE only lets a file be sent when its extension is on a list its server
// gives the apps (the setting function.file.extension.whitelist); a file with
// no extension may always be sent. LINE PC stores its copy of that list, so
// line-cli checks against the list LINE itself uses now.

const fileTypesSetting = "function.file.extension.whitelist"

// lineExt is the extension LINE checks: what follows the last dot, if any.
var lineExt = regexp.MustCompile(`\.([^.]+)$`)

func fileExtOf(name string) string {
	if m := lineExt.FindStringSubmatch(name); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// allowedFileTypes returns LINE's list of the extensions it lets you send, as
// LINE PC stored it; ok is false when LINE PC has not stored it.
func (r *Reader) allowedFileTypes() (map[string]bool, bool) {
	st, err := r.query(`SELECT CAST(_value AS TEXT) FROM _setting WHERE _key = 'setting'`)
	if err != nil || st == nil {
		return nil, false
	}
	defer st.Close()
	if !st.Step() {
		return nil, false
	}
	return parseAllowedFileTypes(st.ColumnText(0))
}

func parseAllowedFileTypes(setting string) (map[string]bool, bool) {
	var s struct {
		Sck struct {
			ConfigMap map[string]json.RawMessage `json:"configMap"`
		} `json:"_sck"`
	}
	if json.Unmarshal([]byte(setting), &s) != nil {
		return nil, false
	}
	var list string
	if json.Unmarshal(s.Sck.ConfigMap[fileTypesSetting], &list) != nil {
		return nil, false
	}
	allowed := map[string]bool{}
	for _, e := range strings.Split(list, ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			allowed[e] = true
		}
	}
	return allowed, len(allowed) > 0
}

func refuseFileTypes(names []string, allowed map[string]bool) error {
	var bad []string
	seen := map[string]bool{}
	for _, name := range names {
		if ext := fileExtOf(name); ext != "" && !allowed[ext] && !seen[ext] {
			seen[ext] = true
			bad = append(bad, "."+ext)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return &Error{Code: ErrInvalidArgument, Message: "LINE does not let you send " + strings.Join(bad, ", ") + " files, so nothing was sent; put them in a ZIP file and send that, as LINE advises"}
}
