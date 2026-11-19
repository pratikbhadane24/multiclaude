package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"multiclaude/internal/auth"
	"multiclaude/internal/registry"
)

func TestParseInterspersedStopsAtDoubleDash(t *testing.T) {
	e := newCLI(t, "")
	fs := e.app.newFlags("x")
	purge := fs.Bool("purge", false, "")

	pos, err := parseInterspersed(fs, []string{"a", "--purge", "--", "--b"})

	if err != nil || !*purge || !reflect.DeepEqual(pos, []string{"a", "--b"}) {
		t.Fatalf("pos=%q purge=%v err=%v", pos, *purge, err)
	}
}

func TestSubcommandHelpFlagExitsZero(t *testing.T) {
	e := newCLI(t, "")
	if code := e.main("doctor", "-h"); code != ExitOK {
		t.Fatalf("code=%d", code)
	}
}

func TestWithWatchDefault(t *testing.T) {
	cases := map[string][2][]string{
		"bare at end":   {{"--watch"}, {"--watch=30s"}},
		"bare then arg": {{"-watch", "x"}, {"--watch=30s", "x"}},
		"with value":    {{"--watch", "5s"}, {"--watch", "5s"}},
		"other flags":   {{"--json"}, {"--json"}},
	}
	for name, tc := range cases {
		if got := withWatchDefault(tc[0]); !reflect.DeepEqual(got, tc[1]) {
			t.Errorf("%s: got %q want %q", name, got, tc[1])
		}
	}
}

func TestLoginText(t *testing.T) {
	work := registry.Account{Name: "work", Email: "w@x.com"}
	cases := []struct {
		r    loginResult
		want string
	}{
		{loginResult{err: errors.New("boom")}, "unknown"},
		{loginResult{st: auth.Status{LoggedIn: false}}, "no"},
		{loginResult{st: auth.Status{LoggedIn: true, Email: "W@x.com"}}, "yes"},
		{loginResult{st: auth.Status{LoggedIn: true, Email: "o@x.com"}}, "yes (as o@x.com)"},
	}
	for _, tc := range cases {
		if got := loginText(work, tc.r); got != tc.want {
			t.Errorf("got %q want %q", got, tc.want)
		}
	}
}

func TestStatusWatchBareFlagIsAccepted(t *testing.T) {
	e := newCLI(t, "")
	// --json with a bare --watch is still rejected as a combination, proving
	// the bare form parsed as a watch flag rather than a usage error.
	if code := e.main("status", "--watch", "--json"); code != ExitUsage {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	if !strings.Contains(e.err.String(), "cannot be combined") {
		t.Fatalf("err=%s", e.err)
	}
}
