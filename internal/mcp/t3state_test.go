package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
)

func t3StatePaths(t *testing.T) paths.Paths {
	t.Helper()
	return paths.Paths{AccountsRoot: t.TempDir()}
}

func TestUpdateT3LaunchArgsRoundTripAndPreservesOtherKeys(t *testing.T) {
	p := t3StatePaths(t)
	statePath := filepath.Join(p.AccountsRoot, stateFileName)
	if err := os.WriteFile(statePath, []byte(`{"mcpSynced":{"/a":["s"]},"other":{"x":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := UpdateT3LaunchArgs(p, func(cur map[string]string) (map[string]string, error) {
		if len(cur) != 0 {
			t.Fatalf("cur=%v", cur)
		}
		return map[string]string{"claude-work": "--a 'b c'"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	err = UpdateT3LaunchArgs(p, func(cur map[string]string) (map[string]string, error) {
		got = cur
		return cur, nil
	})

	if err != nil || !reflect.DeepEqual(got, map[string]string{"claude-work": "--a 'b c'"}) {
		t.Fatalf("got=%v err=%v", got, err)
	}
	raw, _ := os.ReadFile(statePath)
	if !strings.Contains(string(raw), `"mcpSynced"`) || !strings.Contains(string(raw), `"other"`) {
		t.Fatalf("keys lost: %s", raw)
	}
}

func TestMcpStateUpdatePreservesT3LaunchArgs(t *testing.T) {
	p := t3StatePaths(t)
	if err := UpdateT3LaunchArgs(p, func(map[string]string) (map[string]string, error) {
		return map[string]string{"claude-main": "--x"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(p.AccountsRoot, stateFileName)
	members, synced, err := readState(statePath)
	if err != nil {
		t.Fatal(err)
	}

	if err := updateState(statePath, members, synced, "/d", nil, []string{"s"}); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(statePath)
	if !strings.Contains(string(raw), `"t3LaunchArgs"`) || !strings.Contains(string(raw), "--x") {
		t.Fatalf("t3LaunchArgs lost: %s", raw)
	}
}

func TestUpdateT3LaunchArgsFnErrorLeavesStateUntouched(t *testing.T) {
	p := t3StatePaths(t)
	boom := errors.New("boom")

	err := UpdateT3LaunchArgs(p, func(map[string]string) (map[string]string, error) { return map[string]string{"a": "b"}, boom })

	if !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(p.AccountsRoot, stateFileName)); statErr == nil {
		t.Fatal("state written on error")
	}
}

func TestUpdateT3LaunchArgsRejectsCorruptState(t *testing.T) {
	p := t3StatePaths(t)
	if err := os.WriteFile(filepath.Join(p.AccountsRoot, stateFileName), []byte(`{"t3LaunchArgs":[1]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := UpdateT3LaunchArgs(p, func(c map[string]string) (map[string]string, error) { return c, nil })
	if err == nil {
		t.Fatal("want error")
	}
}
