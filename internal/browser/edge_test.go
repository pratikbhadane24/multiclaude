package browser

import (
	"strings"
	"testing"

	"multiclaude/internal/run"
)

func TestHostAllowedEdgeCases(t *testing.T) {
	if !hostAllowed("https://CLAUDE.AI:443/x") {
		t.Error("uppercase host with port should be allowed")
	}
	if !hostAllowed("HTTPS://claude.ai/x") {
		t.Error("uppercase scheme should be allowed")
	}
	if hostAllowed(`https://evil.com\.claude.ai/`) {
		t.Error("backslash host trick should be refused")
	}
}

func TestRefusalMessageNamesHost(t *testing.T) {
	var out strings.Builder
	code := HandleOpenURL("https://evil.example/x", env(map[string]string{EnvAccount: "w"}), &run.Fake{}, &out)
	want := "ccm: refusing to open non-Claude URL host \"evil.example\"\n"
	if code != 1 || out.String() != want {
		t.Fatalf("code %d out %q", code, out.String())
	}
}
