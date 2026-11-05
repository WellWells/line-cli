package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestZipHasFilesInOrder(t *testing.T) {
	mtime := time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC)
	var buf bytes.Buffer
	if err := writeZip(&buf, []file{{"bin/linecli.exe", []byte("BIN")}, {"README.md", []byte("hi")}}, mtime); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if !f.Modified.Equal(mtime) {
			t.Errorf("%s modified %v, want %v", f.Name, f.Modified, mtime)
		}
		if f.Name == "bin/linecli.exe" {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			if string(b) != "BIN" {
				t.Errorf("bin/linecli.exe holds %q", b)
			}
		}
	}
	if strings.Join(names, ",") != "bin/linecli.exe,README.md" {
		t.Fatalf("entries %v", names)
	}
}

func TestChecksumsMatchInstallScriptFormat(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{asset: "zip", script: "ps1"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeChecksums(dir, []string{script, asset}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, sumFile))
	if err != nil {
		t.Fatal(err)
	}
	ps1, zipSum := sha256.Sum256([]byte("ps1")), sha256.Sum256([]byte("zip"))
	want := hex.EncodeToString(ps1[:]) + "  " + script + "\n" + hex.EncodeToString(zipSum[:]) + "  " + asset + "\n"
	if string(b) != want {
		t.Fatalf("checksums.txt:\n%s\nwant:\n%s", b, want)
	}
	// install.ps1 accepts exactly this line shape for the zip.
	line := strings.Split(want, "\n")[1]
	if !regexp.MustCompile(`^[0-9a-f]{64}  ` + regexp.QuoteMeta(asset) + `$`).MatchString(line) {
		t.Fatalf("zip line %q", line)
	}
}

// install.ps1 is fetched with irm, which may not decode it as UTF-8, and run
// with iex; it must stay ASCII, run inside its own scope and name the asset
// dist writes.
func TestInstallScript(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", script))
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range b {
		if c > 0x7e || (c < 0x20 && c != '\n' && c != '\r' && c != '\t') {
			t.Fatalf("install.ps1 byte %d is 0x%02x; keep the script ASCII", i, c)
		}
	}
	s := string(b)
	if !strings.Contains(s, "\n& {\n") || !strings.HasSuffix(strings.TrimRight(s, "\r\n"), "}") {
		t.Error("install.ps1 must be one '& { ... }' block, so its variables and preferences stay out of the caller's session")
	}
	if !strings.Contains(s, "'"+asset+"'") || !strings.Contains(s, "'"+sumFile+"'") {
		t.Errorf("install.ps1 does not fetch %s and %s", asset, sumFile)
	}
	for _, p := range programs {
		if win := strings.ReplaceAll(p.zipPath, "/", `\`); !strings.Contains(s, "'"+win+"'") {
			t.Errorf("install.ps1 does not install %s from the zip", win)
		}
	}
	if regexp.MustCompile(`(?m)^\s*exit\b`).MatchString(s) {
		t.Error("install.ps1 must throw, not exit: under iex, exit closes the user's terminal")
	}
	// Installing needs no administrator rights: only the user's own folders and
	// HKCU, never the machine's settings, an elevated process or links.
	for _, bad := range []string{"HKLM", "LocalMachine", "RunAs", "-Verb", "Program Files", "ProgramData",
		"SetEnvironmentVariable(", "mklink", "SymbolicLink", "Junction", "New-Service", "Set-ExecutionPolicy"} {
		if strings.Contains(s, bad) {
			t.Errorf("install.ps1 uses %s, which needs administrator rights or makes links", bad)
		}
	}
}

func TestDocumentsExistAndAreFinal(t *testing.T) {
	for _, name := range documents {
		data, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Error(err)
			continue
		}
		if bytes.Contains(data, []byte("DRAFT")) {
			t.Errorf("%s is still marked as a draft", name)
		}
	}
}
