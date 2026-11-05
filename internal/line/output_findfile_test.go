package line

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFindFileUntilOrderChatTypeAndLocalPath(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	d := outTestDB(t)
	d.chat("c1group")
	d.chat("u-peer-1")
	d.insert("_contact", map[string]any{"_mid": "u-peer-1", "_displayName": "peer"})
	dir := t.TempDir()
	saved := filepath.Join(dir, "kept.txt")
	if err := os.WriteFile(saved, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.Local).UnixMilli()
	file := func(id, chat string, ms int64, name string, info map[string]any) {
		if info == nil {
			info = map[string]any{}
		}
		info["fileName"] = name
		d.msg(id, chat, ms, 14, map[string]any{"_contentInfo": outJSON(t, info), "_contentMetadata": outJSON(t, map[string]any{"FILE_SIZE": "5"})})
	}
	file("f1", "c1group", base, "kept.txt", map[string]any{"downloadedPath": saved, "downloadedFileSize": 5})
	file("f2", "c1group", base+86_400_000, "resized.txt", map[string]any{"downloadedPath": saved, "downloadedFileSize": 6})
	file("f3", "u-peer-1", base+2*86_400_000, "gone.txt", map[string]any{"downloadedPath": filepath.Join(dir, "gone.txt"), "downloadedFileSize": 5})
	file("f4", "u-peer-1", base+3*86_400_000, "plain.txt", nil)

	ids := func(hits []FileHit) string {
		return strings.Join(outStatuses(hits, func(h FileHit) string { return h.ID }), ",")
	}
	hits, err := d.r.FindFile(FindFileOptions{LocalSearch: true})
	if err != nil {
		t.Fatal(err)
	}
	if ids(hits) != "f4,f3,f2,f1" {
		t.Fatalf("default order %s", ids(hits))
	}
	for _, h := range hits {
		if (h.LocalPath != "") != (h.ID == "f1") {
			t.Fatalf("%s: local_path %q", h.ID, h.LocalPath)
		}
	}
	if hits, _ := d.r.FindFile(FindFileOptions{}); hits[3].LocalPath != "" {
		t.Fatal("local_path set without LocalSearch")
	}
	if hits, _ := d.r.FindFile(FindFileOptions{Order: "asc", Limit: 2}); ids(hits) != "f1,f2" {
		t.Fatalf("asc limit 2: %s", ids(hits))
	}
	if hits, _ := d.r.FindFile(FindFileOptions{UntilTs: (base + 86_400_000) / 1000}); ids(hits) != "f2,f1" {
		t.Fatalf("until: %s", ids(hits))
	}
	if hits, _ := d.r.FindFile(FindFileOptions{ChatType: "personal"}); ids(hits) != "f4,f3" {
		t.Fatalf("chat-type personal: %s", ids(hits))
	}
	if hits, err := d.r.FindFile(FindFileOptions{ChatType: "open"}); err != nil || len(hits) != 0 {
		t.Fatalf("chat-type open: %v %v", hits, err)
	}
}
