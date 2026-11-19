package browser

import (
	"errors"
	"strings"
	"testing"

	"multiclaude/internal/run"
)

func TestParseMode(t *testing.T) {
	cases := []struct {
		in      string
		want    Mode
		wantErr bool
	}{
		{"", Mode{Kind: KindCopy}, false},
		{"copy", Mode{Kind: KindCopy}, false},
		{"incognito", Mode{Kind: KindIncognito}, false},
		{"chrome:Profile 2", Mode{Kind: KindChromeProfile, Profile: "Profile 2"}, false},
		{"chrome:", Mode{}, true},
		{"firefox", Mode{}, true},
	}
	for _, c := range cases {
		got, err := ParseMode(c.in)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("ParseMode(%q)=%+v,%v", c.in, got, err)
		}
		if err != nil && !strings.Contains(err.Error(), "chrome:<Profile dir>") {
			t.Errorf("error should list forms: %v", err)
		}
		if err == nil && c.in != "" && got.String() != c.in {
			t.Errorf("String()=%q want %q", got.String(), c.in)
		}
	}
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestIsOpenURLInvocation(t *testing.T) {
	acct := env(map[string]string{EnvAccount: "work"})
	none := env(nil)
	cases := []struct {
		args []string
		get  func(string) string
		want bool
	}{
		{[]string{"https://claude.ai/x"}, acct, true},
		{[]string{"https://claude.ai/x"}, none, false},
		{[]string{"http://claude.ai/x"}, acct, false},
		{[]string{"status"}, acct, false},
		{nil, acct, false},
		{[]string{"https://a", "b"}, acct, false},
	}
	for i, c := range cases {
		if got := IsOpenURLInvocation(c.args, c.get); got != c.want {
			t.Errorf("case %d got %v", i, got)
		}
	}
}

type fakeClip struct {
	got []string
}

func (f *fakeClip) copy(s string) error { f.got = append(f.got, s); return nil }

func setup(t *testing.T, clip *fakeClip) {
	t.Helper()
	old := Clipboard
	Clipboard = clip.copy
	t.Cleanup(func() { Clipboard = old })
}

const goodURL = "https://claude.ai/oauth/authorize?code=true&x=1"

func TestHandleCopy(t *testing.T) {
	clip := &fakeClip{}
	setup(t, clip)
	var out strings.Builder
	g := env(map[string]string{EnvAccount: "work", EnvEmail: "me@x.com"})
	fake := &run.Fake{}
	if code := HandleOpenURL(goodURL, g, fake, &out); code != 0 {
		t.Fatalf("code %d", code)
	}
	if len(clip.got) != 1 || clip.got[0] != goodURL || len(fake.Calls) != 0 {
		t.Fatalf("clip %v calls %v", clip.got, fake.Calls)
	}
	want := "Login link for \"work\" (me@x.com) copied to clipboard.\n" +
		"Paste it into the browser or profile signed in to that account, or a private window.\n" + goodURL + "\n"
	if out.String() != want {
		t.Fatalf("got %q", out.String())
	}
}

func TestHandleCopyNoEmail(t *testing.T) {
	setup(t, &fakeClip{})
	var out strings.Builder
	HandleOpenURL(goodURL, env(map[string]string{EnvAccount: "work"}), &run.Fake{}, &out)
	if !strings.HasPrefix(out.String(), "Login link for \"work\" copied to clipboard.\n") {
		t.Fatalf("got %q", out.String())
	}
}

func TestHandleBadModeFallsBackToCopy(t *testing.T) {
	clip := &fakeClip{}
	setup(t, clip)
	g := env(map[string]string{EnvAccount: "w", EnvMode: "bogus"})
	if code := HandleOpenURL(goodURL, g, &run.Fake{}, &strings.Builder{}); code != 0 || len(clip.got) != 1 {
		t.Fatalf("code %d clip %v", code, clip.got)
	}
}

func TestHandleIncognitoAndProfile(t *testing.T) {
	cases := []struct {
		mode string
		want []string
	}{
		{"incognito", []string{"-na", "Google Chrome", "--args", "--incognito", goodURL}},
		{"chrome:Profile 1", []string{"-na", "Google Chrome", "--args", "--profile-directory=Profile 1", goodURL}},
	}
	for _, c := range cases {
		clip := &fakeClip{}
		setup(t, clip)
		fake := &run.Fake{}
		g := env(map[string]string{EnvAccount: "w", EnvMode: c.mode})
		if code := HandleOpenURL(goodURL, g, fake, &strings.Builder{}); code != 0 {
			t.Fatalf("code %d", code)
		}
		if len(fake.Calls) != 1 || fake.Calls[0].Name != "/usr/bin/open" || strings.Join(fake.Calls[0].Args, "|") != strings.Join(c.want, "|") {
			t.Fatalf("calls %+v", fake.Calls)
		}
		if len(clip.got) != 0 {
			t.Fatal("should not copy")
		}
	}
}

func TestHandleOpenFailureFallsBackToCopy(t *testing.T) {
	for _, resp := range []func(run.Call) (run.Result, error){
		func(run.Call) (run.Result, error) { return run.Result{Code: 1}, nil },
		func(run.Call) (run.Result, error) { return run.Result{}, errors.New("boom") },
	} {
		clip := &fakeClip{}
		setup(t, clip)
		var out strings.Builder
		g := env(map[string]string{EnvAccount: "w", EnvMode: "incognito"})
		code := HandleOpenURL(goodURL, g, &run.Fake{Respond: resp}, &out)
		if code != 0 || len(clip.got) != 1 || !strings.Contains(out.String(), "warning") || !strings.Contains(out.String(), "copied to clipboard") {
			t.Fatalf("code %d clip %v out %q", code, clip.got, out.String())
		}
	}
}

func TestHandleDisallowedHost(t *testing.T) {
	for _, u := range []string{"https://evil.example/x", "https://claude.ai.evil.com/x", "https://notclaude.ai/x", "https://user@evil.com/", "https://claude.ai@evil.com/", "https://%zz"} {
		clip := &fakeClip{}
		setup(t, clip)
		fake := &run.Fake{}
		var out strings.Builder
		g := env(map[string]string{EnvAccount: "w", EnvMode: "incognito"})
		if code := HandleOpenURL(u, g, fake, &out); code != 1 {
			t.Errorf("%s: code %d", u, code)
		}
		if len(clip.got) != 0 || len(fake.Calls) != 0 || out.Len() == 0 {
			t.Errorf("%s: acted: %v %v", u, clip.got, fake.Calls)
		}
	}
}

func TestAllowedHosts(t *testing.T) {
	for _, u := range []string{"https://claude.com/a", "https://console.anthropic.com/a", "https://api.anthropic.com/a", "https://www.claude.ai/a", "https://platform.claude.com/a"} {
		setup(t, &fakeClip{})
		if code := HandleOpenURL(u, env(map[string]string{EnvAccount: "w"}), &run.Fake{}, &strings.Builder{}); code != 0 {
			t.Errorf("%s refused", u)
		}
	}
}
