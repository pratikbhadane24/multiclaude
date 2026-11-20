package autoswitch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
)

// Owner reports the conversation the supervised claude is in right now; ok
// is false when that is not known (yet).
type Owner func() (id string, ok bool)

// SessionOfPid reads <configDir>/sessions/<pid>.json, where claude records
// which conversation that process is in. Project transcript directories are
// shared by every account and terminal, so this (not "the newest file") is
// the only safe way to know which transcript belongs to this claude.
func SessionOfPid(configDir string, pid int) (string, bool) {
	b, err := os.ReadFile(filepath.Join(configDir, "sessions", strconv.Itoa(pid)+".json"))
	if err != nil {
		return "", false
	}
	var f struct {
		PID       int    `json:"pid"`
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(b, &f) != nil || f.PID != pid || !sessionIDRE.MatchString(f.SessionID) {
		return "", false
	}
	return f.SessionID, true
}

// pidOf is p's process id, 0 when p does not expose one (test fakes).
func pidOf(p Proc) int {
	if withPid, ok := p.(interface{ Pid() int }); ok {
		return withPid.Pid()
	}
	return 0
}
