package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/config"
)

func (e *cliEnv) t3Settings(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(e.p.T3Settings)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (e *cliEnv) stateJSON(t *testing.T) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(e.p.AccountsRoot, "state.json"))
	return string(b)
}

func TestT3SyncWritesLaunchArgsFromConfig(t *testing.T) {
	e := newCLI(t, "")
	e.setConfig(t, config.Config{
		DefaultArgs: []string{"--dangerously-skip-permissions"},
		Accounts:    map[string]config.AccountConfig{"work": {Args: []string{"--model", "claude opus"}}},
	})

	if code := e.main("t3-sync"); code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}

	s := e.t3Settings(t)
	if !strings.Contains(s, `"launchArgs": "--dangerously-skip-permissions"`) ||
		!strings.Contains(s, `"launchArgs": "--dangerously-skip-permissions --model 'claude opus'"`) {
		t.Fatalf("settings=%s", s)
	}
	if st := e.stateJSON(t); !strings.Contains(st, `"t3LaunchArgs"`) || !strings.Contains(st, "claude-work") {
		t.Fatalf("state=%s", st)
	}
}

func TestT3SyncWithoutConfigWritesNoLaunchArgs(t *testing.T) {
	e := newCLI(t, "")

	e.main("t3-sync")

	if strings.Contains(e.t3Settings(t), "launchArgs") {
		t.Fatalf("settings=%s", e.t3Settings(t))
	}
}

func TestT3SyncPreservesUserEditedLaunchArgsAndWarns(t *testing.T) {
	e := newCLI(t, "")
	e.setConfig(t, config.Config{DefaultArgs: []string{"--a"}})
	e.main("t3-sync")
	edited := strings.Replace(e.t3Settings(t), `"launchArgs": "--a"`, `"launchArgs": "--mine"`, -1)
	mustWrite(t, e.p.T3Settings, edited)
	e.setConfig(t, config.Config{DefaultArgs: []string{"--b"}})
	e.err.Reset()

	code := e.main("t3-sync")

	if code != ExitOK || strings.Contains(e.t3Settings(t), "--b") || !strings.Contains(e.t3Settings(t), `"launchArgs": "--mine"`) {
		t.Fatalf("code=%d settings=%s", code, e.t3Settings(t))
	}
	want := "t3: claude-work launchArgs edited in T3; leaving it (ccm wants: --b)"
	if !strings.Contains(e.err.String(), want) {
		t.Fatalf("err=%s", e.err)
	}
}

func TestT3SyncUpdatesValueItWroteAndIsIdempotent(t *testing.T) {
	e := newCLI(t, "")
	e.setConfig(t, config.Config{DefaultArgs: []string{"--a"}})
	e.main("t3-sync")
	e.setConfig(t, config.Config{DefaultArgs: []string{"--b"}})

	e.main("t3-sync")
	e.out.Reset()
	e.main("t3-sync")

	if !strings.Contains(e.t3Settings(t), `"launchArgs": "--b"`) || strings.Contains(e.t3Settings(t), `"--a"`) {
		t.Fatalf("settings=%s", e.t3Settings(t))
	}
	if !strings.Contains(e.out.String(), "already up to date") {
		t.Fatalf("out=%s", e.out)
	}
}

func TestT3SyncRemovesLaunchArgsOnceConfigCleared(t *testing.T) {
	e := newCLI(t, "")
	e.setConfig(t, config.Config{DefaultArgs: []string{"--a"}})
	e.main("t3-sync")

	e.setConfig(t, config.Config{})
	e.main("t3-sync")

	if strings.Contains(e.t3Settings(t), "launchArgs") {
		t.Fatalf("settings=%s", e.t3Settings(t))
	}
}
