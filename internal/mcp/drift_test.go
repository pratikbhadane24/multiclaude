package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDriftTrueWhenMainHasNewServersAndWritesNothing(t *testing.T) {
	p, acc := setupSync(t)
	writeFile(t, p.ClaudeJSON, mainJSON)
	accFile := filepath.Join(acc, ".claude.json")
	writeFile(t, accFile, accountJSON)

	drift, err := Drift(p, acc)

	if err != nil || !drift {
		t.Fatalf("drift=%v err=%v", drift, err)
	}
	if string(mustRead(t, accFile)) != accountJSON {
		t.Fatal("account file was modified")
	}
	if _, err := os.Stat(filepath.Join(p.AccountsRoot, stateFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state.json written: %v", err)
	}
}

func TestDriftFalseAfterSync(t *testing.T) {
	p, acc := setupSync(t)
	writeFile(t, p.ClaudeJSON, mainJSON)
	writeFile(t, filepath.Join(acc, ".claude.json"), accountJSON)
	if _, err := Sync(p, acc); err != nil {
		t.Fatal(err)
	}

	drift, err := Drift(p, acc)

	if err != nil || drift {
		t.Fatalf("drift=%v err=%v", drift, err)
	}
}

func TestDriftUsesStateToDetectRemovedServer(t *testing.T) {
	p, acc := setupSync(t)
	writeFile(t, p.ClaudeJSON, mainJSON)
	writeFile(t, filepath.Join(acc, ".claude.json"), accountJSON)
	if _, err := Sync(p, acc); err != nil {
		t.Fatal(err)
	}
	writeFile(t, p.ClaudeJSON, `{"mcpServers":{"a":{"command":"a"}}}`)

	drift, err := Drift(p, acc)

	if err != nil || !drift {
		t.Fatalf("drift=%v err=%v", drift, err)
	}
}

func TestDriftErrors(t *testing.T) {
	p, acc := setupSync(t)
	if _, err := Drift(p, acc); !errors.Is(err, ErrSource) {
		t.Fatalf("missing main: err=%v", err)
	}
	writeFile(t, p.ClaudeJSON, mainJSON)
	writeFile(t, filepath.Join(acc, ".claude.json"), `{bad`)
	if _, err := Drift(p, acc); err == nil {
		t.Fatal("corrupt account: want error")
	}
	if _, err := Drift(p, p.ClaudeDir); !errors.Is(err, ErrMainTarget) {
		t.Fatalf("main target: err=%v", err)
	}
}
