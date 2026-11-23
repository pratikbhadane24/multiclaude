// Package shellenv builds shell snippets for ccm (pure functions, no I/O).
package shellenv

import (
	"fmt"
	"strings"

	"github.com/pratikbhadane24/multiclaude/internal/registry"
)

// quote wraps s in POSIX single quotes, escaping embedded single quotes.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// UseScript returns the shell snippet that activates account a; it is
// printed by `ccm use` and eval'd by the ccm shell function.
func UseScript(a registry.Account) string {
	if a.IsMain() {
		return "unset CLAUDE_CONFIG_DIR; export CCM_ACCOUNT='main'"
	}
	return fmt.Sprintf("export CLAUDE_CONFIG_DIR=%s; export CCM_ACCOUNT=%s", quote(a.Dir), quote(a.Name))
}

const ccmFunc = `ccm() {
  if [ "$1" = "use" ]; then
    local out
    out="$(command ccm use "$2")" || return $?
    eval "$out"
    return $?
  fi
  command ccm "$@"
}
`

// ShellInit returns the init snippet for the given shell (only zsh).
func ShellInit(shell, accountsRoot string) (string, error) {
	if shell != "zsh" {
		return "", fmt.Errorf("unsupported shell %q (supported: zsh)", shell)
	}
	bin := accountsRoot + "/bin"
	var guard string
	if strings.ContainsAny(accountsRoot, "\"$`\\\n") {
		q := quote(bin)
		guard = fmt.Sprintf(`case ":$PATH:" in *":"%s":"*) ;; *) export PATH=%s:"$PATH";; esac`, q, q)
	} else {
		guard = fmt.Sprintf(`case ":$PATH:" in *":%s:"*) ;; *) export PATH="%s:$PATH";; esac`, bin, bin)
	}
	return guard + "\n" + ccmFunc, nil
}

// ShellInitWorker returns ShellInit plus a claude function that runs the
// ccm shim. On workers Orca installs its own claude wrapper unless the user
// defines one, and macOS path_helper can push the shim dir behind other
// claude binaries, so the function makes the shim win in every pane.
func ShellInitWorker(shell, accountsRoot string) (string, error) {
	base, err := ShellInit(shell, accountsRoot)
	if err != nil {
		return "", err
	}
	fn := fmt.Sprintf("claude() { command %s \"$@\"; }\n", quote(accountsRoot+"/bin/claude"))
	return base + fn, nil
}
