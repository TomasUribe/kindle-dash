package main

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The files that go on the Kindle live inside this program, so there is nothing else to download.
// run.sh, the two library entries and cacert.pem are committed; kdash.gz (the ARM program) and icon.png
// are made by tools/build-release.sh.
//
//go:embed all:payload
var embedded embed.FS

// payloadFS is what gets installed; tests swap it for a small fake.
var payloadFS fs.FS = embedded

const (
	installDir = "kindle-dash"
	entryName  = "Kindle Dash.sh"
	diagName   = "Kindle Dash Diagnose.sh"
)

// isKindle says whether a folder looks like the top of a Kindle's USB drive.
func isKindle(root string) bool {
	for _, d := range []string{"documents", "system"} {
		if st, err := os.Stat(filepath.Join(root, d)); err != nil || !st.IsDir() {
			return false
		}
	}
	return true
}

// findKindles looks in the places operating systems mount USB drives.
func findKindles() []string {
	var roots []string
	switch runtime.GOOS {
	case "windows":
		for c := 'A'; c <= 'Z'; c++ {
			roots = append(roots, fmt.Sprintf("%c:\\", c))
		}
	case "darwin":
		roots = globAll("/Volumes/*")
	default:
		user := os.Getenv("USER")
		for _, pat := range []string{"/media/" + user + "/*", "/run/media/" + user + "/*", "/media/*", "/mnt/*"} {
			roots = append(roots, globAll(pat)...)
		}
	}
	var found []string
	seen := map[string]bool{}
	for _, r := range roots {
		if !seen[r] && isKindle(r) {
			seen[r] = true
			found = append(found, r)
		}
	}
	return found
}

func globAll(pattern string) []string {
	m, _ := filepath.Glob(pattern)
	return m
}

// hasJailbreak looks for the folder the jailbreak's hotfix leaves behind; without it the library cannot
// run scripts, so Kindle Dash would just sit there as a book that does nothing.
func hasJailbreak(root string) bool {
	st, err := os.Stat(filepath.Join(root, "libkh"))
	return err == nil && st.IsDir()
}

// writeFile writes and flushes: the drive may be unplugged soon after.
func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func payloadFile(name string) ([]byte, error) {
	return fs.ReadFile(payloadFS, "payload/"+name)
}

// kindleProgram is the ARM executable, unpacked.
func kindleProgram() ([]byte, error) {
	gz, err := payloadFile("kdash.gz")
	if err != nil {
		return nil, errors.New("this copy of the installer was built without the Kindle program; use a released download or run tools/build-release.sh")
	}
	r, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// scriptletWithIcon puts the icon into the library entry's header, the way the library wants it.
func scriptletWithIcon(script, icon []byte) []byte {
	if len(icon) == 0 {
		return script
	}
	lines := strings.Split(string(script), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "# Icon:") {
			lines[i] = "# Icon: data:image/png;base64," + base64.StdEncoding.EncodeToString(icon)
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// installProgram copies the program, its launcher and the two library entries.
func installProgram(root string) error {
	prog, err := kindleProgram()
	if err != nil {
		return err
	}
	icon, _ := payloadFile("icon.png")
	dir := filepath.Join(root, installDir)
	put := func(name string, mode os.FileMode) error {
		b, err := payloadFile(name)
		if err != nil {
			return err
		}
		return writeFile(filepath.Join(dir, name), b, mode)
	}
	if err := writeFile(filepath.Join(dir, "kdash"), prog, 0o755); err != nil {
		return err
	}
	for _, n := range []string{"run.sh", "cacert.pem"} {
		if err := put(n, 0o644); err != nil {
			return err
		}
	}
	if len(icon) > 0 {
		if err := writeFile(filepath.Join(dir, "icon.png"), icon, 0o644); err != nil {
			return err
		}
	}
	for _, n := range []string{entryName, diagName} {
		b, err := payloadFile(n)
		if err != nil {
			return err
		}
		if err := writeFile(filepath.Join(root, "documents", n), scriptletWithIcon(b, icon), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// settings is config.json as a loose map, so options this program does not ask about survive a re-run.
type settings map[string]any

// loadSettings reads the config already on the Kindle; found says whether there was one.
func loadSettings(root string) (s settings, found bool) {
	s = settings{}
	b, err := os.ReadFile(filepath.Join(root, installDir, "config.json"))
	if err != nil {
		return s, false
	}
	if json.Unmarshal(b, &s) != nil {
		return settings{}, false
	}
	return s, true
}

func (s settings) calendarCount() int {
	list, _ := s["calendars"].([]any)
	return len(list)
}

func (s settings) clientID() string {
	v, _ := s["spotify_client_id"].(string)
	return v
}

// saveSettings writes config.json (secret calendar addresses inside) and, if there is one, the login.
func saveSettings(root string, s settings, tok *tokenFile) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(root, installDir)
	if err := writeFile(filepath.Join(dir, "config.json"), b, 0o600); err != nil {
		return err
	}
	if tok != nil {
		tb, _ := json.Marshal(tok)
		return writeFile(filepath.Join(dir, "spotify_token.json"), tb, 0o600)
	}
	return nil
}

// removeInstall takes everything this program put on the drive off again.
func removeInstall(root string) error {
	for _, n := range []string{entryName, diagName} {
		if err := os.Remove(filepath.Join(root, "documents", n)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.RemoveAll(filepath.Join(root, installDir))
}
