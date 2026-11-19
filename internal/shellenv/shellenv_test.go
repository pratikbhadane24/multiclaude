package shellenv

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"multiclaude/internal/registry"
)

func TestUseScriptMain(t *testing.T) {
	got := UseScript(registry.Account{Name: "main"})
	want := "unset CLAUDE_CONFIG_DIR; export CCM_ACCOUNT='main'"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestUseScriptNormal(t *testing.T) {
	got := UseScript(registry.Account{Name: "work", Dir: "/home/u/.ccm/work"})
	want := "export CLAUDE_CONFIG_DIR='/home/u/.ccm/work'; export CCM_ACCOUNT='work'"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestUseScriptEscapesQuotes(t *testing.T) {
	got := UseScript(registry.Account{Name: "x", Dir: "/tmp/it's"})
	want := `export CLAUDE_CONFIG_DIR='/tmp/it'\''s'; export CCM_ACCOUNT='x'`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestShellInitUnsupported(t *testing.T) {
	_, err := ShellInit("bash", "/r")
	if err == nil || err.Error() != `unsupported shell "bash" (supported: zsh)` {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestShellInitContents(t *testing.T) {
	out, err := ShellInit("zsh", "/r/accounts")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`out="$(command ccm use "$2")" || return $?`,
		`eval "$out"`,
		`case ":$PATH:" in *":/r/accounts/bin:"*) ;; *) export PATH="/r/accounts/bin:$PATH";; esac`,
		`command ccm "$@"`,
		"ccm()",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}
}

func TestShellInitSpecialRoot(t *testing.T) {
	out, err := ShellInit("zsh", `/r/a$b"c`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `export PATH='/r/a$b"c/bin':"$PATH"`) {
		t.Fatalf("bad quoting:\n%s", out)
	}
}

func TestShellInitZshSyntax(t *testing.T) {
	if _, err := os.Stat("/bin/zsh"); err != nil {
		t.Skip("no /bin/zsh")
	}
	for _, root := range []string{"/r/accounts", `/r/a$b"c`, "/tmp/it's"} {
		out, err := ShellInit("zsh", root)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("/bin/zsh", "-n")
		cmd.Stdin = strings.NewReader(out)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("root %q: zsh -n: %v\n%s\n%s", root, err, b, out)
		}
	}
}
