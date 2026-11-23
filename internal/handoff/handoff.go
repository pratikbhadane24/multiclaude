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

	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
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

// Slug converts a working directory to Claude's project directory name:
// every rune that is not an ASCII letter or digit becomes "-".
func Slug(cwd string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, cwd)
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
	return FindLatestExcluding(p, from, cwd, nil)
}

// FindLatestExcluding is FindLatest ignoring the session ids in live. When
// every candidate is live it returns a *LiveError naming the newest one.
func FindLatestExcluding(p paths.Paths, from registry.Account, cwd string, live map[string]bool) (Session, error) {
	projects := filepath.Join(ConfigDir(p, from), projectsDir)
	var pick picker
	pick.scan(filepath.Join(projects, Slug(cwd)), from.Name, live, nil)
	if pick.hasBest {
		return pick.best, nil
	}
	entries, _ := os.ReadDir(projects)
	for _, e := range entries {
		if e.IsDir() {
			pick.scan(filepath.Join(projects, e.Name()), from.Name, live, func(path string) bool { return fileHasCwd(path, cwd) })
		}
	}
	if pick.hasBest {
		return pick.best, nil
	}
	if pick.hasLive {
		return Session{}, &LiveError{ID: pick.newestLive.ID}
	}
	return Session{}, ErrNoSession
}

// picker tracks the newest idle and the newest live session across directories.
type picker struct {
	best, newestLive Session
	hasBest, hasLive bool
}

// scan considers every *.jsonl in dir that passes accept (nil = all).
func (k *picker) scan(dir, account string, live map[string]bool, accept func(path string) bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
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
		s := Session{ID: strings.TrimSuffix(name, jsonlExt), Account: account, ProjectDir: dir, Path: path, ModTime: info.ModTime()}
		if live[s.ID] {
			if !k.hasLive || s.ModTime.After(k.newestLive.ModTime) {
				k.newestLive, k.hasLive = s, true
			}
			continue
		}
		if !k.hasBest || s.ModTime.After(k.best.ModTime) {
			k.best, k.hasBest = s, true
		}
	}
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
