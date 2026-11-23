package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/run"
)

func TestEnvMainDropsConfigDir(t *testing.T) {
	base := []string{"A=1", "CLAUDE_CONFIG_DIR=/x", "B=2"}
	got := Env(registry.Account{Name: "main"}, base)
	for _, e := range got {
		if strings.HasPrefix(e, "CLAUDE_CONFIG_DIR=") {
			t.Fatalf("still set: %v", got)
		}
	}
	if len(got) != 3 || got[2] != "CCM_ACCOUNT=main" || base[1] != "CLAUDE_CONFIG_DIR=/x" {
		t.Fatalf("got %v base %v", got, base)
	}
}

func TestEnvAccountSetsConfigDir(t *testing.T) {
	base := []string{"A=1", "CLAUDE_CONFIG_DIR=/old"}
	got := Env(registry.Account{Name: "work", Dir: "/d/work"}, base)
	n := 0
	for _, e := range got {
		if e == "CLAUDE_CONFIG_DIR=/d/work" {
			n++
		}
		if e == "CLAUDE_CONFIG_DIR=/old" {
			t.Fatal("old value kept")
		}
	}
	if n != 1 || base[1] != "CLAUDE_CONFIG_DIR=/old" {
		t.Fatalf("got %v base %v", got, base)
	}
}

func testPaths(t *testing.T) paths.Paths {
	root := t.TempDir()
	return paths.Paths{Home: root, AccountsRoot: root, ShimPath: filepath.Join(root, "bin", "claude"), RealClaude: "/real/claude"}
}

func TestBinaryFallsBackToRealClaude(t *testing.T) {
	p := testPaths(t)
	if Binary(p) != "/real/claude" {
		t.Fatal("want real")
	}
	if err := os.MkdirAll(filepath.Dir(p.ShimPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ShimPath, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if Binary(p) != p.ShimPath {
		t.Fatal("want shim")
	}
}

func statusFake(email string) *run.Fake {
	return &run.Fake{Respond: func(c run.Call) (run.Result, error) {
		if len(c.Args) > 1 && c.Args[0] == "auth" && c.Args[1] == "status" {
			return run.Result{Stdout: `{"loggedIn":true,"email":"` + email + `","subscriptionType":"max","planTier":"x","loginExpiresAt":"t"}`}, nil
		}
		return run.Result{}, nil
	}}
}

func TestGetStatusParses(t *testing.T) {
	st, err := GetStatus(context.Background(), statusFake("a@x.com"), testPaths(t), registry.Account{Name: "work", Dir: "/d"})
	if err != nil || !st.LoggedIn || st.Email != "a@x.com" || st.SubscriptionType != "max" {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestGetStatusBadJSON(t *testing.T) {
	f := &run.Fake{Respond: func(run.Call) (run.Result, error) { return run.Result{Stdout: "nope"}, nil }}
	if _, err := GetStatus(context.Background(), f, testPaths(t), registry.Account{Name: "main"}); err == nil {
		t.Fatal("want error")
	}
}

func TestLoginPassesEmailAndInherits(t *testing.T) {
	f := statusFake("you@x.com")
	st, err := loginPlain(context.Background(), f, testPaths(t), registry.Account{Name: "work", Dir: "/d"}, "you@x.com")
	if err != nil || st.Email != "you@x.com" {
		t.Fatalf("%+v %v", st, err)
	}
	c := f.Calls[0]
	if strings.Join(c.Args, " ") != "auth login --claudeai --email you@x.com" || !c.Inherit {
		t.Fatalf("%+v", c)
	}
}

func TestLoginMismatch(t *testing.T) {
	_, err := loginPlain(context.Background(), statusFake("other@x.com"), testPaths(t), registry.Account{Name: "work", Dir: "/d"}, "you@x.com")
	var mm *MismatchError
	if !errors.As(err, &mm) || mm.Want != "you@x.com" || mm.Got != "other@x.com" {
		t.Fatalf("%v", err)
	}
}

func TestLoginWithoutEmailOmitsFlag(t *testing.T) {
	f := statusFake("a@x.com")
	if _, err := loginPlain(context.Background(), f, testPaths(t), registry.Account{Name: "main"}, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(f.Calls[0].Args, " "), "--email") {
		t.Fatal("unexpected --email")
	}
}

func TestEnvNamesTheAccount(t *testing.T) {
	base := []string{"A=1", "CCM_ACCOUNT=old"}
	for _, a := range []registry.Account{{Name: "main"}, {Name: "work", Dir: "/d/work"}} {
		got := Env(a, base)
		n := 0
		for _, e := range got {
			if strings.HasPrefix(e, "CCM_ACCOUNT=") {
				n++
				if e != "CCM_ACCOUNT="+a.Name {
					t.Fatalf("%s: got %q", a.Name, e)
				}
			}
		}
		if n != 1 || base[1] != "CCM_ACCOUNT=old" {
			t.Fatalf("%s: got %v base %v", a.Name, got, base)
		}
	}
}
