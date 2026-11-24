package status

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

func TestWindowViewIdle(t *testing.T) {
	u := ok("team", nil, f(20), nil)
	u.Plan = "team"
	r := Row{AccountUsage: u}
	v := Window5h(r, now)
	if v.Used != "0%" || v.Left != "100%" || v.Reset != "idle" || !v.Idle || v.Low || v.Limited {
		t.Fatalf("%+v", v)
	}
	if w := Window7d(r, now); w.Idle || w.Used != "20%" || w.Left != "80%" {
		t.Fatalf("%+v", w)
	}
}

func TestWindowViewMaxHasNoFiveHourLimit(t *testing.T) {
	v := Window5h(Row{AccountUsage: ok("m", nil, f(20), nil)}, now)
	if !v.NoLimit || v.Reset != NoLimitText {
		t.Fatalf("%+v", v)
	}
	if w := Window5h(Row{AccountUsage: ok("m", f(3), f(20), nil)}, now); w.NoLimit {
		t.Fatalf("reported window marked no-limit: %+v", w)
	}
}

func TestWindowViewNAForErrorRows(t *testing.T) {
	for _, st := range []usage.State{usage.StateLoggedOut, usage.StateAPIError} {
		r := Row{AccountUsage: usage.AccountUsage{Account: "x", State: st}}
		v := Window5h(r, now)
		if v.Used != "n/a" || v.Left != "n/a" || v.Reset != "n/a" || v.Idle {
			t.Fatalf("%v %+v", st, v)
		}
	}
}

func TestWindowViewLowAndLimitedThresholds(t *testing.T) {
	cases := []struct {
		used        float64
		low, limit  bool
		usedText    string
		leftText    string
		description string
	}{
		{94.9, false, false, "95%", "5%", "below"},
		{95, true, false, "95%", "5%", "at 95"},
		{99.9, true, false, "100%", "0%", "just under 100"},
		{100, false, true, "100%", "0%", "full"},
		{120, false, true, "120%", "0%", "over"},
	}
	for _, c := range cases {
		v := Window5h(Row{AccountUsage: ok("a", f(c.used), f(1), nil)}, now)
		if v.Low != c.low || v.Limited != c.limit {
			t.Errorf("%s: %+v", c.description, v)
		}
	}
}

func TestWindowViewReset(t *testing.T) {
	reset := now.Add(72 * time.Minute)
	v := Window5h(Row{AccountUsage: ok("a", f(40), f(1), &reset)}, now)
	if v.Reset != "1h12m" {
		t.Fatalf("%+v", v)
	}
	if v := Window5h(Row{AccountUsage: ok("a", f(40), f(1), nil)}, now); v.Reset != "n/a" {
		t.Fatalf("%+v", v)
	}
}

func TestStateLabel(t *testing.T) {
	cases := []struct {
		row  Row
		want string
	}{
		{Row{AccountUsage: ok("a", f(10), f(10), nil)}, "ok"},
		{Row{AccountUsage: ok("a", nil, nil, nil)}, "ok"},
		{Row{AccountUsage: ok("a", f(96), f(10), nil)}, "low (5h)"},
		{Row{AccountUsage: ok("a", f(10), f(97), nil)}, "low (7d)"},
		{Row{AccountUsage: ok("a", f(96), f(97), nil)}, "low (5h)"},
		{Row{AccountUsage: ok("a", f(96), f(100), nil)}, "limited (7d)"},
		{Row{AccountUsage: ok("a", f(100), f(97), nil)}, "limited (5h)"},
		{Row{AccountUsage: usage.AccountUsage{Account: "x", State: usage.StateLoggedOut}}, "logged out → ccm login x"},
		{Row{AccountUsage: ok("admin", f(1), f(1), nil), SameAs: "main"}, "same as main"},
	}
	for _, c := range cases {
		if got := StateLabel(c.row); got != c.want {
			t.Errorf("%q != %q", got, c.want)
		}
	}
}

func TestRenderIdleAndLowRows(t *testing.T) {
	idle, low := ok("idle", nil, f(5), nil), ok("low", f(96), f(5), nil)
	idle.Plan = "team"
	rows := []Row{{AccountUsage: idle}, {AccountUsage: low}}
	out := Render(rows, usage.Aggregate([]usage.AccountUsage{idle, low}), now)
	line := lineFor(out, "idle")
	for _, want := range []string{"0%", "100%", "idle", "ok"} {
		if !strings.Contains(line, want) {
			t.Errorf("idle row missing %q: %s", want, line)
		}
	}
	if f := strings.Fields(line); len(f) < 6 || f[3] != "0%" || f[4] != "100%" || f[5] != "idle" {
		t.Errorf("5h columns must be 0%% 100%% idle: %s", line)
	}
	if !strings.Contains(lineFor(out, "low"), "low (5h)") {
		t.Errorf("out:\n%s", out)
	}
	if !strings.Contains(out, "available now 2") {
		t.Errorf("low must stay available:\n%s", out)
	}
}

func lineFor(out, prefix string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix+" ") {
			return l
		}
	}
	return ""
}

func TestPickBestIdleHasFullHeadroom(t *testing.T) {
	got, found := PickBest([]usage.AccountUsage{ok("a", f(50), f(10), nil), ok("idle", nil, nil, nil)}, now)
	if !found || got.Account != "idle" {
		t.Fatalf("%v %v", got, found)
	}
}

func dupDeps(showAll bool) Deps {
	return Deps{
		Accounts:       []registry.Account{{Name: "main"}, {Name: "admin", Dir: "/d/admin"}, {Name: "w", Dir: "/d/w"}},
		Now:            func() time.Time { return now },
		ShowDuplicates: showAll,
		Fetch: func(_ context.Context, a registry.Account) usage.AccountUsage {
			u := ok(a.Name, f(40), f(10), nil)
			u.Email, u.AccountUUID = "admin@x.io", "uuid-admin"
			if a.Name == "w" {
				u.Email, u.AccountUUID = "w@x.io", "uuid-w"
			}
			return u
		},
	}
}

func TestCollectCollapsesDuplicateLogins(t *testing.T) {
	rows, c := Collect(context.Background(), dupDeps(false))
	if len(rows) != 2 || rows[0].Account != "main" || rows[1].Account != "w" {
		t.Fatalf("%+v", rows)
	}
	if got := rows[0].Aliases; len(got) != 1 || got[0] != "admin" {
		t.Fatalf("aliases %v", got)
	}
	if rows[0].DisplayName() != "main (= admin)" || rows[1].DisplayName() != "w" {
		t.Fatalf("names %q %q", rows[0].DisplayName(), rows[1].DisplayName())
	}
	if c.Counted != 2 || c.AvailableNow != 2 {
		t.Fatalf("combined %+v", c)
	}
}

func TestDisplayNameJoinsThreeWithComma(t *testing.T) {
	r := Row{AccountUsage: usage.AccountUsage{Account: "main"}, Aliases: []string{"a", "b"}}
	if r.DisplayName() != "main (= a, b)" {
		t.Fatalf("%q", r.DisplayName())
	}
}

func TestCollectShowDuplicatesKeepsSeparateRows(t *testing.T) {
	rows, c := Collect(context.Background(), dupDeps(true))
	if len(rows) != 3 || rows[1].Account != "admin" || rows[1].SameAs != "main" || rows[0].SameAs != "" {
		t.Fatalf("%+v", rows)
	}
	if c.Counted != 2 {
		t.Fatalf("still counted once: %+v", c)
	}
	out := Render(rows, c, now)
	if !strings.Contains(lineFor(out, "admin"), "same as main") {
		t.Fatalf("out:\n%s", out)
	}
}

func TestCollectSplitsWhenLoginsDiffer(t *testing.T) {
	d := dupDeps(false)
	inner := d.Fetch
	d.Fetch = func(ctx context.Context, a registry.Account) usage.AccountUsage {
		u := inner(ctx, a)
		if a.Name == "admin" {
			u.Email, u.AccountUUID = "elsewhere@x.io", "uuid-else"
		}
		return u
	}
	rows, c := Collect(context.Background(), d)
	if len(rows) != 3 || rows[0].DisplayName() != "main" || c.Counted != 3 {
		t.Fatalf("%+v %+v", rows, c)
	}
}

func TestPickBestSkipsHiddenDuplicate(t *testing.T) {
	m := ok("main", f(80), f(10), nil)
	m.AccountUUID = "u"
	a := ok("admin", f(80), f(10), nil)
	a.AccountUUID = "u"
	got, found := PickBest([]usage.AccountUsage{m, a}, now)
	if !found || got.Account != "main" {
		t.Fatalf("%v", got)
	}
	// The only usable account is the canonical one; the duplicate is never offered.
	lim := ok("main", f(100), f(10), nil)
	lim.AccountUUID = "u"
	if got, found := PickBest([]usage.AccountUsage{lim, a}, now); found {
		t.Fatalf("duplicate of a limited login must not be picked: %v", got)
	}
}

func TestJSONAliasesAndIdle(t *testing.T) {
	rows, c := Collect(context.Background(), dupDeps(false))
	rows[1].AccountUsage.Usage.FiveHour.UsedPct = nil
	b, err := RenderJSON(rows, c)
	if err != nil || !json.Valid(b) {
		t.Fatalf("%v %s", err, b)
	}
	var out struct {
		Accounts []struct {
			Account string   `json:"account"`
			Aliases []string `json:"aliases"`
			SameAs  string   `json:"same_as"`
			Five    struct {
				Idle bool `json:"idle"`
			} `json:"five_hour"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Accounts[0].Aliases) != 1 || out.Accounts[0].Aliases[0] != "admin" || out.Accounts[1].Aliases != nil {
		t.Fatalf("%s", b)
	}
	if !out.Accounts[1].Five.Idle || out.Accounts[0].Five.Idle {
		t.Fatalf("%s", b)
	}
	if strings.Contains(string(b), "uuid") {
		t.Fatalf("identity leaked: %s", b)
	}
}

func TestCacheWritesAliasEntriesAndIdleState(t *testing.T) {
	rows, c := Collect(context.Background(), dupDeps(false))
	cache := buildCache(rows, c, now)
	names := map[string]CacheAccount{}
	for _, a := range cache.Accounts {
		names[a.Name] = a
	}
	adm, ok := names["admin"]
	if !ok || adm.FiveHourUsed == nil || *adm.FiveHourUsed != 40 {
		t.Fatalf("alias entry missing: %+v", cache.Accounts)
	}
	if len(cache.Accounts) != 3 {
		t.Fatalf("%+v", cache.Accounts)
	}
}

func TestWindowViewZeroUsedNoResetIsIdle(t *testing.T) {
	r := Row{AccountUsage: ok("max", f(0), f(0), nil)}

	v5, v7 := Window5h(r, now), Window7d(r, now)

	if !v5.Idle || v5.Reset != "idle" || v5.Used != "0%" || !v7.Idle {
		t.Fatalf("%+v %+v", v5, v7)
	}
}

func TestWindowViewZeroUsedWithResetIsNotIdle(t *testing.T) {
	reset := now.Add(time.Hour)
	r := Row{AccountUsage: usage.AccountUsage{Account: "x", State: usage.StateOK, Usage: &usage.Usage{
		FiveHour: usage.Window{UsedPct: f(0), ResetsAt: &reset}}}}

	if v := Window5h(r, now); v.Idle || v.Reset != "1h0m" {
		t.Fatalf("%+v", v)
	}
}
