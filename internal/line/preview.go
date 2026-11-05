package line

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PreviewResult describes one media message and whether its bytes were recovered
// from LINE's local cache.
type PreviewResult struct {
	ID       string `json:"id"`
	ChatID   string `json:"chat_id"`
	ChatName string `json:"chat_name"`
	Type     string `json:"type"`
	Sender   string `json:"sender"`
	SenderID string `json:"sender_id,omitempty"`
	SentAt   string `json:"sent_at,omitempty"`
	FromMe   bool   `json:"from_me,omitempty"`
	Status   string `json:"status"` // saved | not_cached | server_only | unsupported | write_failed
	SavedAs  string `json:"saved_as,omitempty"`
	Bytes    int    `json:"bytes,omitempty"`
	Note     string `json:"note,omitempty"`
}

// PreviewOptions controls which messages Preview considers.
type PreviewOptions struct {
	ChatArg string
	SinceTs int64
	UntilTs int64
	Limit   int
	OutDir  string
}

// openChatMediaNote explains why open-chat (Square) media can never be recovered:
// those rows carry no keyMaterial and no SID/OID, and their cached thumbnails are
// encrypted with a client-wide key instead of a per-message one.
const openChatMediaNote = "open-chat media: LINE stores no per-message key or server object id for it on this PC, so line-cli cannot recover it; view it in LINE"

// isOpenChatMessage reports whether _toType marks an open chat (4) or one of its threads (7).
func isOpenChatMessage(toType int64) bool { return toType == 4 || toType == 7 }

// Preview recovers locally-cached media (image and video thumbnails) for a chat,
// decrypts each with its per-message keyMaterial, and writes them to OutDir.
// Like read, it takes the first N from --since when given, otherwise the newest
// N up to --until or now; results are always oldest first.
//
// LINE only caches medium-resolution thumbnails of images and video poster
// frames; full-resolution originals and shared files (type 14) live only on
// LINE's obs servers and are never on disk unless explicitly saved, so they are
// reported with status "server_only" rather than recovered.
func (r *Reader) Preview(opt PreviewOptions) ([]PreviewResult, error) {
	if err := r.buildMaps(); err != nil {
		return nil, err
	}
	if err := r.buildContacts(); err != nil {
		return nil, err
	}
	if err := r.buildOwnMids(); err != nil {
		return nil, err
	}
	chatID, err := r.ResolveChat(opt.ChatArg, false)
	if err != nil {
		return nil, err
	}
	outDir, err := absOutDir(opt.OutDir, DefaultMediaDir())
	if err != nil {
		return nil, err
	}

	where := []string{"_chatId=?", "_contentType IN (1,2,14)", sqlPlainKind}
	params := []any{chatID}
	if opt.SinceTs != 0 {
		where = append(where, "_createdTime>=?")
		params = append(params, opt.SinceTs*1000)
	}
	if opt.UntilTs != 0 {
		where = append(where, "_createdTime<=?")
		params = append(params, untilMs(opt.UntilTs))
	}
	newest := opt.SinceTs == 0
	order := "ASC"
	if newest {
		order = "DESC"
	}
	sql := "SELECT _id,_from,_toType,_contentType,_contentInfo,_createdTime FROM _message WHERE " +
		strings.Join(where, " AND ") + " ORDER BY _createdTime " + order + " LIMIT ?"
	st, err := r.query(sql, append(params, saneLimit(opt.Limit, DefaultPreviewLimit))...)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, nil
	}
	defer st.Close()
	info := r.maps.resolveChat(chatID)

	type row struct {
		res   PreviewResult
		ct    int64
		open  bool
		km    string
		thumb string
	}
	var rows []row
	for st.Step() {
		from := st.ColumnText(1)
		ci := parseMeta(st.ColumnText(4))
		rows = append(rows, row{
			res: PreviewResult{
				ID:       st.ColumnText(0),
				ChatID:   chatID,
				ChatName: info.name,
				Type:     orElse(contentType[st.ColumnInt64(3)], "type"),
				Sender:   r.displaySender(from, st.ColumnText(4), ""),
				SenderID: from,
				SentAt:   tsToIso(st.ColumnInt64(5)),
				FromMe:   from != "" && r.ownMids[from],
			},
			ct:    st.ColumnInt64(3),
			open:  isOpenChatMessage(st.ColumnInt64(2)),
			km:    metaString(ci, "keyMaterial"),
			thumb: metaString(ci, "thumbPath"),
		})
	}
	if err := stepErr(st); err != nil {
		return nil, err
	}
	if newest {
		reverse(rows)
	}

	out := make([]PreviewResult, 0, len(rows))
	madeDir := false
	for _, rw := range rows {
		res := rw.res
		switch {
		case rw.open:
			res.Status = "unsupported"
			res.Note = openChatMediaNote
		case rw.ct == 14:
			res.Status = "server_only"
			res.Note = "file is not cached locally; only on LINE's server"
		case rw.thumb == "" || rw.km == "":
			res.Status = "not_cached"
			res.Note = "no cached thumbnail on this PC"
		}
		if res.Status != "" {
			out = append(out, res)
			continue
		}
		blob, err := os.ReadFile(rw.thumb)
		if err != nil {
			res.Status = "not_cached"
			res.Note = "thumbnail evicted from cache"
			out = append(out, res)
			continue
		}
		plain, err := decryptE2EEFile(rw.km, blob)
		if err != nil {
			res.Status = "not_cached"
			res.Note = "decrypt failed: " + err.Error()
			out = append(out, res)
			continue
		}
		if !madeDir {
			if err := os.MkdirAll(outDir, 0o700); err != nil {
				return nil, &Error{Code: ErrInvalidArgument, Message: "cannot create output directory: " + err.Error()}
			}
			madeDir = true
		}
		dst := filepath.Join(outDir, previewFileName(res.SentAt, res.ID, plain))
		if err := os.WriteFile(dst, plain, 0o600); err != nil {
			res.Status = "write_failed"
			res.Note = "write failed: " + err.Error()
			out = append(out, res)
			continue
		}
		res.Status = "saved"
		res.SavedAs = dst
		res.Bytes = len(plain)
		if rw.ct == 2 {
			res.Note = "video poster thumbnail (not the full clip)"
		}
		out = append(out, res)
	}
	return out, nil
}

// absOutDir returns dir (or fallback when dir is empty) as an absolute path, so
// saved_as is absolute whatever the caller's working directory.
func absOutDir(dir, fallback string) (string, error) {
	if dir == "" {
		dir = fallback
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", &Error{Code: ErrInvalidArgument, Message: "invalid output directory: " + err.Error()}
	}
	return abs, nil
}

// previewFileName builds a stable, sortable file name from the send time and id,
// with an extension sniffed from the decrypted bytes.
func previewFileName(sentAt, id string, b []byte) string {
	ts := sentAt
	if t, err := time.Parse("2006-01-02T15:04:05-07:00", sentAt); err == nil {
		ts = t.Format("20060102-150405")
	}
	return ts + "_" + id + sniffExt(b)
}

func sniffExt(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return ".jpg"
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return ".png"
	case len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"):
		return ".gif"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return ".webp"
	default:
		return ".bin"
	}
}
