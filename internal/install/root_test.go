package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAccountsRootCreated0700ExistingUntouched(t *testing.T) {
	e := setup(t)
	e.p.AccountsRoot = filepath.Join(filepath.Dir(filepath.Dir(e.p.ShimPath)), "accts")
	if _, err := Install(e.p, e.exe, e.localBin); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(e.p.AccountsRoot)
	if err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("root %v %v", st, err)
	}
	existing := t.TempDir()
	if err := os.Chmod(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	e.p.AccountsRoot = existing
	if _, err := Install(e.p, e.exe, e.localBin); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(existing); st.Mode().Perm() != 0o755 {
		t.Fatalf("existing dir chmodded: %v", st.Mode())
	}
}
