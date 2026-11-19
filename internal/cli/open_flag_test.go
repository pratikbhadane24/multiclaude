package cli

import (
	"context"
	"strings"
	"testing"

	"multiclaude/internal/registry"
	"multiclaude/internal/run"
)

// ctxRunner records whether the context handed to the login call is cancellable.
type ctxRunner struct {
	inner       run.Runner
	loginCancel []bool
}

func (c *ctxRunner) Run(ctx context.Context, name string, args []string, env []string, inherit bool) (run.Result, error) {
	if len(args) > 1 && args[1] == "login" {
		c.loginCancel = append(c.loginCancel, ctx.Done() != nil)
	}
	return c.inner.Run(ctx, name, args, env, inherit)
}

func TestLoginDetachesInteractiveCallFromCancel(t *testing.T) {
	h := newHarness(t, "")
	cr := &ctxRunner{inner: h.fake}
	h.app.Runner = cr
	_ = registry.Save(h.p, []registry.Account{{Name: "work", Dir: "/d", Email: "you@x.com"}})
	h.emails["/d"] = "you@x.com"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h.app.Login(ctx, "work")

	if len(cr.loginCancel) != 1 || cr.loginCancel[0] {
		t.Fatalf("login ctx still cancellable: %v", cr.loginCancel)
	}
}

func TestAddDetachesInteractiveCallFromCancel(t *testing.T) {
	h := newHarness(t, "")
	cr := &ctxRunner{inner: h.fake}
	h.app.Runner = cr
	h.emails[h.p.AccountsRoot+"/work"] = "you@x.com"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h.app.Add(ctx, "work", "you@x.com")

	if len(cr.loginCancel) != 1 || cr.loginCancel[0] {
		t.Fatalf("add login ctx still cancellable: %v", cr.loginCancel)
	}
}

func TestLoginWarnsWithoutSelfExe(t *testing.T) {
	h := newHarness(t, "")
	_ = registry.Save(h.p, []registry.Account{{Name: "work", Dir: "/d", Email: "you@x.com"}})
	h.emails["/d"] = "you@x.com"

	h.app.Login(context.Background(), "work")

	want := "ccm: warning: could not resolve ccm path; Claude may open your default browser"
	if !strings.Contains(h.err.String(), want) {
		t.Fatalf("err %q", h.err)
	}
}

func TestLoginOpenFlagSavesMode(t *testing.T) {
	e := newCLI(t, "")
	e.main("login", "work", "--open", "incognito")
	accts, _ := registry.Load(e.app.Paths)
	if a, _ := registry.Find(accts, "work"); a.Browser != "incognito" {
		t.Fatalf("browser %q err %s", a.Browser, e.err)
	}
}

func TestLoginOpenFlagInvalidStopsBeforeLogin(t *testing.T) {
	e := newCLI(t, "")
	if code := e.main("login", "work", "--open", "firefox"); code != ExitError {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(e.err.String(), "invalid login browser") {
		t.Fatalf("err %s", e.err)
	}
}

func TestAddOpenFlagInvalid(t *testing.T) {
	e := newCLI(t, "")
	if code := e.main("add", "new", "--email", "n@x.com", "--open", "firefox"); code != ExitError {
		t.Fatalf("code %d", code)
	}
	accts, _ := registry.Load(e.app.Paths)
	if _, ok := registry.Find(accts, "new"); ok {
		t.Fatal("account created despite invalid --open")
	}
}

func TestAddOpenFlagStored(t *testing.T) {
	e := newCLI(t, "")
	e.main("add", "new", "--email", "n@x.com", "--open", "chrome:Profile 2")
	accts, _ := registry.Load(e.app.Paths)
	if a, _ := registry.Find(accts, "new"); a.Browser != "chrome:Profile 2" {
		t.Fatalf("%+v", a)
	}
}

func TestLoginAllWithOpenIsUsageError(t *testing.T) {
	e := newCLI(t, "")
	if code := e.main("login", "--all", "--open", "copy"); code != ExitUsage {
		t.Fatalf("code %d", code)
	}
}

func TestHelpDescribesOpen(t *testing.T) {
	e := newCLI(t, "")
	e.main("help")
	out := e.out.String()
	for _, w := range []string{"--open", "copy|incognito|chrome:<Profile dir>", "clipboard"} {
		if !strings.Contains(out, w) {
			t.Errorf("help missing %q", w)
		}
	}
}
