package sdkproxy

import "strings"

const (
	streamJSON      = "stream-json"
	flagOutput      = "--output-format"
	flagInput       = "--input-format"
	flagResume      = "--resume"
	flagReplayUsers = "--replay-user-messages"
	noProxyEnv      = "CCM_NO_PROXY"
)

// sessionFlags select which conversation claude starts with. A replacement
// child drops them all and resumes the proxied session instead. The bool
// says whether the flag takes a value.
var sessionFlags = map[string]bool{
	"--resume": true, "-r": true, "--session-id": true, "--resume-session-at": true,
	"--continue": false, "-c": false, "--fork-session": false,
}

// flagValue returns the value of flag in args (as "--x v" or "--x=v"; the
// last occurrence wins) and whether it was present.
func flagValue(args []string, flag string) (string, bool) {
	val, found := "", false
	for i, a := range args {
		switch {
		case a == flag && i+1 < len(args):
			val, found = args[i+1], true
		case strings.HasPrefix(a, flag+"="):
			val, found = a[len(flag)+1:], true
		}
	}
	return val, found
}

// hasFlag reports whether a bare flag appears in args.
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}

// IsStreamJSON reports whether args run claude as an SDK child: stream-json
// on both stdin and stdout.
func IsStreamJSON(args []string) bool {
	out, _ := flagValue(args, flagOutput)
	in, _ := flagValue(args, flagInput)
	return out == streamJSON && in == streamJSON
}

// Enabled reports whether the shim should proxy this invocation: an SDK
// stream-json session, auto-switch on, and CCM_NO_PROXY unset in env.
func Enabled(args, env []string, autoSwitch bool) bool {
	return autoSwitch && lookupEnv(env, noProxyEnv) == "" && IsStreamJSON(args)
}

// ResumeArgs returns a copy of args that resumes sessionID: every existing
// --resume/-r/--session-id/--resume-session-at/--continue/-c/--fork-session
// is dropped and "--resume <sessionID>" appended. args is not modified.
func ResumeArgs(args []string, sessionID string) []string {
	out := make([]string, 0, len(args)+2)
	for i := 0; i < len(args); i++ {
		name, _, hasEq := strings.Cut(args[i], "=")
		takesValue, isSession := sessionFlags[name]
		if !isSession {
			out = append(out, args[i])
			continue
		}
		if takesValue && !hasEq && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++ // skip the flag's value
		}
	}
	return append(out, flagResume, sessionID)
}

// lookupEnv returns the last value of key in env (as getenv would).
func lookupEnv(env []string, key string) string {
	prefix := key + "="
	val := ""
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			val = kv[len(prefix):]
		}
	}
	return val
}

// withoutEnv returns a copy of env without key.
func withoutEnv(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}
	return out
}
