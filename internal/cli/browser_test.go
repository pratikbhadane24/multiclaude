package cli

import (
	"context"
	"strings"
	"testing"

	"multiclaude/internal/registry"
)

func TestSetBrowserInvalidMode(t *testing.T) {
	h := newHarness(t, "")
	_ = registry.Save(h.p, []registry.Account{{Name: "work", Dir: "/d"}})
	if code := h.app.SetBrowser("work", "firefox"); code != 1 {
		t.Fatalf("code %d", code)
	}
}

func TestSetBrowserUnknownAndMain(t *testing.T) {
	h := newHarness(t, "")
	if h.app.SetBrowser("nope", "copy") != 1 || h.app.SetBrowser("main", "copy") != 1 {
		t.Fatal("want exit 1")
	}
}

func TestSetBrowserSavesAndPrints(t *testing.T) {
	h := newHarness(t, "")
	_ = registry.Save(h.p, []registry.Account{{Name: "work", Dir: "/d", Email: "you@x.com"}})
	if code := h.app.SetBrowser("work", "chrome:Profile 1"); code != 0 {
		t.Fatalf("code %d %s", code, h.err)
	}
	if h.out.String() != "Login browser for \"work\": chrome:Profile 1\n" {
		t.Fatalf("out %q", h.out)
	}
	accts, _ := registry.Load(h.p)
	if a, _ := registry.Find(accts, "work"); a.Browser != "chrome:Profile 1" || a.Email != "you@x.com" {
		t.Fatalf("%+v", a)
	}
}

func TestLoginOnePassesModeAndSelfExe(t *testing.T) {
	h := newHarness(t, "")
	h.app.SelfExe = "/bin/ccm"
	_ = registry.Save(h.p, []registry.Account{{Name: "work", Dir: "/d", Email: "you@x.com", Browser: "incognito"}})
	h.emails["/d"] = "you@x.com"
	if code := h.app.Login(context.Background(), "work"); code != 0 {
		t.Fatalf("code %d %s", code, h.err)
	}
	env := strings.Join(h.fake.Calls[0].Env, "\n")
	if !strings.Contains(env, "BROWSER=/bin/ccm") || !strings.Contains(env, "CCM_LOGIN_OPEN=incognito") {
		t.Fatalf("env %s", env)
	}
}
