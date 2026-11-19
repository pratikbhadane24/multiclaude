package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"multiclaude/internal/run"
)

// sameLoginEnv has main and work both logged in as workEmail.
func sameLoginEnv(t *testing.T) *cliEnv {
	t.Helper()
	e := newCLI(t, "")
	e.loginWithUsage("", usageBody("40", "20"))
	e.loginWithUsage(e.workDir, usageBody("40", "20"))
	inner := e.fake.Respond
	e.fake.Respond = func(c run.Call) (run.Result, error) {
		if len(c.Args) >= 2 && c.Args[0] == "auth" && c.Args[1] == "status" {
			return run.Result{Stdout: `{"loggedIn":true,"email":"` + workEmail + `"}`}, nil
		}
		return inner(c)
	}
	return e
}

func TestStatusJSONCollapsesSameLogin(t *testing.T) {
	e := sameLoginEnv(t)
	if code := e.main("status", "--json"); code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	var got struct {
		Accounts []struct {
			Account string
			Aliases []string
		}
		Combined struct{ Counted int }
	}
	if err := json.Unmarshal(e.out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Accounts) != 1 || got.Accounts[0].Account != "main" || len(got.Accounts[0].Aliases) != 1 || got.Accounts[0].Aliases[0] != "work" || got.Combined.Counted != 1 {
		t.Fatalf("%s", e.out)
	}
}

func TestStatusAllShowsSameAsRow(t *testing.T) {
	e := sameLoginEnv(t)
	if code := e.main("status", "--all"); code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	out := e.out.String()
	if !strings.Contains(out, "same as main") || !strings.Contains(out, "available now 1") {
		t.Fatalf("%s", out)
	}
	e.out.Reset()
	e.main("status")
	if !strings.Contains(e.out.String(), "main (= work)") || strings.Contains(e.out.String(), "same as") {
		t.Fatalf("%s", e.out)
	}
}

func TestDoctorShowsDuplicateAsInfo(t *testing.T) {
	e := sameLoginEnv(t)
	healthyLayout(t, e)
	e.main("sync")
	e.main("t3-sync")
	e.out.Reset()
	e.main("doctor")
	if !strings.Contains(e.out.String(), "i [work] same login as main — shown as one row") {
		t.Fatalf("%s", e.out)
	}
}
