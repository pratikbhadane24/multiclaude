package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
)

func TestSyncHTMLCharsDoNotCauseRewriteEveryLaunch(t *testing.T) {
	p, acc := setupSync(t)
	writeFile(t, p.ClaudeJSON, `{"mcpServers": {"h": {"url": "https://x/?a=1&b=2", "args": ["<x>"]}}}`)
	accFile := filepath.Join(acc, ".claude.json")
	writeFile(t, accFile, accountJSON)
	if _, err := Sync(p, acc); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mustRead(t, accFile)), `\`+"u00") {
		t.Fatalf("HTML-escaped output:\n%s", mustRead(t, accFile))
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(accFile, old, old); err != nil {
		t.Fatal(err)
	}

	changed, err := Sync(p, acc)

	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	fi, _ := os.Stat(accFile)
	if !fi.ModTime().Equal(old) {
		t.Fatal("account file rewritten although nothing changed")
	}
}

func TestSyncRefusesMainAsTarget(t *testing.T) {
	p, _ := setupSync(t)
	writeFile(t, p.ClaudeJSON, mainJSON)
	if err := os.MkdirAll(filepath.Join(p.ClaudeDir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{p.Home, p.ClaudeDir, filepath.Join(p.ClaudeDir, "sub"), p.ClaudeDir + "/"} {
		before := string(mustRead(t, p.ClaudeJSON))

		_, err := Sync(p, dir)

		if !errors.Is(err, ErrMainTarget) {
			t.Fatalf("Sync(%s) err = %v, want ErrMainTarget", dir, err)
		}
		if string(mustRead(t, p.ClaudeJSON)) != before {
			t.Fatal("main file modified")
		}
		if _, err := os.Stat(filepath.Join(p.ClaudeDir, ".claude.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("wrote inside ClaudeDir")
		}
	}
}

func readSynced(t *testing.T, p paths.Paths) map[string][]string {
	t.Helper()
	var st struct {
		MCPSynced map[string][]string `json:"mcpSynced"`
	}
	if err := json.Unmarshal(mustRead(t, filepath.Join(p.AccountsRoot, "state.json")), &st); err != nil {
		t.Fatal(err)
	}
	return st.MCPSynced
}

func TestSyncFailedAccountWriteKeepsUnionInState(t *testing.T) {
	p, acc := setupSync(t)
	writeFile(t, p.ClaudeJSON, `{"mcpServers": {"a": 1, "b": 2}}`)
	accFile := filepath.Join(acc, ".claude.json")
	writeFile(t, accFile, `{"mcpServers": {"x": 3}}`)
	if _, err := Sync(p, acc); err != nil {
		t.Fatal(err)
	}
	writeFile(t, p.ClaudeJSON, `{"mcpServers": {"a": 1, "c": 4}}`)
	if err := os.Chmod(acc, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(acc, 0o700) })

	_, err := Sync(p, acc)

	if err == nil {
		t.Fatal("want write error")
	}
	if got := readSynced(t, p)[acc]; !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("state after failed write = %v, want union [a b c]", got)
	}
	if err := os.Chmod(acc, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(p, acc); err != nil {
		t.Fatal(err)
	}
	if got := serverNames(t, accFile); !reflect.DeepEqual(got, []string{"a", "c", "x"}) {
		t.Fatalf("servers = %v (b must be removed, x kept)", got)
	}
	if got := readSynced(t, p)[acc]; !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Fatalf("final state = %v", got)
	}
}

func TestSyncSkipsWhenAccountFileChangesDuringSync(t *testing.T) {
	p, acc := setupSync(t)
	writeFile(t, p.ClaudeJSON, mainJSON)
	accFile := filepath.Join(acc, ".claude.json")
	writeFile(t, accFile, accountJSON)
	const edited = `{"edited": "by claude, longer than before so size differs ............................................................................................................................"}`
	beforeRename = func() { writeFile(t, accFile, edited) }
	defer func() { beforeRename = nil }()

	_, err := Sync(p, acc)

	if !errors.Is(err, ErrAccountChanged) {
		t.Fatalf("err = %v, want ErrAccountChanged", err)
	}
	if string(mustRead(t, accFile)) != edited {
		t.Fatal("concurrent edit overwritten")
	}
	entries, _ := os.ReadDir(acc)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover temp %s", e.Name())
		}
	}
}

func TestSyncSkipsWhenAccountFileAppearsDuringSync(t *testing.T) {
	p, acc := setupSync(t)
	writeFile(t, p.ClaudeJSON, mainJSON)
	accFile := filepath.Join(acc, ".claude.json")
	beforeRename = func() { writeFile(t, accFile, `{}`) }
	defer func() { beforeRename = nil }()

	_, err := Sync(p, acc)

	if !errors.Is(err, ErrAccountChanged) {
		t.Fatalf("err = %v, want ErrAccountChanged", err)
	}
}

func TestSyncSkipsWhenLockIsHeld(t *testing.T) {
	p, acc := setupSync(t)
	writeFile(t, p.ClaudeJSON, mainJSON)
	accFile := filepath.Join(acc, ".claude.json")
	writeFile(t, accFile, accountJSON)
	unlock, err := acquireLock(filepath.Join(p.AccountsRoot, lockFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	saved := lockWait
	lockWait = 50 * time.Millisecond
	defer func() { lockWait = saved }()

	_, err = Sync(p, acc)

	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("err = %v, want ErrLockTimeout", err)
	}
	if string(mustRead(t, accFile)) != accountJSON {
		t.Fatal("account file modified")
	}
}

func TestSyncPreservesUnknownStateKeys(t *testing.T) {
	p, acc := setupSync(t)
	writeFile(t, p.ClaudeJSON, mainJSON)
	statePath := filepath.Join(p.AccountsRoot, "state.json")
	writeFile(t, statePath, `{"other": {"k": [1, 2]}, "mcpSynced": {"/elsewhere": ["z"]}}`)

	if _, err := Sync(p, acc); err != nil {
		t.Fatal(err)
	}
	top := readTop(t, statePath)
	if string(top["other"]) != `{"k": [1, 2]}` {
		t.Fatalf("other = %s", top["other"])
	}
	var synced map[string][]string
	if err := json.Unmarshal(top["mcpSynced"], &synced); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(synced, map[string][]string{"/elsewhere": {"z"}, acc: {"a", "b"}}) {
		t.Fatalf("synced = %v", synced)
	}
}

func TestParseObjectRejectsNonObjectsAndTrailingData(t *testing.T) {
	for _, in := range []string{`{} {}`, `[1]`, `{"a":1`, `"x"`} {
		if _, err := parseObject([]byte(in)); err == nil {
			t.Fatalf("parseObject(%q) succeeded", in)
		}
	}
}
