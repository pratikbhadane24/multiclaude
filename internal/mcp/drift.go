package mcp

import (
	"path/filepath"

	"multiclaude/internal/paths"
)

// Drift reports whether Sync would change accountDir's MCP servers. It is
// read-only: it takes no lock and writes neither the account file nor state.
func Drift(p paths.Paths, accountDir string) (bool, error) {
	dir := filepath.Clean(accountDir)
	if err := checkTarget(p, dir); err != nil {
		return false, err
	}
	mainServers, err := readMainServers(p.ClaudeJSON)
	if err != nil {
		return false, err
	}
	_, synced, err := readState(filepath.Join(p.AccountsRoot, stateFileName))
	if err != nil {
		return false, err
	}
	acc, err := readAccount(filepath.Join(dir, claudeJSON))
	if err != nil {
		return false, err
	}
	_, _, changed := Merge(mainServers, acc.servers, synced[dir])
	return changed, nil
}
