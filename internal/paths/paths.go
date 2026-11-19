// Package paths resolves filesystem locations used by ccm.
package paths

import "path/filepath"

// Paths are the resolved locations for a given home directory and environment.
type Paths struct {
	Home, ClaudeDir, ClaudeJSON, AccountsRoot, ShimPath, RealClaude, T3Settings string
}

// Resolve computes Paths; CCM_HOME and CCM_CLAUDE_BIN override defaults.
func Resolve(getenv func(string) string, home string) Paths {
	root := getenv("CCM_HOME")
	if root == "" {
		root = filepath.Join(home, ".claude-accounts")
	}
	real := getenv("CCM_CLAUDE_BIN")
	if real == "" {
		real = filepath.Join(home, ".local", "bin", "claude")
	}
	return Paths{
		Home:         home,
		ClaudeDir:    filepath.Join(home, ".claude"),
		ClaudeJSON:   filepath.Join(home, ".claude.json"),
		AccountsRoot: root,
		ShimPath:     filepath.Join(root, "bin", "claude"),
		RealClaude:   real,
		T3Settings:   filepath.Join(home, ".t3", "userdata", "settings.json"),
	}
}
