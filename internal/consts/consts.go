// Package consts holds shared constants for ccm.
package consts

import "regexp"

const (
	UsageURL     = "https://api.anthropic.com/api/oauth/usage"
	UsageBeta    = "oauth-2025-04-20"
	KeychainBase = "Claude Code-credentials"
	MainName     = "main"
)

// AccountNameRE validates account names.
var AccountNameRE = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// ReservedNames are command names that cannot be used as account names.
var ReservedNames = map[string]struct{}{
	"main": {}, "add": {}, "login": {}, "list": {}, "run": {}, "best": {}, "use": {},
	"status": {}, "sync": {}, "t3-sync": {}, "doctor": {}, "remove": {},
	"shell-init": {}, "help": {}, "install": {}, "bin": {}, "browser": {}, "config": {}, "continue": {},
	"dash": {}, "statusline": {}, "version": {},
}

// SharedItems are linked from ~/.claude into each account dir when present.
var SharedItems = []string{
	"CLAUDE.md", "settings.json", "rules", "skills", "plugins", "scripts",
	"agents", "commands", "hooks", "output-styles", "keybindings.json",
	"projects", "file-history", "todos", "plans",
}

// HistoryItems are the SharedItems holding conversation history. They are
// merged into ~/.claude (see package history) before being linked.
var HistoryItems = []string{"projects", "file-history", "todos", "plans"}
