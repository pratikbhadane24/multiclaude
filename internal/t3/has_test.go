package t3

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
)

func t3Paths(t *testing.T) paths.Paths {
	t.Helper()
	return paths.Paths{T3Settings: filepath.Join(t.TempDir(), "settings.json")}
}

func TestHasInstanceMissingFileIsFalse(t *testing.T) {
	has, err := HasInstance(t3Paths(t), accts[1], shim)
	if err != nil || has {
		t.Fatalf("has=%v err=%v", has, err)
	}
}

func TestHasInstanceTrueOnlyForSyncedAccounts(t *testing.T) {
	p := t3Paths(t)
	out, _, _, err := Merge([]byte(realFile), accts[:1], shim)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.T3Settings, out, 0o600); err != nil {
		t.Fatal(err)
	}

	hasMain, err1 := HasInstance(p, accts[0], shim)
	hasWork, err2 := HasInstance(p, accts[1], shim)

	if err1 != nil || err2 != nil || !hasMain || hasWork {
		t.Fatalf("main=%v work=%v errs=%v %v", hasMain, hasWork, err1, err2)
	}
}

func TestHasInstanceStaleBinaryPathStillCounts(t *testing.T) {
	p := t3Paths(t)
	out, _, _, err := Merge(nil, []registry.Account{accts[1]}, "/old/shim")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.T3Settings, out, 0o600); err != nil {
		t.Fatal(err)
	}
	if has, err := HasInstance(p, accts[1], shim); err != nil || !has {
		t.Fatalf("has=%v err=%v", has, err)
	}
}

func TestHasInstanceCorruptSettingsIsError(t *testing.T) {
	p := t3Paths(t)
	if err := os.WriteFile(p.T3Settings, []byte("[1]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := HasInstance(p, accts[1], shim); err == nil {
		t.Fatal("want error")
	}
}
