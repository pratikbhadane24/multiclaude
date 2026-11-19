// Package handoff locates the latest Claude conversation for a folder. History
// is shared by all accounts, so any account can resume what it finds.
package handoff

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
)

const (
	projectsDir  = "projects"
	jsonlExt     = ".jsonl"
	maxScanLines = 20
	maxLineBytes = 1 << 20
	scanBufBytes = 64 * 1024
)

// ErrNoSession means no conversation exists for the folder.
var ErrNoSession = errors.New("no conversation found")

// Session is one conversation transcript.
type Session struct {
	ID, Account, ProjectDir, Path string
	ModTime                       time.Time
}

// Slug converts a working directory to Claude's project directory name.
func Slug(cwd string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(cwd)
}

// ConfigDir returns the config directory of an account.
func ConfigDir(p paths.Paths, a registry.Account) string {
	if a.IsMain() {
		return p.ClaudeDir
	}
	return a.Dir
}

// FindLatest returns the newest session for cwd in account from.
func FindLatest(p paths.Paths, from registry.Account, cwd string) (Session, error) {
	projects := filepath.Join(ConfigDir(p, from), projectsDir)
	if s, ok := newestIn(filepath.Join(projects, Slug(cwd)), from.Name, nil); ok {
		return s, nil
	}
	var best Session
	found := false
	entries, _ := os.ReadDir(projects)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, ok := newestIn(filepath.Join(projects, e.Name()), from.Name, func(path string) bool { return fileHasCwd(path, cwd) })
		if ok && (!found || s.ModTime.After(best.ModTime)) {
			best, found = s, true
		}
	}
	if !found {
		return Session{}, ErrNoSession
	}
	return best, nil
}

// newestIn returns the newest *.jsonl in dir that passes accept (nil = all).
func newestIn(dir, account string, accept func(path string) bool) (Session, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Session{}, false
	}
	var best Session
	found := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, jsonlExt) {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(dir, name)
		if accept != nil && !accept(path) {
			continue
		}
		if !found || info.ModTime().After(best.ModTime) {
			best = Session{ID: strings.TrimSuffix(name, jsonlExt), Account: account, ProjectDir: dir, Path: path, ModTime: info.ModTime()}
			found = true
		}
	}
	return best, found
}

// fileHasCwd reports whether one of the first lines carries "cwd" == cwd.
// Bad lines are skipped; an over-long line ends the scan of that file.
func fileHasCwd(path, cwd string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, scanBufBytes), maxLineBytes)
	for i := 0; i < maxScanLines && sc.Scan(); i++ {
		var line struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(sc.Bytes(), &line) == nil && line.Cwd == cwd {
			return true
		}
	}
	return false
}
