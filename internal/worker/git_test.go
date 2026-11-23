package worker

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/run"
)

func hubGit(out string) *run.Fake {
	return &run.Fake{Respond: func(c run.Call) (run.Result, error) {
		if c.Name == "git" {
			return run.Result{Stdout: out}, nil
		}
		if c.Name == "gh" {
			return run.Result{Stdout: "gh version 2.105.0 (2026-11-17)\n"}, nil
		}
		return run.Result{}, nil
	}}
}

func TestGitIdentityCopiesOnlyIdentityKeys(t *testing.T) {
	r := newFakeRemote()
	s := newSetup(t, r)
	s.Local = hubGit("user.name Pratik Bhadane\nuser.email p@x.com\nurl.https://github.com/.insteadof git@github.com:\n")

	if err := s.GitIdentity(context.Background()); err != nil {
		t.Fatal(err)
	}

	all := strings.Join(r.scripts, "\n")
	for _, want := range []string{
		"git config --global 'user.name' 'Pratik Bhadane'",
		"git config --global 'user.email' 'p@x.com'",
		"git config --global 'url.https://github.com/.insteadof' 'git@github.com:'",
	} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in:\n%s", want, all)
		}
	}
	if call := s.Local.(*run.Fake).Calls[0]; !slices.Contains(call.Args, "--get-regexp") {
		t.Fatalf("hub git read = %v", call.Args)
	}
}

func TestGitIdentityRejectsMultilineValues(t *testing.T) {
	s := newSetup(t, newFakeRemote())
	s.Local = hubGit("user.name a\nuser.email x\n")
	s.Local.(*run.Fake).Respond = func(run.Call) (run.Result, error) {
		return run.Result{Stdout: "user.name ok\nuser.email \x1b[31mx\n"}, nil
	}

	if err := s.GitIdentity(context.Background()); err == nil {
		t.Fatal("want error for control characters")
	}
}

func TestInstallGHSkipsWhenPresent(t *testing.T) {
	r := newFakeRemote()
	s := newSetup(t, r)
	s.info.HasGH = true

	if err := s.InstallGH(context.Background()); err != nil || len(r.scripts) != 0 {
		t.Fatalf("err=%v scripts=%v", err, r.scripts)
	}
}

func TestInstallGHDownloadsHubVersion(t *testing.T) {
	r := newFakeRemote()
	s := newSetup(t, r)
	s.Local = hubGit("")
	s.info.Arch = "arm64"

	if err := s.InstallGH(context.Background()); err != nil {
		t.Fatal(err)
	}

	script := strings.Join(r.scripts, "\n")
	if !strings.Contains(script, "https://github.com/cli/cli/releases/download/v2.105.0/gh_2.105.0_macOS_arm64.zip") ||
		!strings.Contains(script, ".local/bin/gh") {
		t.Fatalf("script:\n%s", script)
	}
}

func TestGitHubLoginOnlyWhenLoggedOut(t *testing.T) {
	r := newFakeRemote()
	r.respond["gh auth status"] = run.Result{Code: 1}
	s := newSetup(t, r)

	if err := s.GitHubLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.interactive) != 1 || !strings.Contains(r.interactive[0], "gh auth login --web") {
		t.Fatalf("interactive = %v", r.interactive)
	}

	r2 := newFakeRemote()
	s2 := newSetup(t, r2)
	if err := s2.GitHubLogin(context.Background()); err != nil || len(r2.interactive) != 0 {
		t.Fatalf("logged in: err=%v interactive=%v", err, r2.interactive)
	}
}
