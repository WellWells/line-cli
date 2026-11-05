package line

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FileHit struct {
	ChatID            string `json:"chat_id"`
	ChatName          string `json:"chat_name"`
	ChatType          string `json:"chat_type"`
	Sender            string `json:"sender"`
	SenderID          string `json:"sender_id,omitempty"`
	Filename          string `json:"filename"`
	FileSize          *int64 `json:"file_size,omitempty"`
	SentAt            string `json:"sent_at,omitempty"`
	ExpiresAt         string `json:"expires_at,omitempty"`
	Expired           bool   `json:"expired"`
	E2EE              bool   `json:"e2ee,omitempty"`
	FromMe            bool   `json:"from_me,omitempty"`
	ID                string `json:"id"`
	LocalPath         string `json:"local_path,omitempty"`
	ProbableLocalPath string `json:"probable_local_path,omitempty"`
}

type FindFileOptions struct {
	Query       string
	Ext         string
	ChatID      string
	ChatType    string
	From        string
	SinceTs     int64
	UntilTs     int64
	Limit       int
	Order       string // "asc" for oldest first; newest first otherwise
	LocalSearch bool
}

func fileNameOf(contentInfo, metadata, text string) string {
	if ci := parseMeta(contentInfo); ci != nil {
		if s := metaString(ci, "fileName"); s != "" {
			return s
		}
	}
	if m := parseMeta(metadata); m != nil {
		if s := metaString(m, "FILE_NAME", "fileName"); s != "" {
			return s
		}
	}
	return text
}

func (r *Reader) FindFile(opt FindFileOptions) ([]FileHit, error) {
	if err := r.buildMaps(); err != nil {
		return nil, err
	}
	if err := r.buildContacts(); err != nil {
		return nil, err
	}
	if err := r.buildOwnMids(); err != nil {
		return nil, err
	}
	var where = []string{"_contentType=?"}
	var params = []any{int64(14)}
	if opt.ChatID != "" {
		chatID, err := r.ResolveChat(opt.ChatID, false)
		if err != nil {
			return nil, err
		}
		where = append(where, "_chatId=?")
		params = append(params, chatID)
	} else if opt.ChatType != "" {
		ids, err := r.chatIdsOfType(opt.ChatType)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, nil
		}
		where = append(where, "_chatId IN ("+placeholders(len(ids))+")")
		for _, id := range ids {
			params = append(params, id)
		}
	}
	if opt.From != "" {
		mids, err := r.senderMids(opt.From)
		if err != nil {
			return nil, err
		}
		where = append(where, "_from IN ("+placeholders(len(mids))+")")
		for _, m := range mids {
			params = append(params, m)
		}
	}
	if opt.SinceTs != 0 {
		where = append(where, "_createdTime>=?")
		params = append(params, opt.SinceTs*1000)
	}
	if opt.UntilTs != 0 {
		where = append(where, "_createdTime<=?")
		params = append(params, untilMs(opt.UntilTs))
	}
	for _, term := range []string{strings.TrimSpace(opt.Query), strings.TrimPrefix(strings.TrimSpace(opt.Ext), ".")} {
		if term == "" || strings.ContainsAny(term, `"\`) {
			continue
		}
		pat := "%" + escapeLike(term) + "%"
		where = append(where, `(_contentInfo LIKE ? ESCAPE '\' OR _contentMetadata LIKE ? ESCAPE '\' OR _text LIKE ? ESCAPE '\')`)
		params = append(params, pat, pat, pat)
	}
	scanLimit := saneLimit(opt.Limit, DefaultFindFileLimit)
	order := "DESC"
	if strings.EqualFold(opt.Order, "asc") {
		order = "ASC"
	}
	sql := "SELECT * FROM _message WHERE " + strings.Join(where, " AND ") + " ORDER BY _createdTime " + order

	st, err := r.query(sql, params...)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, nil
	}
	defer st.Close()
	idx := colIndex(st)
	nowMs := time.Now().UnixMilli()
	q := strings.ToLower(strings.TrimSpace(opt.Query))
	ext := strings.ToLower(strings.TrimPrefix(opt.Ext, "."))
	var out []FileHit
	for st.Step() {
		if len(out) >= scanLimit {
			break
		}
		name := fileNameOf(st.ColumnText(idx["_contentInfo"]), st.ColumnText(idx["_contentMetadata"]), st.ColumnText(idx["_text"]))
		if name == "" {
			continue
		}
		lname := strings.ToLower(name)
		if q != "" && !strings.Contains(lname, q) {
			continue
		}
		if ext != "" && !strings.HasSuffix(lname, "."+ext) {
			continue
		}
		meta := parseMeta(st.ColumnText(idx["_contentMetadata"]))
		from := st.ColumnText(idx["_from"])
		chatID := st.ColumnText(idx["_chatId"])
		info := r.maps.resolveChat(chatID)
		hit := FileHit{
			ChatID: chatID, ChatName: info.name, ChatType: info.typ,
			Sender:   r.displaySender(from, st.ColumnText(idx["_contentInfo"]), ""),
			SenderID: from,
			Filename: name,
			FileSize: metaNumber(meta, "FILE_SIZE"),
			SentAt:   tsToIso(st.ColumnInt64(idx["_createdTime"])),
			FromMe:   from != "" && r.ownMids[from],
			ID:       st.ColumnText(idx["_id"]),
		}
		if exp := metaNumber(meta, "FILE_EXPIRE_TIMESTAMP"); exp != nil {
			hit.ExpiresAt = tsToIso(*exp)
			hit.Expired = nowMs > *exp
		}
		if v, ok := meta["e2eeMark"]; ok {
			switch x := v.(type) {
			case float64:
				hit.E2EE = x != 0
			case string:
				hit.E2EE = x != "" && x != "0"
			}
		}
		if opt.LocalSearch {
			hit.LocalPath = downloadedPath(st.ColumnText(idx["_contentInfo"]), hit.FileSize)
			if hit.LocalPath == "" && hit.FileSize != nil {
				hit.ProbableLocalPath = findLocalFile(name, *hit.FileSize)
			}
		}
		out = append(out, hit)
	}
	return out, stepErr(st)
}

// downloadedPath returns the path LINE recorded when this PC saved the file
// (_contentInfo.downloadedPath), if that file still exists with the recorded size.
func downloadedPath(contentInfo string, fileSize *int64) string {
	ci := parseMeta(contentInfo)
	p := metaString(ci, "downloadedPath")
	if p == "" {
		return ""
	}
	size := metaNumber(ci, "downloadedFileSize")
	if size == nil {
		size = fileSize
	}
	if size == nil {
		return ""
	}
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Size() == *size {
		return p
	}
	return ""
}

func localSearchDirs() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		for _, d := range []string{"Downloads", "Desktop", "Documents"} {
			dirs = append(dirs, filepath.Join(home, d))
		}
	}
	if up := os.Getenv("USERPROFILE"); up != "" {
		matches, _ := filepath.Glob(filepath.Join(up, "OneDrive*"))
		dirs = append(dirs, matches...)
	}
	return dirs
}

func findLocalFile(name string, size int64) string {
	base := filepath.Base(name)
	for _, dir := range localSearchDirs() {
		p := filepath.Join(dir, base)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Size() == size {
			return p
		}
	}
	return ""
}
