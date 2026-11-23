package worker

import "strings"

// Markers around the shell block ccm manages in a worker's zsh files.
const (
	BlockStart = "# >>> ccm >>>"
	BlockEnd   = "# <<< ccm <<<"
)

// UpsertBlock returns content with the ccm block set to body: an existing
// block is replaced in place, otherwise one is appended.
func UpsertBlock(content, body string) string {
	block := BlockStart + "\n" + body + "\n" + BlockEnd + "\n"
	start := strings.Index(content, BlockStart)
	if start >= 0 {
		if end := strings.Index(content[start:], BlockEnd); end >= 0 {
			tail := content[start+end+len(BlockEnd):]
			tail = strings.TrimPrefix(tail, "\n")
			return content[:start] + block + tail
		}
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if content != "" {
		content += "\n"
	}
	return content + block
}

// ZshenvBody puts Claude Code's and the ccm shim's bin dirs on PATH for
// non-interactive SSH commands.
func ZshenvBody() string {
	return `export PATH="$HOME/.local/bin:$HOME/.claude-accounts/bin:$PATH"`
}

// ZshrcBody loads ccm's worker shell integration in interactive shells
// (Orca panes), after macOS path_helper has reordered PATH.
func ZshrcBody() string {
	return `eval "$(ccm shell-init zsh --worker)"`
}
