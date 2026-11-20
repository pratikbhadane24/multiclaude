package autoswitch

import "strings"

// CredentialEnv are variables that make claude use one fixed credential
// whatever CLAUDE_CONFIG_DIR says, which would defeat a switch.
var CredentialEnv = []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"}

// StripCredentialEnv returns env without CredentialEnv entries and the names
// it removed (each once, in CredentialEnv order). env is not modified.
func StripCredentialEnv(env []string) ([]string, []string) {
	out := make([]string, 0, len(env))
	found := map[string]bool{}
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if isCredentialKey(key) {
			found[key] = true
			continue
		}
		out = append(out, kv)
	}
	var removed []string
	for _, k := range CredentialEnv {
		if found[k] {
			removed = append(removed, k)
		}
	}
	return out, removed
}

func isCredentialKey(key string) bool {
	for _, k := range CredentialEnv {
		if key == k {
			return true
		}
	}
	return false
}
