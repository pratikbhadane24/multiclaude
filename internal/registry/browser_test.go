package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBrowserRoundTripAndOldFiles(t *testing.T) {
	p := testPaths(t)
	work := Account{Name: "work", Dir: "/h/w", Browser: "chrome:Profile 1"}
	if err := Save(p, []Account{{Name: "main"}, work}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || got[1] != work {
		t.Fatalf("got %+v %v", got, err)
	}
	old := `[{"name":"old","dir":"/h/o"}]`
	if err := os.WriteFile(filepath.Join(p.AccountsRoot, "accounts.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = Load(p)
	if err != nil || got[1].Browser != "" {
		t.Fatalf("got %+v %v", got, err)
	}
}
