package doctor

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/links"
	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
)

type fakeInfo struct {
	os.FileInfo
	mode os.FileMode
}

func (f fakeInfo) Mode() os.FileMode { return f.mode }
func (f fakeInfo) IsDir() bool       { return f.mode.IsDir() }

type calls struct{ link, replaced, mcpSync, keychain, t3 int }

type scenario struct {
	items       []links.Item
	drift       bool
	keychainOK  bool
	t3OK        bool
	realMissing bool
	realNotExec bool
	shimMissing bool
	sameTarget  bool
	noT3        bool
	linkErr     error
}

func build(s scenario, accts []registry.Account) (Deps, *calls) {
	c := &calls{}
	p := paths.Paths{RealClaude: "/real/claude", ShimPath: "/shim/claude"}
	d := Deps{
		Paths:    p,
		Accounts: accts,
		Now:      func() time.Time { return time.Unix(100, 0) },
		Stat: func(path string) (os.FileInfo, error) {
			if (path == p.RealClaude && s.realMissing) || (path == p.ShimPath && s.shimMissing) {
				return nil, os.ErrNotExist
			}
			if path == p.RealClaude && s.realNotExec {
				return fakeInfo{mode: 0o644}, nil
			}
			return fakeInfo{mode: 0o755}, nil
		},
		EvalSymlinks: func(path string) (string, error) {
			if s.sameTarget {
				return "/same", nil
			}
			return path, nil
		},
		LookPath: func(string) (string, error) {
			if s.noT3 {
				return "", errors.New("not found")
			}
			return "/bin/t3", nil
		},
		Inspect: func(paths.Paths, string) ([]links.Item, error) { return s.items, nil },
		LinkMissing: func(paths.Paths, string) ([]string, error) {
			c.link++
			return []string{"rules"}, s.linkErr
		},
		FixReplaced: func(_ paths.Paths, _, item string, _ time.Time) (links.FixResult, error) {
			c.replaced++
			return links.FixResult{Action: "relinked-with-backup", BackupPath: "/b/" + item + ".bak"}, nil
		},
		MCPDrift: func(paths.Paths, string) (bool, error) { return s.drift, nil },
		MCPSync:  func(paths.Paths, string) (bool, error) { c.mcpSync++; return true, nil },
		KeychainPresent: func(context.Context, registry.Account) (bool, error) {
			c.keychain++
			return s.keychainOK, nil
		},
		T3HasInstance: func(registry.Account) (bool, error) { c.t3++; return s.t3OK, nil },
	}
	return d, c
}

var (
	mainAcct = registry.Account{Name: "main", Dir: "/h/.claude"}
	workAcct = registry.Account{Name: "work", Dir: "/h/.claude-accounts/work"}
	healthy  = scenario{keychainOK: true, t3OK: true, items: []links.Item{{Name: "rules", State: links.StateOK}}}
)

func find(cs []Check, acct, name string) (Check, bool) {
	for _, c := range cs {
		if c.Account == acct && strings.Contains(c.Name, name) {
			return c, true
		}
	}
	return Check{}, false
}

func TestAllGood(t *testing.T) {
	d, _ := build(healthy, []registry.Account{mainAcct, workAcct})
	cs := Run(context.Background(), d, false)
	code, text := Summary(cs)
	if code != 0 || !strings.Contains(text, "doctor: all good") {
		t.Fatalf("code=%d text=%q", code, text)
	}
}

func TestGlobalChecks(t *testing.T) {
	tests := []struct {
		name   string
		mod    func(*scenario)
		check  string
		status Status
		detail string
	}{
		{"real missing", func(s *scenario) { s.realMissing = true }, "real claude", StatusFail, ""},
		{"real not executable", func(s *scenario) { s.realNotExec = true }, "real claude", StatusFail, ""},
		{"real is shim", func(s *scenario) { s.sameTarget = true }, "shim", StatusFail, "set CCM_CLAUDE_BIN"},
		{"shim missing", func(s *scenario) { s.shimMissing = true }, "shim", StatusFail, "run: ccm install"},
		{"t3 missing", func(s *scenario) { s.noT3 = true }, "t3", StatusWarn, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := healthy
			tc.mod(&s)
			d, _ := build(s, nil)
			cs := Run(context.Background(), d, true)
			var hit bool
			for _, c := range cs {
				if c.Account == "" && c.Status == tc.status && strings.Contains(c.Name, tc.check) && strings.Contains(c.Detail, tc.detail) {
					hit = true
				}
			}
			if !hit {
				t.Fatalf("no matching check in %+v", cs)
			}
		})
	}
}

func TestAccountChecksReadOnly(t *testing.T) {
	tests := []struct {
		name   string
		mod    func(*scenario)
		check  string
		status Status
		detail string
	}{
		{"replaced", func(s *scenario) { s.items = []links.Item{{Name: "rules", State: links.StateReplaced}} }, "rules", StatusFail, "rules is no longer shared"},
		{"missing", func(s *scenario) { s.items = []links.Item{{Name: "rules", State: links.StateMissing}} }, "rules", StatusWarn, ""},
		{"wrong target", func(s *scenario) { s.items = []links.Item{{Name: "rules", State: links.StateWrongTarget}} }, "rules", StatusWarn, ""},
		{"drift", func(s *scenario) { s.drift = true }, "MCP", StatusWarn, "MCP servers out of sync"},
		{"logged out", func(s *scenario) { s.keychainOK = false }, "keychain", StatusWarn, "ccm login work"},
		{"not in t3", func(s *scenario) { s.t3OK = false }, "T3", StatusWarn, "ccm t3-sync"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := healthy
			tc.mod(&s)
			d, c := build(s, []registry.Account{workAcct})
			cs := Run(context.Background(), d, false)
			got, ok := find(cs, "work", tc.check)
			if !ok || got.Status != tc.status || !strings.Contains(got.Detail, tc.detail) {
				t.Fatalf("got %+v ok=%v in %+v", got, ok, cs)
			}
			if c.link+c.replaced+c.mcpSync != 0 {
				t.Fatalf("fix=false mutated: %+v", c)
			}
			if code, _ := Summary(cs); code != 1 {
				t.Fatalf("code=%d", code)
			}
		})
	}
}

func TestNoSourceIgnored(t *testing.T) {
	s := healthy
	s.items = []links.Item{{Name: "hooks", State: links.StateNoSource}}
	d, _ := build(s, []registry.Account{workAcct})
	if code, _ := Summary(Run(context.Background(), d, false)); code != 0 {
		t.Fatal("no-source should be ignored")
	}
}

func TestMainOnlyKeychainAndT3(t *testing.T) {
	s := healthy
	s.drift = true
	s.items = []links.Item{{Name: "rules", State: links.StateReplaced}}
	d, c := build(s, []registry.Account{mainAcct})
	cs := Run(context.Background(), d, true)
	if code, _ := Summary(cs); code != 0 {
		t.Fatalf("main should skip links/mcp: %+v", cs)
	}
	if c.link+c.replaced+c.mcpSync != 0 || c.keychain != 1 || c.t3 != 1 {
		t.Fatalf("calls %+v", c)
	}
}

func TestFixReplacedPrintsBackup(t *testing.T) {
	s := healthy
	s.items = []links.Item{{Name: "rules", State: links.StateReplaced}}
	d, c := build(s, []registry.Account{workAcct})
	cs := Run(context.Background(), d, true)
	got, _ := find(cs, "work", "rules")
	if got.Status != StatusFixed || !strings.Contains(got.Detail, "backup: /b/rules.bak") || !strings.Contains(got.Detail, "relinked-with-backup") {
		t.Fatalf("%+v", got)
	}
	code, text := Summary(cs)
	if code != 0 || c.replaced != 1 || !strings.Contains(text, "/b/rules.bak") || !strings.Contains(text, "✓ fixed") {
		t.Fatalf("code=%d calls=%+v text=%q", code, c, text)
	}
}

func TestFixMissingAndDrift(t *testing.T) {
	s := healthy
	s.drift = true
	s.items = []links.Item{{Name: "rules", State: links.StateMissing}}
	d, c := build(s, []registry.Account{workAcct})
	cs := Run(context.Background(), d, true)
	if code, _ := Summary(cs); code != 0 || c.link != 1 || c.mcpSync != 1 {
		t.Fatalf("code=%d calls=%+v cs=%+v", code, c, cs)
	}
}

func TestFixDoesNotTouchKeychainOrT3BeyondRead(t *testing.T) {
	s := healthy
	s.keychainOK, s.t3OK = false, false
	d, c := build(s, []registry.Account{workAcct})
	cs := Run(context.Background(), d, true)
	if c.keychain != 1 || c.t3 != 1 {
		t.Fatalf("calls %+v", c)
	}
	if code, _ := Summary(cs); code != 1 {
		t.Fatal("warns must remain")
	}
}

func TestFixErrorIsFail(t *testing.T) {
	s := healthy
	s.linkErr = errors.New("boom")
	s.items = []links.Item{{Name: "rules", State: links.StateMissing}}
	d, _ := build(s, []registry.Account{workAcct})
	cs := Run(context.Background(), d, true)
	got, _ := find(cs, "work", "rules")
	if got.Status != StatusFail || !strings.Contains(got.Detail, "boom") {
		t.Fatalf("%+v", got)
	}
	code, text := Summary(cs)
	if code != 1 || !strings.Contains(text, "doctor: 1 problems") || !strings.Contains(text, "✗ [work]") {
		t.Fatalf("code=%d %q", code, text)
	}
}

func TestRunSkipsT3ChecksWhenT3NotSetUp(t *testing.T) {
	d, _ := build(scenario{keychainOK: true, noT3: true}, []registry.Account{{Name: "main"}})
	d.Paths.T3Settings = "/no/t3/settings.json"
	stat := d.Stat
	d.Stat = func(path string) (os.FileInfo, error) {
		if path == d.Paths.T3Settings {
			return nil, os.ErrNotExist
		}
		return stat(path)
	}

	for _, c := range Run(context.Background(), d, false) {
		if strings.Contains(c.Name, "T3") || strings.Contains(c.Name, "t3") {
			t.Fatalf("T3 check ran without T3: %+v", c)
		}
	}
}
