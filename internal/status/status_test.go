package status

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"multiclaude/internal/links"
	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
	"multiclaude/internal/tokens"
	"multiclaude/internal/usage"
)

var now = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func f(v float64) *float64 { return &v }

func ok(name string, five, seven *float64, reset *time.Time) usage.AccountUsage {
	return usage.AccountUsage{Account: name, Email: name + "@x.io", Plan: "max", State: usage.StateOK,
		Usage: &usage.Usage{FiveHour: usage.Window{UsedPct: five, ResetsAt: reset}, SevenDay: usage.Window{UsedPct: seven}}}
}

func TestCollectConcurrentOrderedAndWarnings(t *testing.T) {
	accts := []registry.Account{{Name: "main"}, {Name: "a", Dir: "/d/a"}, {Name: "b", Dir: "/d/b"}}
	var mu sync.Mutex
	var dirs []string
	d := Deps{
		Accounts: accts,
		Paths:    paths.Paths{ClaudeDir: "/home/.claude"},
		Now:      func() time.Time { return now },
		Fetch: func(_ context.Context, a registry.Account) usage.AccountUsage {
			time.Sleep(100 * time.Millisecond)
			return ok(a.Name, f(10), f(10), nil)
		},
		Today: func(dir string, _ time.Time) (tokens.Tokens, error) {
			mu.Lock()
			dirs = append(dirs, dir)
			mu.Unlock()
			if strings.Contains(dir, "/d/b") {
				return tokens.Tokens{}, context.DeadlineExceeded
			}
			return tokens.Tokens{Input: 1, Output: 2}, nil
		},
		Inspect: func(_ paths.Paths, dir string) ([]links.Item, error) {
			if dir == "/d/a" {
				return []links.Item{{Name: "rules", State: links.StateReplaced}, {Name: "hooks", State: links.StateMissing}}, nil
			}
			return nil, nil
		},
	}
	start := time.Now()
	rows, c := Collect(context.Background(), d)
	if el := time.Since(start); el > 250*time.Millisecond {
		t.Fatalf("not concurrent: %v", el)
	}
	if len(rows) != 3 || rows[0].Account != "main" || rows[1].Account != "a" || rows[2].Account != "b" {
		t.Fatalf("order %+v", rows)
	}
	if c.Counted != 3 {
		t.Fatalf("combined %+v", c)
	}
	if len(rows[1].LinkWarnings) != 1 || rows[1].LinkWarnings[0] != "rules" {
		t.Fatalf("warnings %+v", rows[1].LinkWarnings)
	}
	if rows[2].TodayErr == "" || rows[0].Today.Input != 1 {
		t.Fatalf("today %+v", rows)
	}
	found := map[string]bool{}
	for _, x := range dirs {
		found[x] = true
	}
	if !found["/home/.claude/projects"] || !found["/d/a/projects"] {
		t.Fatalf("dirs %v", dirs)
	}
}

func TestRender(t *testing.T) {
	reset := now.Add(72 * time.Minute)
	rows := []Row{
		{AccountUsage: ok("a", f(40), f(10), nil), Today: tokens.Tokens{Input: 1_000_000, Output: 200_000}, LinkWarnings: []string{"rules"}},
		{AccountUsage: ok("b", f(100), f(50), &reset), Today: tokens.Tokens{Input: 34_500}},
		{AccountUsage: usage.AccountUsage{Account: "x", State: usage.StateLoggedOut}},
	}
	c := usage.Aggregate([]usage.AccountUsage{rows[0].AccountUsage, rows[1].AccountUsage, rows[2].AccountUsage})
	out := Render(rows, c, now)
	for _, want := range []string{
		"ACCOUNT", "TODAY TOK", "5H USED", "7D RESET", "40%", "60%", "100%", "0%", "1.2M", "34.5k", "1h12m", "n/a",
		"logged out → ccm login x",
		"ALL  5h left 0.6/2 acct (30%) · 7d left 1.4/2 (70%) · available now 1 · next reset in 1h12m · excluded: x",
		"⚠ a: rules is no longer shared — run ccm doctor",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	hdr := strings.Fields(strings.Split(out, "\n")[0])
	if strings.Join(hdr, " ") != "ACCOUNT EMAIL PLAN 5H USED 5H LEFT 5H RESET 7D USED 7D LEFT 7D RESET TODAY TOK STATE" {
		t.Fatalf("header %v", hdr)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[time.Duration]string{45 * time.Minute: "45m", 72 * time.Minute: "1h12m", 76 * time.Hour: "3d4h", -time.Minute: "0m"}
	for d, want := range cases {
		if got := fmtDuration(d); got != want {
			t.Errorf("%v: %q != %q", d, got, want)
		}
	}
}

func TestRenderJSONNoSecrets(t *testing.T) {
	rows := []Row{{AccountUsage: ok("a", f(1), nil, nil), Today: tokens.Tokens{Input: 5}}}
	b, err := RenderJSON(rows, usage.Aggregate([]usage.AccountUsage{rows[0].AccountUsage}))
	if err != nil || !json.Valid(b) {
		t.Fatalf("%v %s", err, b)
	}
	s := strings.ToLower(string(b))
	if strings.Contains(s, "accesstoken") || !strings.Contains(s, `"account":"a"`) {
		t.Fatalf("%s", b)
	}
}

func TestPickBest(t *testing.T) {
	list := []usage.AccountUsage{ok("a", f(90), f(20), nil), ok("b", f(30), f(60), nil), ok("c", f(100), nil, nil)}
	got, found := PickBest(list)
	if !found || got.Account != "b" {
		t.Fatalf("%v %v", got, found)
	}
	_, found = PickBest([]usage.AccountUsage{ok("c", f(100), f(5), nil), {Account: "d", State: usage.StateAPIError}})
	if found {
		t.Fatal("expected none")
	}
}

func TestWatcherRetriesErrorsAfterTenMinutes(t *testing.T) {
	clock := now
	var mu sync.Mutex
	calls := map[string]int{}
	d := Deps{
		Accounts: []registry.Account{{Name: "good"}, {Name: "bad"}},
		Now:      func() time.Time { return clock },
		Fetch: func(_ context.Context, a registry.Account) usage.AccountUsage {
			mu.Lock()
			calls[a.Name]++
			mu.Unlock()
			if a.Name == "bad" {
				return usage.AccountUsage{Account: "bad", State: usage.StateAPIError, Err: "boom"}
			}
			return ok("good", f(1), f(1), nil)
		},
	}
	w := NewWatcher(d.Now)
	step := func() []Row { rows, _ := w.Step(context.Background(), d); return rows }
	step()
	clock = clock.Add(2 * time.Minute)
	rows := step()
	if calls["good"] != 2 || calls["bad"] != 1 {
		t.Fatalf("calls %v", calls)
	}
	if rows[1].State != usage.StateAPIError || rows[1].Err != "boom" {
		t.Fatalf("cached row %+v", rows[1])
	}
	clock = clock.Add(8 * time.Minute)
	step()
	if calls["bad"] != 2 {
		t.Fatalf("retry after 10m: %v", calls)
	}
}
