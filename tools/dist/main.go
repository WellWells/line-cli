// Command dist builds the release files into dist/, as they are published on
// GitHub Releases: line-cli_windows_amd64.zip (bin/linecli.exe, the program;
// cmd/line.exe, the line command that runs it; README.md and so on),
// install.ps1 and checksums.txt. With -install it then runs install.ps1 on
// those files the way "irm .../install.ps1 | iex" installs a release, so each
// build is installed on this computer with one command:
//
//	go run ./tools/dist -install
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	asset   = "line-cli_windows_amd64.zip"
	script  = "install.ps1"
	sumFile = "checksums.txt"
)

// programs are built into the zip at the paths they get when installed: only
// cmd goes on PATH, so 'linec' + Tab completes nothing.
var programs = []struct{ pkg, zipPath string }{
	{"./cmd/linecli", "bin/linecli.exe"},
	{"./cmd/line", "cmd/line.exe"},
}

// documents go into the zip beside the programs. Each is required: a release
// without its license terms must not build.
var documents = []string{"README.md", "LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.txt"}

type file struct {
	Name string
	Data []byte
}

// writeZip writes the files into a zip archive, each stamped with mtime.
func writeZip(w io.Writer, files []file, mtime time.Time) error {
	zw := zip.NewWriter(w)
	for _, f := range files {
		h := &zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: mtime}
		h.SetMode(0o644)
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := fw.Write(f.Data); err != nil {
			return err
		}
	}
	return zw.Close()
}

// writeChecksums writes checksums.txt into dir in the sha256sum format
// install.ps1 reads: "<sha256>  <name>", one line per file, sorted by name.
func writeChecksums(dir string, names []string) error {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	var b strings.Builder
	for _, n := range sorted {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), n)
	}
	return os.WriteFile(filepath.Join(dir, sumFile), []byte(b.String()), 0o644)
}

// buildTime is SOURCE_DATE_EPOCH when set, for reproducible archives, else now.
func buildTime() time.Time {
	if s := os.Getenv("SOURCE_DATE_EPOCH"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(n, 0).UTC()
		}
	}
	return time.Now().UTC()
}

func build(out, pkg string) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", out, pkg)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=windows", "GOARCH=amd64")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// install runs dist's install.ps1 through Invoke-Expression, as irm | iex
// does, with LINECLI_DOWNLOAD_URL pointing it at dist instead of GitHub.
func install(dist string) error {
	if runtime.GOOS != "windows" {
		return errors.New("-install works on Windows only")
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"[IO.File]::ReadAllText($env:LINECLI_INSTALL_SCRIPT) | Invoke-Expression")
	// Windows PowerShell started from PowerShell 7 inherits its PSModulePath and
	// then cannot load its own modules; without it, it uses its default.
	var env []string
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); !strings.EqualFold(name, "PSModulePath") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "LINECLI_DOWNLOAD_URL="+dist, "LINECLI_INSTALL_SCRIPT="+filepath.Join(dist, script))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func run() error {
	out := flag.String("out", "dist", "output directory")
	doInstall := flag.Bool("install", false, "then install the build on this computer with install.ps1")
	flag.Parse()

	if _, err := os.Stat("go.mod"); err != nil {
		return errors.New("run from the repository root")
	}
	dist, err := filepath.Abs(*out)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dist, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "linecli-dist-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	var files []file
	for _, p := range programs {
		exe := filepath.Join(tmp, filepath.FromSlash(p.zipPath))
		if err := build(exe, p.pkg); err != nil {
			return fmt.Errorf("build %s: %w", p.pkg, err)
		}
		data, err := os.ReadFile(exe)
		if err != nil {
			return err
		}
		files = append(files, file{p.zipPath, data})
	}
	for _, name := range documents {
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		files = append(files, file{name, data})
	}
	f, err := os.Create(filepath.Join(dist, asset))
	if err != nil {
		return err
	}
	err = writeZip(f, files, buildTime())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	data, err := os.ReadFile(script)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dist, script), data, 0o644); err != nil {
		return err
	}
	if err := writeChecksums(dist, []string{asset, script}); err != nil {
		return err
	}
	zi, err := os.Stat(filepath.Join(dist, asset))
	if err != nil {
		return err
	}
	fmt.Printf("%s  %.1f MB\n", filepath.Join(dist, asset), float64(zi.Size())/1e6)
	if *doInstall {
		return install(dist)
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "dist:", err)
		os.Exit(1)
	}
}
