package history

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"multiclaude/internal/consts"
)

const (
	sessionsDir = "sessions"
	// recentSessionWindow is how fresh an unparsable session file must be to
	// count as a live session.
	recentSessionWindow = 10 * time.Minute
)

// pidAlive reports whether a process exists; tests replace it.
var pidAlive = func(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// NeedsMerge reports whether the account still has a real (unlinked) history dir.
func NeedsMerge(accountDir string) bool {
	for _, item := range consts.HistoryItems {
		if fi, err := os.Lstat(filepath.Join(accountDir, item)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// HasLiveSession reports whether a Claude session may be running in the
// account. Each file in <accountDir>/sessions names a pid (by file name or a
// "pid" field); a file whose pid cannot be determined counts as live when it
// changed within the last 10 minutes.
func HasLiveSession(accountDir string, now time.Time) bool {
	entries, err := os.ReadDir(filepath.Join(accountDir, sessionsDir))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(accountDir, sessionsDir, e.Name())
		if pid, ok := sessionPID(e.Name(), path); ok {
			if pidAlive(pid) {
				return true
			}
			continue
		}
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) < recentSessionWindow {
			return true
		}
	}
	return false
}

func sessionPID(name, path string) (int, bool) {
	if pid, err := strconv.Atoi(strings.TrimSuffix(name, filepath.Ext(name))); err == nil && pid > 0 {
		return pid, true
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	var body struct {
		PID int `json:"pid"`
	}
	if json.Unmarshal(b, &body) == nil && body.PID > 0 {
		return body.PID, true
	}
	return 0, false
}
