package mcp

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"

	"multiclaude/internal/paths"
)

const t3LaunchArgsKey = "t3LaunchArgs"

// UpdateT3LaunchArgs runs fn under the sync lock with the launchArgs values
// ccm last wrote into T3 (instance id -> value), then stores fn's result in
// state.json under "t3LaunchArgs". Other state keys are preserved verbatim.
// Nothing is written when fn fails or the map is unchanged.
func UpdateT3LaunchArgs(p paths.Paths, fn func(cur map[string]string) (map[string]string, error)) error {
	unlock, err := acquireLock(filepath.Join(p.AccountsRoot, lockFileName))
	if err != nil {
		return err
	}
	defer unlock()
	statePath := filepath.Join(p.AccountsRoot, stateFileName)
	members, _, err := readState(statePath)
	if err != nil {
		return err
	}
	cur := map[string]string{}
	if raw, ok := lookup(members, t3LaunchArgsKey); ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &cur); err != nil {
			return fmt.Errorf("parse %s in %s: %w", t3LaunchArgsKey, statePath, err)
		}
	}
	next, err := fn(maps.Clone(cur))
	if err != nil {
		return err
	}
	if next == nil {
		next = map[string]string{}
	}
	if maps.Equal(cur, next) {
		return nil
	}
	val, err := encodeIndented(next)
	if err != nil {
		return fmt.Errorf("encode %s: %w", t3LaunchArgsKey, err)
	}
	data, err := renderObject(withMember(members, t3LaunchArgsKey, val))
	if err != nil {
		return fmt.Errorf("encode %s: %w", statePath, err)
	}
	return writeAtomic(statePath, data, nil)
}
