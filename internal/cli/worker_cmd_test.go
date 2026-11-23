package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/config"
	"github.com/pratikbhadane24/multiclaude/internal/consts"
)

func TestWorkerAddUnknownAccountIsUsageError(t *testing.T) {
	e := newCLI(t, "")

	code := e.main("worker", "add", "worker1", "--accounts", "nope")

	if code != ExitUsage || !strings.Contains(e.err.String(), "nope") {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}

func TestWorkerAddNeedsAccounts(t *testing.T) {
	e := newCLI(t, "")

	if code := e.main("worker", "add", "worker1"); code != ExitUsage {
		t.Fatalf("code=%d", code)
	}
}

func TestWorkerListEmpty(t *testing.T) {
	e := newCLI(t, "")

	if code := e.main("worker", "list"); code != ExitOK || !strings.Contains(e.out.String(), "no workers") {
		t.Fatalf("code=%d out=%s", code, e.out)
	}
}

func TestWorkerStatusJSONHasNoTokens(t *testing.T) {
	e := newCLI(t, "")

	if code := e.main("worker", "status", "--json"); code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	var v []any
	if err := json.Unmarshal(e.out.Bytes(), &v); err != nil {
		t.Fatalf("not json: %s", e.out)
	}
	if strings.Contains(e.out.String(), testToken) {
		t.Fatal("token in output")
	}
}

func TestConfigSetDefaultAccount(t *testing.T) {
	e := newCLI(t, "")

	if code := e.main("config", "set", "default-account", "best"); code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	cfg, _ := config.Load(e.p)
	if cfg.DefaultAccount != "best" {
		t.Fatalf("DefaultAccount = %q", cfg.DefaultAccount)
	}
	if code := e.main("config", "set", "default-account", "off"); code != ExitOK {
		t.Fatal(code)
	}
	if cfg, _ = config.Load(e.p); cfg.DefaultAccount != "" {
		t.Fatalf("DefaultAccount = %q", cfg.DefaultAccount)
	}
	if code := e.main("config", "set", "default-account", "x"); code != ExitUsage {
		t.Fatalf("bad value code=%d", code)
	}
}

func TestListJSON(t *testing.T) {
	e := newCLI(t, "")

	if code := e.main("list", "--json"); code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	var rows []struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		LoggedIn bool   `json:"loggedIn"`
	}
	if err := json.Unmarshal(e.out.Bytes(), &rows); err != nil || len(rows) != 2 || rows[1].Name != "work" || rows[1].Email != workEmail {
		t.Fatalf("rows=%+v err=%v out=%s", rows, err, e.out)
	}
}

func TestWorkerIsReserved(t *testing.T) {
	if _, ok := consts.ReservedNames["worker"]; !ok {
		t.Fatal("worker not reserved")
	}
}

func TestWorkerAddDuplicateAccountIsUsageError(t *testing.T) {
	e := newCLI(t, "")

	if code := e.main("worker", "add", "worker1", "--accounts", "work,work"); code != ExitUsage {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}

func TestListJSONHasLoginEmail(t *testing.T) {
	e := newCLI(t, "")

	if code := e.main("list", "--json"); code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(e.out.String(), `"loginEmail"`) {
		t.Fatalf("no loginEmail: %s", e.out)
	}
}
