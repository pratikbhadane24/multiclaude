package cli

import (
	"context"
	"strings"
	"testing"

	"multiclaude/internal/registry"
)

func TestAddRefusesMainsEmailExit2(t *testing.T) {
	h := newHarness(t, "")
	h.loggedIn[""], h.emails[""] = true, "Admin@X.com"
	code := h.app.Add(context.Background(), "x", "admin@x.com")
	if code != 2 {
		t.Fatalf("code %d err %s", code, h.err)
	}
	if !strings.Contains(h.err.String(), `already used by account "main"`) {
		t.Fatalf("err: %s", h.err)
	}
	accts, _ := registry.Load(h.p)
	if _, ok := registry.Find(accts, "x"); ok || h.loginCalls() != 0 {
		t.Fatalf("account must not be created: %+v logins=%d", accts, h.loginCalls())
	}
}

func TestAddAllowedWhenMainLoggedOutOrDifferent(t *testing.T) {
	h := newHarness(t, "")
	h.emails[""] = "admin@x.com" // main is logged out: nothing to compare
	dir := h.p.AccountsRoot + "/x"
	h.emails[dir] = "admin@x.com"
	if code := h.app.Add(context.Background(), "x", "admin@x.com"); code != 0 {
		t.Fatalf("code %d err %s", code, h.err)
	}
	h2 := newHarness(t, "")
	h2.loggedIn[""], h2.emails[""] = true, "main@x.com"
	h2.emails[h2.p.AccountsRoot+"/y"] = "y@x.com"
	if code := h2.app.Add(context.Background(), "y", "y@x.com"); code != 0 {
		t.Fatalf("code %d err %s", code, h2.err)
	}
}
