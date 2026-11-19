package status

import (
	"strings"
	"testing"

	"multiclaude/internal/usage"
)

func TestRenderOpusSonnetColumnsAndLimited(t *testing.T) {
	a := ok("a", f(100), f(10), nil)
	a.Usage.SevenDayOpus = &usage.Window{UsedPct: f(80)}
	a.Usage.SevenDaySonnet = &usage.Window{UsedPct: f(35)}
	b := ok("b", f(10), f(100), nil)
	rows := []Row{{AccountUsage: a}, {AccountUsage: b}}
	out := Render(rows, usage.Aggregate([]usage.AccountUsage{a, b}), now)
	hdr := strings.Join(strings.Fields(strings.Split(out, "\n")[0]), " ")
	if !strings.Contains(hdr, "7D RESET 7D OPUS 7D SONNET TODAY TOK STATE") {
		t.Fatalf("header %q", hdr)
	}
	for _, want := range []string{"80%", "35%", "limited (5h)", "limited (7d)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderLimitedWithoutOpusKeepsHeader(t *testing.T) {
	a := ok("a", f(100), f(10), nil)
	out := Render([]Row{{AccountUsage: a}}, usage.Aggregate([]usage.AccountUsage{a}), now)
	if strings.Contains(out, "OPUS") || !strings.Contains(out, "limited (5h)") {
		t.Fatalf("out:\n%s", out)
	}
}

func TestRenderJSONOpusSonnetLimited(t *testing.T) {
	a := ok("a", f(100), f(10), nil)
	a.Usage.SevenDayOpus = &usage.Window{UsedPct: f(80)}
	b, err := RenderJSON([]Row{{AccountUsage: a}}, usage.Aggregate([]usage.AccountUsage{a}))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"seven_day_opus":{"used_pct":80`, `"seven_day_sonnet":null`, `"limited":true`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %s", want, s)
		}
	}
}
