package mcp

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestSyncUsesHubFileWhenPresent(t *testing.T) {
	p, acc := setupSync(t)
	writeFile(t, p.ClaudeJSON, `{"mcpServers":{"a":{"command":"x"}}}`)
	writeFile(t, filepath.Join(p.AccountsRoot, "mcp-hub.json"), `{"mcpServers":{"b":{"command":"y"}}}`)

	if _, err := Sync(p, acc); err != nil {
		t.Fatal(err)
	}

	if got := serverNames(t, filepath.Join(acc, ".claude.json")); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("servers = %v, want [b]", got)
	}
}

func TestSourcePathDefaultsToClaudeJSON(t *testing.T) {
	p, _ := setupSync(t)

	if got := SourcePath(p); got != p.ClaudeJSON {
		t.Fatalf("SourcePath = %q, want %q", got, p.ClaudeJSON)
	}
	writeFile(t, filepath.Join(p.AccountsRoot, "mcp-hub.json"), `{}`)
	if got := SourcePath(p); got != filepath.Join(p.AccountsRoot, "mcp-hub.json") {
		t.Fatalf("SourcePath = %q with hub file", got)
	}
}
