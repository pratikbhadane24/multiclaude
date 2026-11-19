package paths

import "testing"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolveDefaults(t *testing.T) {
	got := Resolve(env(nil), "/h")
	want := Paths{
		Home:         "/h",
		ClaudeDir:    "/h/.claude",
		ClaudeJSON:   "/h/.claude.json",
		AccountsRoot: "/h/.claude-accounts",
		ShimPath:     "/h/.claude-accounts/bin/claude",
		RealClaude:   "/h/.local/bin/claude",
		T3Settings:   "/h/.t3/userdata/settings.json",
	}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestResolveCCMHomeOverride(t *testing.T) {
	got := Resolve(env(map[string]string{"CCM_HOME": "/x"}), "/h")
	if got.AccountsRoot != "/x" || got.ShimPath != "/x/bin/claude" {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveClaudeBinOverride(t *testing.T) {
	got := Resolve(env(map[string]string{"CCM_CLAUDE_BIN": "/b"}), "/h")
	if got.RealClaude != "/b" {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveNormalizesCCMHome(t *testing.T) {
	got := Resolve(func(k string) string {
		if k == "CCM_HOME" {
			return "/x/"
		}
		return ""
	}, "/h")
	if got.AccountsRoot != "/x" {
		t.Errorf("got %q", got.AccountsRoot)
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"~/a/": "/h/a", "x": "/h/x", "/h/é": "/h/é"} {
		if got := Normalize(in, "/h"); got != want {
			t.Errorf("%q: got %q", in, got)
		}
	}
}
