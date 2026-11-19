package cli

import (
	"reflect"
	"strings"
	"testing"

	"multiclaude/internal/config"
	"multiclaude/internal/run"
)

func (e *cliEnv) setConfig(t *testing.T, c config.Config) {
	t.Helper()
	if err := config.Save(e.p, c); err != nil {
		t.Fatal(err)
	}
}

func TestRunPrependsMergedConfigArgs(t *testing.T) {
	e := newCLI(t, "")
	e.setConfig(t, config.Config{
		DefaultArgs: []string{"--dangerously-skip-permissions"},
		Accounts:    map[string]config.AccountConfig{"work": {Args: []string{"--model", "opus"}}},
	})

	e.main("run", "work", "-p", "hi")

	calls := e.callsTo(e.p.RealClaude)
	want := []string{"--dangerously-skip-permissions", "--model", "opus", "-p", "hi"}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Fatalf("calls=%d args=%q", len(calls), firstArgs(calls))
	}
}

func TestShorthandAndBestAlsoGetConfigArgs(t *testing.T) {
	e := newCLI(t, "")
	e.setConfig(t, config.Config{DefaultArgs: []string{"--d"}})
	e.loginWithUsage("", usageBody("90", "20"))
	e.loginWithUsage(e.workDir, usageBody("30", "10"))

	e.main("work", "x")
	e.main("best", "y")

	calls := e.callsTo(e.p.RealClaude)
	if len(calls) != 2 || !reflect.DeepEqual(calls[0].Args, []string{"--d", "x"}) || !reflect.DeepEqual(calls[1].Args, []string{"--d", "y"}) {
		t.Fatalf("calls=%d args=%q", len(calls), firstArgs(calls))
	}
}

func TestInternalAuthCallsDoNotGetConfigArgs(t *testing.T) {
	e := newCLI(t, "")
	e.setConfig(t, config.Config{DefaultArgs: []string{"--dangerously-skip-permissions"}})

	e.main("list")

	for _, c := range e.fake.Calls {
		if slicesContains(c.Args, "--dangerously-skip-permissions") {
			t.Fatalf("internal call got default args: %q", c.Args)
		}
	}
}

func slicesContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestRunWithCorruptConfigWarnsAndLaunches(t *testing.T) {
	e := newCLI(t, "")
	mustWrite(t, e.p.AccountsRoot+"/config.json", "{bad")

	code := e.main("run", "work", "x")

	if code != ExitOK || !strings.Contains(e.err.String(), "warning") {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	if calls := e.callsTo(e.p.RealClaude); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"x"}) {
		t.Fatalf("calls=%d args=%q", len(calls), firstArgs(calls))
	}
}

func TestConfigSetDefaultArgsTakesDashArgsVerbatim(t *testing.T) {
	e := newCLI(t, "")

	code := e.main("config", "set", "default-args", "--dangerously-skip-permissions", "--model", "a b")

	c, _ := config.Load(e.p)
	if code != ExitOK || !reflect.DeepEqual(c.DefaultArgs, []string{"--dangerously-skip-permissions", "--model", "a b"}) {
		t.Fatalf("code=%d c=%+v err=%s", code, c, e.err)
	}
}

func TestConfigSetAccountArgs(t *testing.T) {
	e := newCLI(t, "")

	code := e.main("config", "set", "args", "work", "--model", "opus")

	c, _ := config.Load(e.p)
	if code != ExitOK || !reflect.DeepEqual(c.Accounts["work"].Args, []string{"--model", "opus"}) {
		t.Fatalf("code=%d c=%+v err=%s", code, c, e.err)
	}
}

func TestConfigSetArgsUnknownAccountFails(t *testing.T) {
	e := newCLI(t, "")

	code := e.main("config", "set", "args", "nope", "--x")

	c, _ := config.Load(e.p)
	if code != ExitError || len(c.Accounts) != 0 || !strings.Contains(e.err.String(), `unknown account "nope"`) {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}

func TestConfigSetNeedsArgs(t *testing.T) {
	e := newCLI(t, "")
	for _, args := range [][]string{
		{"config", "set"}, {"config", "set", "default-args"}, {"config", "set", "args", "work"},
		{"config", "set", "bogus", "x"}, {"config", "bogus"}, {"config"}, {"config", "clear"},
		{"config", "clear", "args"}, {"config", "show", "extra"}, {"config", "clear", "default-args", "x"},
	} {
		if code := e.main(args...); code != ExitUsage {
			t.Errorf("%v: code=%d", args, code)
		}
	}
}

func TestConfigClear(t *testing.T) {
	e := newCLI(t, "")
	e.setConfig(t, config.Config{DefaultArgs: []string{"--d"}, Accounts: map[string]config.AccountConfig{"work": {Args: []string{"--a"}}}})

	if code := e.main("config", "clear", "args", "work"); code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	c, _ := config.Load(e.p)
	if len(c.Accounts) != 0 || len(c.DefaultArgs) != 1 {
		t.Fatalf("c=%+v", c)
	}
	if code := e.main("config", "clear", "default-args"); code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	c, _ = config.Load(e.p)
	if len(c.DefaultArgs) != 0 {
		t.Fatalf("c=%+v", c)
	}
}

func TestConfigShow(t *testing.T) {
	e := newCLI(t, "")
	e.main("config", "show")
	if !strings.Contains(e.out.String(), "default-args: (none)") {
		t.Fatalf("out=%s", e.out)
	}

	e.out.Reset()
	e.setConfig(t, config.Config{DefaultArgs: []string{"--d", "a b"}, Accounts: map[string]config.AccountConfig{"work": {Args: []string{"--a"}}}})
	e.main("config", "show")

	out := e.out.String()
	if !strings.Contains(out, "default-args: --d 'a b'") || !strings.Contains(out, "args work: --a") {
		t.Fatalf("out=%s", out)
	}
}

func TestListAndStatusFallBackToAuthStatusEmailForMain(t *testing.T) {
	e := newCLI(t, "")
	e.fake.Respond = func(c run.Call) (run.Result, error) {
		if len(c.Args) >= 2 && c.Args[0] == "auth" && c.Args[1] == "status" && !envHasKey(c.Env, "CLAUDE_CONFIG_DIR") {
			return run.Result{Stdout: `{"loggedIn":true,"email":"main@x.com","subscriptionType":"max"}`}, nil
		}
		return e.respond(c)
	}

	e.main("list")
	listOut := e.out.String()
	e.out.Reset()
	e.main("status", "--json")

	if !strings.Contains(listOut, "main@x.com") {
		t.Fatalf("list=%s", listOut)
	}
	if !strings.Contains(e.out.String(), `"email": "main@x.com"`) && !strings.Contains(e.out.String(), `"email":"main@x.com"`) {
		t.Fatalf("status=%s", e.out)
	}
}

func firstArgs(calls []run.Call) [][]string {
	out := make([][]string, len(calls))
	for i, c := range calls {
		out[i] = c.Args
	}
	return out
}
