package cli

import (
	"strings"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/consts"
)

func TestVersionCommandPrintsVersion(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		e := newCLI(t, "")

		code := e.main(arg)

		if code != ExitOK {
			t.Fatalf("%s: code=%d", arg, code)
		}
		if got, want := e.out.String(), "ccm "+versionString()+"\n"; got != want {
			t.Errorf("%s: got %q want %q", arg, got, want)
		}
	}
}

func TestVersionIsReservedAccountName(t *testing.T) {
	if _, ok := consts.ReservedNames["version"]; !ok {
		t.Fatal("version must be a reserved account name")
	}
}

func TestUsageTextListsVersion(t *testing.T) {
	if !strings.Contains(usageText, "\n  version ") {
		t.Fatal("usage text does not list the version command")
	}
}

func TestVersionStringPrefersBuildTimeVersion(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	Version = "v1.2.3"

	if got := versionString(); got != "v1.2.3" {
		t.Fatalf("got %q", got)
	}
}
