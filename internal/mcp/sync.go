package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"multiclaude/internal/paths"
)

const (
	serversKey    = "mcpServers"
	syncedKey     = "mcpSynced"
	stateFileName = "state.json"
	claudeJSON    = ".claude.json"
)

// ErrSource means the main ~/.claude.json is missing or unparseable.
var ErrSource = errors.New("main .claude.json unreadable")

// Sync merges main's MCP servers into accountDir/.claude.json. It writes the
// account file only when its servers change and never touches other keys or
// the main file. Writers are serialized with a lock in AccountsRoot.
func Sync(p paths.Paths, accountDir string) (bool, error) {
	mainServers, err := readMainServers(p.ClaudeJSON)
	if err != nil {
		return false, err
	}
	unlock, err := acquireLock(filepath.Join(p.AccountsRoot, lockFileName))
	if err != nil {
		return false, err
	}
	defer unlock()

	statePath := filepath.Join(p.AccountsRoot, stateFileName)
	stateMembers, synced, err := readState(statePath)
	if err != nil {
		return false, err
	}
	accFile := filepath.Join(accountDir, claudeJSON)
	accMembers, accServers, err := readAccount(accFile)
	if err != nil {
		return false, err
	}
	prev := synced[accountDir]
	servers, now, changed := Merge(mainServers, accServers, prev)
	if changed {
		if err := writeAccount(accFile, accMembers, servers); err != nil {
			return false, err
		}
	}
	if !slices.Equal(prev, now) {
		if err := writeState(statePath, stateMembers, synced, accountDir, now); err != nil {
			return changed, err
		}
	}
	return changed, nil
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

// readAccount reads the account file; a missing file is an empty object.
func readAccount(path string) ([]member, map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	members, err := parseObject(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s (left untouched): %w", path, err)
	}
	servers, err := decodeServers(members)
	if err != nil {
		return nil, nil, fmt.Errorf("%s (left untouched): %w", path, err)
	}
	return members, servers, nil
}

func writeAccount(path string, members []member, servers map[string]json.RawMessage) error {
	val, err := json.MarshalIndent(servers, indent, indent)
	if err != nil {
		return fmt.Errorf("encode %s for %s: %w", serversKey, path, err)
	}
	data, err := renderObject(withMember(members, serversKey, val))
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return writeAtomic(path, data)
}

// readState returns state.json's members and its mcpSynced map; missing = empty.
func readState(path string) ([]member, map[string][]string, error) {
	synced := map[string][]string{}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, synced, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	members, err := parseObject(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if raw, ok := lookup(members, syncedKey); ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &synced); err != nil {
			return nil, nil, fmt.Errorf("parse %s in %s: %w", syncedKey, path, err)
		}
	}
	return members, synced, nil
}

func writeState(path string, members []member, synced map[string][]string, dir string, names []string) error {
	next := make(map[string][]string, len(synced)+1)
	for k, v := range synced {
		next[k] = v
	}
	next[dir] = names
	val, err := json.MarshalIndent(next, indent, indent)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	data, err := renderObject(withMember(members, syncedKey, val))
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return writeAtomic(path, data)
}
