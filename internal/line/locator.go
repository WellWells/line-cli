package line

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const dbSubdir = "db"

var excludePrefixes = []string{"album_", "keep_", "chatstats_"}

func dataDirCandidates() []string {
	var out []string
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		out = append(out, filepath.Join(la, "LINE", "Data"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, "AppData", "Local", "LINE", "Data"))
	}
	return out
}

func firstExistingDir(cands []string) string {
	for _, c := range cands {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c
		}
	}
	return ""
}

func isDbFile(name string) bool {
	ln := strings.ToLower(name)
	if strings.HasSuffix(ln, "-wal") || strings.HasSuffix(ln, "-shm") {
		return false
	}
	return strings.HasSuffix(ln, ".db") || strings.HasSuffix(ln, ".edb")
}

type dbEntry struct {
	name string
	path string
	size int64
}

func collectDbEntries(dataDir string) []dbEntry {
	var out []dbEntry
	seen := map[string]bool{}
	scan := func(dir string) {
		ents, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range ents {
			if e.IsDir() || !isDbFile(e.Name()) {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if seen[strings.ToLower(e.Name())] {
				continue
			}
			seen[strings.ToLower(e.Name())] = true
			fi, err := e.Info()
			if err != nil {
				continue
			}
			out = append(out, dbEntry{e.Name(), p, fi.Size()})
		}
	}
	scan(dataDir)
	scan(filepath.Join(dataDir, dbSubdir))
	return out
}

func pickMainEdb(entries []dbEntry) string {
	var edbs []dbEntry
	for _, e := range entries {
		if strings.HasSuffix(strings.ToLower(e.name), ".edb") {
			edbs = append(edbs, e)
		}
	}
	if len(edbs) == 0 {
		return ""
	}
	var filtered []dbEntry
	for _, e := range edbs {
		ln := strings.ToLower(e.name)
		excluded := false
		for _, p := range excludePrefixes {
			if strings.HasPrefix(ln, p) {
				excluded = true
				break
			}
		}
		if !excluded {
			filtered = append(filtered, e)
		}
	}
	use := filtered
	if len(use) == 0 {
		use = edbs
	}
	sort.Slice(use, func(i, j int) bool { return use[i].size > use[j].size })
	return use[0].path
}

type Location struct {
	Platform               string      `json:"platform"`
	KeyExtractionSupported bool        `json:"key_extraction_supported"`
	Candidates             []string    `json:"candidates"`
	DataDir                string      `json:"data_dir"`
	MainEdb                string      `json:"main_edb"`
	DbFiles                []StorageDB `json:"db_files"`
}

type StorageDB struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

func Locate() (Location, error) {
	cands := dataDirCandidates()
	loc := Location{
		Platform:               "windows",
		KeyExtractionSupported: true,
		Candidates:             cands,
	}
	dataDir := firstExistingDir(cands)
	if dataDir == "" {
		return loc, &Error{Code: ErrDBNotFound, Message: "LINE data directory not found"}
	}
	loc.DataDir = dataDir
	entries := collectDbEntries(dataDir)
	for _, e := range entries {
		loc.DbFiles = append(loc.DbFiles, StorageDB{e.name, e.size})
	}
	loc.MainEdb = pickMainEdb(entries)
	if loc.MainEdb == "" {
		return loc, &Error{Code: ErrDBNotFound, Message: "no LINE database file found"}
	}
	return loc, nil
}
