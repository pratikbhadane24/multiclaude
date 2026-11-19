package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"multiclaude/internal/paths"
)

const (
	serversKey    = "mcpServers"
	syncedKey     = "mcpSynced"
	stateFileName = "state.json"
	claudeJSON    = ".claude.json"
)

var (
	// ErrSource means the main ~/.claude.json is missing or unparseable.
	ErrSource = errors.New("main .claude.json unreadable")
	// ErrMainTarget means the account dir is the main config (or inside it).
	ErrMainTarget = errors.New("refusing to sync into the main Claude config")
	// ErrAccountChanged means the account file changed while syncing; the
	// write was skipped so the other writer's change is kept.
	ErrAccountChanged = errors.New("account .claude.json changed during sync; skipped")
)

// accountFile is a parsed account .claude.json plus the stamp it was read at.
type accountFile struct {
	members []member
	servers map[string]json.RawMessage
	stamp   fileStamp
}

// Sync merges main's MCP servers into accountDir/.claude.json. It writes the
// account file only when its servers change and never touches other keys or
// the main file. Writers are serialized with a lock in AccountsRoot.
func Sync(p paths.Paths, accountDir string) (bool, error) {
	dir := filepath.Clean(accountDir)
	if err := checkTarget(p, dir); err != nil {
		return false, err
	}
	mainServers, err := readMainServers(p.ClaudeJSON)
	if err != nil {
		return false, err
	}
	unlock, err := acquireLock(filepath.Join(p.AccountsRoot, lockFileName))
	if err != nil {
		return false, err
	}
	defer unlock()
	return syncLocked(p, dir, mainServers)
}

// syncLocked must run under the sync lock. state.json is first widened to
// prev ∪ new before the account write, so a failed write never leaves a
// main-derived server looking account-only; it is narrowed after success.
func syncLocked(p paths.Paths, dir string, mainServers map[string]json.RawMessage) (bool, error) {
	statePath := filepath.Join(p.AccountsRoot, stateFileName)
	stateMembers, synced, err := readState(statePath)
	if err != nil {
		return false, err
	}
	accFile := filepath.Join(dir, claudeJSON)
	acc, err := readAccount(accFile)
	if err != nil {
		return false, err
	}
	prev := synced[dir]
	servers, now, changed := Merge(mainServers, acc.servers, prev)
	if !changed {
		return false, updateState(statePath, stateMembers, synced, dir, prev, now)
	}
	wide := union(prev, now)
	if err := updateState(statePath, stateMembers, synced, dir, prev, wide); err != nil {
		return false, err
	}
	if err := writeAccount(accFile, acc, servers); err != nil {
		return false, err
	}
	return true, updateState(statePath, stateMembers, synced, dir, wide, now)
}

// checkTarget refuses an account dir that is ClaudeDir, inside it, or whose
// .claude.json would be the main ~/.claude.json (also after symlinks).
func checkTarget(p paths.Paths, dir string) error {
	if isMainTarget(p.ClaudeDir, p.ClaudeJSON, dir) {
		return fmt.Errorf("%w: %s", ErrMainTarget, dir)
	}
	rdir, err1 := filepath.EvalSymlinks(dir)
	rclaude, err2 := filepath.EvalSymlinks(p.ClaudeDir)
	if err1 == nil && err2 == nil && isMainTarget(rclaude, "", rdir) {
		return fmt.Errorf("%w: %s", ErrMainTarget, dir)
	}
	if rjson, err := filepath.EvalSymlinks(p.ClaudeJSON); err == nil && err1 == nil {
		if filepath.Join(rdir, claudeJSON) == rjson {
			return fmt.Errorf("%w: %s", ErrMainTarget, dir)
		}
	}
	return nil
}

func isMainTarget(claudeDir, mainJSON, dir string) bool {
	claudeDir = filepath.Clean(claudeDir)
	if dir == claudeDir || strings.HasPrefix(dir, claudeDir+string(filepath.Separator)) {
		return true
	}
	return mainJSON != "" && filepath.Join(dir, claudeJSON) == filepath.Clean(mainJSON)
}

func union(a, b []string) []string {
	out := slices.Concat(a, b)
	slices.Sort(out)
	return slices.Compact(out)
}

func readMainServers(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSource, err)
	}
	members, err := parseObject(data)
	if err != nil {
		return nil, fmt.Errorf("%w: parse %s: %v", ErrSource, path, err)
	}
	servers, err := decodeServers(members)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrSource, path, err)
	}
	return servers, nil
}

// decodeServers returns the mcpServers object; absent or null means empty.
func decodeServers(members []member) (map[string]json.RawMessage, error) {
	raw, ok := lookup(members, serversKey)
	servers := map[string]json.RawMessage{}
	if !ok || string(raw) == "null" {
		return servers, nil
	}
	if err := json.Unmarshal(raw, &servers); err != nil {
		return nil, fmt.Errorf("%s is not an object: %w", serversKey, err)
	}
	return servers, nil
}

// readAccount stats then reads the account file (stat first, so any later
// change shows up in the pre-rename check). Missing file = empty object.
func readAccount(path string) (accountFile, error) {
	stamp, err := stampOf(path)
	if err != nil {
		return accountFile{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return accountFile{servers: map[string]json.RawMessage{}, stamp: stamp}, nil
	}
	if err != nil {
		return accountFile{}, fmt.Errorf("read %s: %w", path, err)
	}
	members, err := parseObject(data)
	if err != nil {
		return accountFile{}, fmt.Errorf("parse %s (left untouched): %w", path, err)
	}
	servers, err := decodeServers(members)
	if err != nil {
		return accountFile{}, fmt.Errorf("%s (left untouched): %w", path, err)
	}
	return accountFile{members: members, servers: servers, stamp: stamp}, nil
}

func writeAccount(path string, acc accountFile, servers map[string]json.RawMessage) error {
	val, err := encodeIndented(servers)
	if err != nil {
		return fmt.Errorf("encode %s for %s: %w", serversKey, path, err)
	}
	data, err := renderObject(withMember(acc.members, serversKey, val))
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return writeAtomic(path, data, func() error {
		cur, err := stampOf(path)
		if err != nil {
			return err
		}
		if !cur.equal(acc.stamp) {
			return fmt.Errorf("%w: %s", ErrAccountChanged, path)
		}
		return nil
	})
}
