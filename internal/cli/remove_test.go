package cli

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"multiclaude/internal/keychain"
	"multiclaude/internal/registry"
)

func loadAccts(t *testing.T, e *cliEnv) []registry.Account {
	t.Helper()
	accts, err := registry.Load(e.p)
	if err != nil {
		t.Fatal(err)
	}
	return accts
}

func TestRemoveDeclinedLeavesRegistry(t *testing.T) {
	e := newCLI(t, "n\n")
	before := loadAccts(t, e)

	code := e.main("remove", "work")

	if code != ExitError || !strings.Contains(e.out.String(), `Remove account "work"? [y/N] `) || !strings.Contains(e.out.String(), "Cancelled.") {
		t.Fatalf("code=%d out=%s", code, e.out)
	}
	if !reflect.DeepEqual(loadAccts(t, e), before) {
		t.Fatal("registry changed")
	}
}

func TestRemoveConfirmedKeepsDirWithoutPurge(t *testing.T) {
	e := newCLI(t, "y\n")

	code := e.main("remove", "work")

	if code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	if _, ok := registry.Find(loadAccts(t, e), "work"); ok {
		t.Fatal("work still registered")
	}
	if _, err := os.Stat(e.workDir); err != nil {
		t.Fatalf("dir removed without --purge: %v", err)
	}
	if len(e.callsTo(securityPath)) != 0 {
		t.Fatal("security must not run without --purge")
	}
}

func TestRemovePurgeDeletesDirAndKeychainItem(t *testing.T) {
	e := newCLI(t, "y\n")

	code := e.main("remove", "work", "--purge")

	if code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	if _, err := os.Stat(e.workDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dir still there: %v", err)
	}
	want := []string{"delete-generic-password", "-s", keychain.Service(e.workDir)}
	calls := e.callsTo(securityPath)
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Fatalf("security calls=%v", calls)
	}
	if !strings.Contains(e.out.String(), "ccm t3-sync") {
		t.Fatalf("missing t3 hint: %s", e.out)
	}
}

func TestRemovePurgeRefusesDirOutsideAccountsRoot(t *testing.T) {
	e := newCLI(t, "y\n")
	outside := filepath.Join(e.p.Home, "elsewhere")
	mustMkdir(t, outside)
	accts := registry.With(loadAccts(t, e), registry.Account{Name: "odd", Dir: outside})
	if err := registry.Save(e.p, accts); err != nil {
		t.Fatal(err)
	}

	code := e.main("remove", "--purge", "odd")

	if code != ExitError || !strings.Contains(e.err.String(), "outside") {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside dir touched: %v", err)
	}
	if _, ok := registry.Find(loadAccts(t, e), "odd"); !ok {
		t.Fatal("registry changed")
	}
	if len(e.callsTo(securityPath)) != 0 {
		t.Fatal("security must not run")
	}
}

func TestRemoveMainRefused(t *testing.T) {
	e := newCLI(t, "y\n")
	if code := e.main("remove", "main"); code != ExitError {
		t.Fatalf("code=%d", code)
	}
}

func TestRemoveUnknownAccount(t *testing.T) {
	e := newCLI(t, "y\n")
	if code := e.main("remove", "nope"); code != ExitError || !strings.Contains(e.err.String(), "unknown account") {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}
