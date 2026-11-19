package shellenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const zshPath = "/bin/zsh"

// runZsh writes the ShellInit output for root to a file and runs script in a
// clean zsh that has fakeBin first on PATH after sourcing... (see callers).
func runZsh(t *testing.T, root, script string, fakeBin string) (string, int) {
	t.Helper()
	if _, err := os.Stat(zshPath); err != nil {
		t.Skip("no /bin/zsh")
	}
	dir := t.TempDir()
	init, err := ShellInit("zsh", root)
	if err != nil {
		t.Fatal(err)
	}
	initFile := filepath.Join(dir, "init.zsh")
	if err := os.WriteFile(initFile, []byte(init), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(zshPath, "-f", "-c", script)
	cmd.Env = []string{"PATH=" + fakeBin + ":/usr/bin:/bin", "INIT=" + initFile}
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

func fakeCcm(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	body := `#!/bin/sh
if [ "$2" = "ok" ]; then
  echo "export CLAUDE_CONFIG_DIR='/x/y'; export CCM_ACCOUNT='ok'"
  exit 0
fi
echo "no such account" >&2
exit 7
`
	if err := os.WriteFile(filepath.Join(dir, "ccm"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCcmUseSuccessExports(t *testing.T) {
	out, code := runZsh(t, "/r/accounts", `source $INIT; ccm use ok; echo "rc=$? dir=$CLAUDE_CONFIG_DIR acct=$CCM_ACCOUNT"`, fakeCcm(t))
	want := "rc=0 dir=/x/y acct=ok"
	if code != 0 || !strings.Contains(out, want) {
		t.Fatalf("code=%d out=%q want contains %q", code, out, want)
	}
}

func TestCcmUseFailurePropagatesStatus(t *testing.T) {
	out, _ := runZsh(t, "/r/accounts", `source $INIT; ccm use bogus; echo "rc=$? dir=[$CLAUDE_CONFIG_DIR] acct=[$CCM_ACCOUNT]"`, fakeCcm(t))
	want := "rc=7 dir=[] acct=[]"
	if !strings.Contains(out, want) {
		t.Fatalf("out=%q want contains %q", out, want)
	}
}

func TestShellInitIdempotentPath(t *testing.T) {
	roots := []string{"/r/accounts", "/r/a`b", `/r/a$b"c`, `/r/a\b`}
	for _, root := range roots {
		out, _ := runZsh(t, root, `source $INIT; source $INIT; print -r -- "$PATH"`, "/nonexistent")
		path := strings.TrimSpace(out)
		count := 0
		for _, p := range strings.Split(path, ":") {
			if p == root+"/bin" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("root %q: bin appears %d times in PATH %q", root, count, path)
		}
	}
}

func TestShellInitSpecialRootsQuoted(t *testing.T) {
	for _, root := range []string{"/r/a`b", `/r/a\b`, "/r/a\nb"} {
		out, err := ShellInit("zsh", root)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, `export PATH='`+root+`/bin':"$PATH"`) {
			t.Errorf("root %q not single-quoted:\n%s", root, out)
		}
	}
}
