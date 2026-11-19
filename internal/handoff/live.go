package handoff

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
)

const (
	sessionsDir = "sessions"
	liveExt     = ".json"
)

// pidAlive reports whether a process exists (EPERM means it does); tests
// replace it.
var pidAlive = func(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// LiveError means every candidate conversation is open in a running claude.
type LiveError struct{ ID string }

func (e *LiveError) Error() string {
	return fmt.Sprintf("the latest chat here is still open in another window (session %s); close it first or pass --session", e.ID)
}

// LiveSessions returns the ids of sessions whose claude process is running,
// read from every account's sessions/<pid>.json ({"pid", "sessionId"}).
// Files that are not plain JSON session records (e.g. *.key) are ignored.
func LiveSessions(p paths.Paths, accts []registry.Account) map[string]bool {
	live := map[string]bool{}
	for _, a := range accts {
		dir := filepath.Join(ConfigDir(p, a), sessionsDir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), liveExt) {
				continue
			}
			if id, ok := liveID(filepath.Join(dir, e.Name())); ok {
				live[id] = true
			}
		}
	}
	return live
}

func liveID(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var rec struct {
		PID       int    `json:"pid"`
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(b, &rec) != nil || rec.PID <= 0 || rec.SessionID == "" || !pidAlive(rec.PID) {
		return "", false
	}
	return rec.SessionID, true
}
