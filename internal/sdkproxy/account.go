package sdkproxy

import (
	"strings"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
)

const configDirEnv = "CLAUDE_CONFIG_DIR"

// credentialEnv override the account's stored login; a child running with
// any of them would keep the same credentials after a switch.
var credentialEnv = []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"}

// currentAccount maps the env's CLAUDE_CONFIG_DIR to a registered account:
// unset or ~/.claude is main. ok is false for a directory ccm does not know.
func currentAccount(env []string, p paths.Paths, accts []registry.Account) (registry.Account, bool) {
	raw := lookupEnv(env, configDirEnv)
	dir := ""
	if raw != "" {
		dir = paths.Normalize(raw, p.Home)
	}
	if dir == paths.Normalize(p.ClaudeDir, p.Home) {
		dir = ""
	}
	for _, a := range accts {
		if a.IsMain() && dir == "" {
			return a, true
		}
		if !a.IsMain() && dir != "" && registry.NormalizeDir(a.Dir, p.Home) == dir {
			return a, true
		}
	}
	return registry.Account{Dir: dir}, false
}

// envFor returns a copy of env for running claude as a: CLAUDE_CONFIG_DIR
// unset for main, set to the account's directory otherwise.
func envFor(env []string, a registry.Account) []string {
	out := withoutEnv(env, configDirEnv)
	if a.IsMain() {
		return out
	}
	return append(out, configDirEnv+"="+a.Dir)
}

// withoutCredentials returns a copy of env without credentialEnv and the
// names of those it removed. env is not modified.
func withoutCredentials(env []string) ([]string, []string) {
	out, removed := append([]string(nil), env...), []string(nil)
	for _, k := range credentialEnv {
		if hasEnvKey(out, k) {
			removed = append(removed, k)
			out = withoutEnv(out, k)
		}
	}
	return out, removed
}

func hasEnvKey(env []string, key string) bool {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return true
		}
	}
	return false
}
