package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

func guardPaths(root string) paths.Paths {
	return paths.Paths{
		Home: filepath.Join(root, "home"), ClaudeDir: filepath.Join(root, "home", ".claude"),
		AccountsRoot: filepath.Join(root, "home", "accts"), ShimPath: filepath.Join(root, "home", "accts", "bin", "claude"),
	}
}

func TestPurgeGuardStrict(t *testing.T) {
	root := t.TempDir()
	p := guardPaths(root)
	mustMkdir(t, filepath.Join(p.AccountsRoot, "work", "nested"))
	mustMkdir(t, filepath.Join(root, "victim"))
	mustMkdir(t, filepath.Dir(p.ShimPath))
	mustMkdir(t, p.ClaudeDir)
	if err := os.Symlink(filepath.Join(root, "victim"), filepath.Join(p.AccountsRoot, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(p.AccountsRoot, filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	homeRoot := p
	homeRoot.AccountsRoot = p.Home // CCM_HOME=$HOME
	ancestor := paths.Paths{Home: p.Home, ClaudeDir: p.ClaudeDir, AccountsRoot: root, ShimPath: filepath.Join(root, "bin", "claude")}

	cases := []struct {
		name string
		p    paths.Paths
		dir  string
		want bool
	}{
		{"direct child", p, filepath.Join(p.AccountsRoot, "work"), true},
		{"nested refused", p, filepath.Join(p.AccountsRoot, "work", "nested"), false},
		{"dir is symlink refused", p, filepath.Join(p.AccountsRoot, "linked"), false},
		{"parent is another dir refused", p, filepath.Join(root, "victim", "work"), false},
		{"alias of root allowed (identity)", p, filepath.Join(root, "alias", "work"), true},
		{"claude dir refused", p, p.ClaudeDir, false},
		{"root==home, ~/.claude refused", homeRoot, p.ClaudeDir, false},
		{"home itself refused", p, p.Home, false},
		{"ancestor of home refused", ancestor, filepath.Join(root, "home"), false},
		{"shim dir refused", p, filepath.Dir(p.ShimPath), false},
	}
	for _, c := range cases {
		if got := purgeAllowed(c.p, c.dir) == nil; got != c.want {
			t.Errorf("%s: allowed=%v want %v", c.name, got, c.want)
		}
	}
}

func TestPromptLineCancelsWithContext(t *testing.T) {
	e := newCLI(t, "")
	pr, pw := io.Pipe()
	defer pw.Close()
	e.app.In = pr
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := e.app.promptLine(ctx, "? "); done <- err }()

	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("want error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("promptLine did not return on cancel")
	}
}

func TestRemoveCancelledByInterruptExits130(t *testing.T) {
	e := newCLI(t, "")
	pr, pw := io.Pipe()
	defer pw.Close()
	e.app.In = pr
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()

	code := e.app.Main(ctx, []string{"remove", "work"})

	if code != ExitInterrupted || !strings.Contains(e.out.String(), "Cancelled.") {
		t.Fatalf("code=%d out=%s", code, e.out)
	}
	if _, ok := registry.Find(loadAccts(t, e), "work"); !ok {
		t.Fatal("account removed")
	}
}

func TestHandlesInterrupt(t *testing.T) {
	cases := map[string]bool{
		"run": true, "best": true, "login": true, "add": true, "status": true, "remove": true, "work": true,
		"sync": false, "t3-sync": false, "install": false, "list": false, "doctor": false, "use": false, "shell-init": false,
	}
	for cmd, want := range cases {
		if got := HandlesInterrupt([]string{cmd}); got != want {
			t.Errorf("%s: got %v want %v", cmd, got, want)
		}
	}
	if HandlesInterrupt(nil) {
		t.Error("no args: default behaviour")
	}
}

func TestNoBestClampsNegativeReset(t *testing.T) {
	e := newCLI(t, "")
	past := testNow.Add(-time.Hour)

	code := e.app.noBest(usage.Combined{NextResetAt: &past})

	if code != exitAllLimited || !strings.Contains(e.err.String(), "next reset in") || strings.Contains(e.err.String(), "-") {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}

func TestRunHelpAndMissingNameAreUsage(t *testing.T) {
	for _, args := range [][]string{{"run"}, {"run", "-h"}, {"run", "--help"}} {
		e := newCLI(t, "")
		if code := e.app.Main(context.Background(), args); code != ExitUsage || !strings.Contains(e.err.String(), "usage: ccm run") {
			t.Errorf("%v: code=%d err=%s", args, code, e.err)
		}
	}
}

func TestUnknownCommandWithCorruptRegistryIsUsage(t *testing.T) {
	e := newCLI(t, "")
	mustWrite(t, filepath.Join(e.p.AccountsRoot, "accounts.json"), "{corrupt")

	code := e.main("bogus")

	if code != ExitUsage || !strings.Contains(e.err.String(), "unknown command") {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}

func TestPurgeRemoveAllFailureStillDeletesKeychainAndListsLeftovers(t *testing.T) {
	e := newCLI(t, "y\n")
	locked := filepath.Join(e.workDir, "locked")
	mustMkdir(t, locked)
	mustWrite(t, filepath.Join(locked, "f"), "x")
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	code := e.main("remove", "work", "--purge")

	if code != ExitError {
		t.Fatalf("code=%d", code)
	}
	if len(e.callsTo(securityPath)) != 1 {
		t.Fatalf("keychain delete not attempted: %v", e.fake.Calls)
	}
	if !strings.Contains(e.err.String(), e.workDir) || !strings.Contains(e.err.String(), "rm -rf") {
		t.Fatalf("no cleanup hint: %s", e.err)
	}
}

func isCaseInsensitive(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "CaseProbe")
	mustWrite(t, probe, "x")
	_, err := os.Stat(filepath.Join(dir, "caseprobe"))
	return err == nil
}

func TestPurgeGuardCaseVariantsAreIdentityChecked(t *testing.T) {
	root := t.TempDir()
	if !isCaseInsensitive(t, root) {
		t.Skip("case-sensitive filesystem")
	}
	p := guardPaths(root)
	p.ClaudeDir = filepath.Join(p.AccountsRoot, ".claude")
	mustMkdir(t, p.ClaudeDir)
	mustMkdir(t, filepath.Dir(p.ShimPath))
	homeRoot := guardPaths(root)
	homeRoot.AccountsRoot = homeRoot.Home // CCM_HOME=$HOME
	mustMkdir(t, homeRoot.ClaudeDir)

	if err := purgeAllowed(p, filepath.Join(p.AccountsRoot, ".CLAUDE")); err == nil {
		t.Error(".CLAUDE variant of ClaudeDir allowed")
	}
	if err := purgeAllowed(p, filepath.Join(p.AccountsRoot, "BIN")); err == nil {
		t.Error("BIN variant of shim dir allowed")
	}
	if err := purgeAllowed(homeRoot, filepath.Join(homeRoot.Home, ".CLAUDE")); err == nil {
		t.Error("root==Home with ~/.CLAUDE allowed")
	}
}

func TestPurgeRefusedWhenRootOverlapsHomeOrClaudeDir(t *testing.T) {
	root := t.TempDir()
	p := guardPaths(root)
	mustMkdir(t, p.ClaudeDir)
	for name, accounts := range map[string]string{"home": p.Home, "claude dir": p.ClaudeDir, "inside claude dir": filepath.Join(p.ClaudeDir, "accts")} {
		mustMkdir(t, accounts)
		q := p
		q.AccountsRoot = accounts
		err := purgeAllowed(q, filepath.Join(accounts, "work"))
		if err == nil || !strings.Contains(err.Error(), "accounts root overlaps your home or ~/.claude") {
			t.Errorf("%s: err=%v", name, err)
		}
	}
}

func TestAddEmailPromptInterruptedExits130(t *testing.T) {
	e := newCLI(t, "")
	pr, pw := io.Pipe()
	defer pw.Close()
	e.app.In = pr
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()

	code := e.app.Main(ctx, []string{"add", "fresh"})

	if code != ExitInterrupted || !strings.Contains(e.out.String(), "Cancelled.") {
		t.Fatalf("code=%d out=%s err=%s", code, e.out, e.err)
	}
}
