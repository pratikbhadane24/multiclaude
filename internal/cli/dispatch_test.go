package cli

import (
	"strings"
	"testing"
)

func TestMainRoutesEachCommand(t *testing.T) {
	cases := []struct {
		args     []string
		in       string
		wantCode int
		wantText string // substring of stdout+stderr proving the handler ran
	}{
		{nil, "", ExitUsage, "Usage:"},
		{[]string{"help"}, "", ExitOK, "Usage:"},
		{[]string{"-h"}, "", ExitOK, "Usage:"},
		{[]string{"--help"}, "", ExitOK, "Usage:"},
		{[]string{"bogus"}, "", ExitUsage, `unknown command "bogus"`},
		{[]string{"add"}, "", ExitUsage, "usage: ccm add"},
		{[]string{"add", "new", "--email", "n@x.com"}, "", ExitMismatch, "Sign in as n@x.com"},
		{[]string{"login"}, "", ExitUsage, "usage: ccm login"},
		{[]string{"login", "work"}, "", ExitMismatch, "Sign in as " + workEmail},
		{[]string{"login", "--all"}, "", ExitMismatch, "Sign in as " + workEmail},
		{[]string{"list"}, "", ExitOK, "LOGGED IN"},
		{[]string{"run"}, "", ExitUsage, "usage: ccm run"},
		{[]string{"run", "nope"}, "", ExitError, `unknown account "nope"`},
		{[]string{"run", "work"}, "", ExitOK, ""},
		{[]string{"work"}, "", ExitOK, ""},
		{[]string{"best"}, "", exitAllLimited, "no usable accounts"},
		{[]string{"use"}, "", ExitUsage, "usage: ccm use"},
		{[]string{"use", "work"}, "", ExitOK, "export CLAUDE_CONFIG_DIR="},
		{[]string{"use", "nope"}, "", ExitError, `unknown account "nope"`},
		{[]string{"status", "--json"}, "", ExitOK, `"accounts"`},
		{[]string{"status", "--bogus"}, "", ExitUsage, "flag provided but not defined"},
		{[]string{"status", "--json", "--watch", "1s"}, "", ExitUsage, "cannot be combined"},
		{[]string{"sync"}, "", ExitOK, "work:"},
		{[]string{"t3-sync"}, "", ExitOK, "Restart T3 Code to apply."},
		{[]string{"doctor"}, "", ExitError, "doctor:"},
		{[]string{"remove", "work"}, "n\n", ExitError, "Cancelled."},
		{[]string{"remove"}, "", ExitUsage, "usage: ccm remove"},
		{[]string{"shell-init", "zsh"}, "", ExitOK, `command ccm use "$2"`},
		{[]string{"shell-init", "fish"}, "", ExitError, "unsupported shell"},
		{[]string{"shell-init"}, "", ExitUsage, "usage: ccm shell-init"},
		{[]string{"install"}, "", ExitError, "ccm-shim not found"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			e := newCLI(t, tc.in)

			code := e.main(tc.args...)

			all := e.out.String() + e.err.String()
			if code != tc.wantCode || !strings.Contains(all, tc.wantText) {
				t.Fatalf("code=%d want %d; output:\n%s", code, tc.wantCode, all)
			}
		})
	}
}

func TestUnknownCommandPrintsUsageToStderr(t *testing.T) {
	e := newCLI(t, "")
	e.main("bogus")
	if e.out.Len() != 0 || !strings.Contains(e.err.String(), "Usage:") {
		t.Fatalf("out=%q err=%q", e.out, e.err)
	}
}

func TestUseMainExactOutput(t *testing.T) {
	e := newCLI(t, "")
	if code := e.main("use", "main"); code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	if got, want := e.out.String(), "unset CLAUDE_CONFIG_DIR; export CCM_ACCOUNT='main'\n"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestShellInitIncludesPathLine(t *testing.T) {
	e := newCLI(t, "")
	e.main("shell-init", "zsh")
	if !strings.Contains(e.out.String(), e.p.AccountsRoot+"/bin") {
		t.Fatalf("out=%s", e.out)
	}
}

func TestAddAcceptsEmailFlagBeforeName(t *testing.T) {
	e := newCLI(t, "")
	e.main("add", "--email", "n@x.com", "new")
	if !strings.Contains(e.out.String(), "Sign in as n@x.com") {
		t.Fatalf("out=%s err=%s", e.out, e.err)
	}
}
