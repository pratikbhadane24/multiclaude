package mcp

import (
	"os"
	"path/filepath"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
)

// HubFileName is the ccm-owned MCP source on workers, written by
// `ccm worker` from the hub's main servers ({"mcpServers": {...}}).
const HubFileName = "mcp-hub.json"

// SourcePath is where Sync and Drift read the shared MCP servers from:
// AccountsRoot/mcp-hub.json when it exists (workers), else main's
// ~/.claude.json.
func SourcePath(p paths.Paths) string {
	hub := filepath.Join(p.AccountsRoot, HubFileName)
	if _, err := os.Stat(hub); err == nil {
		return hub
	}
	return p.ClaudeJSON
}
