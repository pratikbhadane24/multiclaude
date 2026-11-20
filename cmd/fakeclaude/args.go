package main

import "strings"

// valueFlags are claude flags that consume the following argument.
var valueFlags = map[string]bool{
	"--session-id": true, "--resume": true, "-r": true, "--model": true,
	"--output-format": true, "--input-format": true, "--append-system-prompt": true,
	"--system-prompt": true, "--permission-mode": true, "--allowedTools": true,
	"--disallowedTools": true, "--mcp-config": true, "--settings": true,
	"--add-dir": true, "--max-turns": true, "--permission-prompt-tool": true,
}

// options is the parsed subset of claude's command line that fakeclaude honors.
type options struct {
	SessionID    string
	IsResume     bool
	OutputFormat string
	InputFormat  string
	Prompt       string // last positional argument
}

// isStream reports whether both stream-json flags are present.
func (o options) isStream() bool {
	return o.OutputFormat == "stream-json" && o.InputFormat == "stream-json"
}

// parseArgs parses argv (without the program name). Both "--flag v" and
// "--flag=v" spellings are accepted.
func parseArgs(args []string) options {
	var o options
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			o.Prompt = a
			continue
		}
		name, val, hasVal := strings.Cut(a, "=")
		if !hasVal && valueFlags[a] && i+1 < len(args) {
			i++
			val, hasVal = args[i], true
		}
		if !hasVal {
			continue
		}
		switch name {
		case "--session-id":
			o.SessionID, o.IsResume = val, false
		case "--resume", "-r":
			o.SessionID, o.IsResume = val, true
		case "--output-format":
			o.OutputFormat = val
		case "--input-format":
			o.InputFormat = val
		}
	}
	return o
}
