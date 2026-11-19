package cli

import (
	"reflect"
	"strings"
	"testing"

	"multiclaude/internal/config"
)

func TestDashLatestSessionMatchesContinueLookup(t *testing.T) {
	e := continueEnv(t)

	id, err := e.app.dashDeps().LatestSession()

	if err != nil || id != "sess-1" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}

func TestDashLatestSessionNoneIsAnError(t *testing.T) {
	e := newCLI(t, "")
	prev := getwd
	getwd = func() (string, error) { return "/nowhere/at/all", nil }
	t.Cleanup(func() { getwd = prev })

	if _, err := e.app.dashDeps().LatestSession(); err == nil || !strings.Contains(err.Error(), "no conversation") {
		t.Fatalf("err=%v", err)
	}
}

func TestDashContinueLaunchMatchesCcmContinue(t *testing.T) {
	e := continueEnv(t)
	e.setConfig(t, config.Config{DefaultArgs: []string{"--model", "opus"}})
	if code := e.main("continue", "--from", "main", "--to", "work"); code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	id, err := e.app.dashDeps().LatestSession()
	if err != nil {
		t.Fatal(err)
	}

	c, err := e.app.dashDeps().Launch("work", []string{"--resume", id})

	calls := e.callsTo(e.p.RealClaude)
	if err != nil || len(calls) != 1 || !reflect.DeepEqual(c.Args[1:], calls[0].Args) || !reflect.DeepEqual(c.Env, calls[0].Env) {
		t.Fatalf("dash=%q continue=%v err=%v", c.Args, calls, err)
	}
}
