package keychain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/run"
)

const credsJSON = `{"claudeAiOauth":{"accessToken":"tok","expiresAt":5,"subscriptionType":"max","rateLimitTier":"t","accountUuid":"u"}}`

// writeCreds writes a credentials file under dir with the given mode.
func writeCreds(t *testing.T, dir, body string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".credentials.json")
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func missingKeychain() *run.Fake { return fakeWith(run.Result{Code: notFoundCode}, nil) }

func TestReadFallsBackToFileWhenKeychainMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "acct")
	writeCreds(t, dir, credsJSON, 0o600)

	c, err := Read(context.Background(), missingKeychain(), registry.Account{Name: "acct", Dir: dir})

	if err != nil || c == nil {
		t.Fatalf("got %v, %v", c, err)
	}
	if c.AccessToken != "tok" || c.SubscriptionType != "max" || c.AccountUUID != "u" || c.ExpiresAt != 5 {
		t.Fatalf("wrong creds %+v", *c)
	}
}

func TestReadMainUsesHomeClaudeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeCreds(t, filepath.Join(home, ".claude"), credsJSON, 0o600)

	c, err := Read(context.Background(), missingKeychain(), registry.Account{Name: "main"})

	if err != nil || c == nil {
		t.Fatalf("got %v, %v", c, err)
	}
}

func TestReadRefusesLoosePermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "acct")
	writeCreds(t, dir, credsJSON, 0o644)

	c, err := Read(context.Background(), missingKeychain(), registry.Account{Name: "acct", Dir: dir})

	if err != nil || c != nil {
		t.Fatalf("want nil, nil; got %v, %v", c, err)
	}
}

func TestReadRefusesSymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	target := writeCreds(t, filepath.Join(home, "real"), credsJSON, 0o600)
	dir := filepath.Join(home, "acct")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, ".credentials.json")); err != nil {
		t.Fatal(err)
	}

	c, err := Read(context.Background(), missingKeychain(), registry.Account{Name: "acct", Dir: dir})

	if err != nil || c != nil {
		t.Fatalf("want nil, nil; got %v, %v", c, err)
	}
}

func TestReadKeychainWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "acct")
	writeCreds(t, dir, credsJSON, 0o600)
	kc := `{"claudeAiOauth":{"accessToken":"kc"}}`
	f := fakeWith(run.Result{Code: 0, Stdout: kc}, nil)

	c, err := Read(context.Background(), f, registry.Account{Name: "acct", Dir: dir})

	if err != nil || c == nil || c.AccessToken != "kc" {
		t.Fatalf("got %v, %v", c, err)
	}
}

func TestPresentTrueFromFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "acct")
	acct := registry.Account{Name: "acct", Dir: dir}

	ok, err := Present(context.Background(), missingKeychain(), acct)
	if err != nil || ok {
		t.Fatalf("no file: got %v, %v", ok, err)
	}
	writeCreds(t, dir, credsJSON, 0o600)
	ok, err = Present(context.Background(), missingKeychain(), acct)
	if err != nil || !ok {
		t.Fatalf("with file: got %v, %v", ok, err)
	}
}

func TestReadBadFileJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "acct")
	writeCreds(t, dir, "{", 0o600)

	_, err := Read(context.Background(), missingKeychain(), registry.Account{Name: "acct", Dir: dir})

	if !errors.Is(err, ErrCredsParse) {
		t.Fatalf("want ErrCredsParse, got %v", err)
	}
}

func TestPresentLockedKeychainFallsBackToFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "acct")
	acct := registry.Account{Name: "acct", Dir: dir}
	locked := fakeWith(run.Result{Code: 36}, nil)

	ok, err := Present(context.Background(), locked, acct)
	if err != nil || ok {
		t.Fatalf("no file: got %v, %v", ok, err)
	}
	writeCreds(t, dir, credsJSON, 0o600)
	if ok, err = Present(context.Background(), locked, acct); err != nil || !ok {
		t.Fatalf("with file: got %v, %v", ok, err)
	}
}
