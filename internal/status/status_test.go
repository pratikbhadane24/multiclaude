package status

import (
	"context"
	"encoding/json"
	"errors"
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
	var arrive sync.WaitGroup
	arrive.Add(len(accts))
	release := make(chan struct{})
	go func() { arrive.Wait(); close(release) }()
	d := Deps{
		Accounts: accts,
		Paths:    paths.Paths{ClaudeDir: "/home/.claude"},
		Now:      func() time.Time { return now },
		Fetch: func(_ context.Context, a registry.Account) usage.AccountUsage {
			arrive.Done()
			select {
			case <-release:
			case <-time.After(5 * time.Second):
				t.Error("fetches are not concurrent")
			}
			return ok(a.Name, f(10), f(10), nil)
		},
		Today: func(dir string, _ time.Time) (tokens.Tokens, error) {
			mu.Lock()
			dirs = append(dirs, dir)
			mu.Unlock()
			return tokens.Tokens{Input: 1, Output: 2}, nil
		},
		Inspect: func(_ paths.Paths, dir string) ([]links.Item, error) {
			if dir == "/d/a" {
				return []links.Item{{Name: "rules", State: links.StateReplaced}, {Name: "hooks", State: links.StateMissing}}, nil
			}
			if dir == "/d/b" {
				return nil, errors.New("boom")
			}
			return nil, nil
		},
	}
	rows, c := Collect(context.Background(), d)
	if len(rows) != 3 || rows[0].Account != "main" || rows[1].Account != "a" || rows[2].Account != "b" {
		t.Fatalf("order %+v", rows)
	}
	if c.Counted != 3 {
		t.Fatalf("combined %+v", c)
	}
	if len(rows[1].LinkWarnings) != 1 || rows[1].LinkWarnings[0] != "rules" {
		t.Fatalf("warnings %+v", rows[1].LinkWarnings)
	}
	if rows[2].LinkErr != "boom" || rows[1].LinkErr != "" {
		t.Fatalf("link err %+v", rows)
	}
	if c.Today != (tokens.Tokens{Input: 1, Output: 2}) || c.TodayErr != "" {
		t.Fatalf("today %+v err=%q", c.Today, c.TodayErr)
	}
	if len(dirs) != 1 || dirs[0] != "/home/.claude/projects" {
		t.Fatalf("tokens must be scanned once, from main's projects: %v", dirs)
	}
}

func TestRender(t *testing.T) {
	reset := now.Add(72 * time.Minute)
	rows := []Row{
		{AccountUsage: ok("a", f(40), f(10), nil), LinkWarnings: []string{"rules"}},
		{AccountUsage: ok("b", f(100), f(50), &reset)},
		{AccountUsage: usage.AccountUsage{Account: "x", State: usage.StateLoggedOut}},
	}
	c := usage.Aggregate([]usage.AccountUsage{rows[0].AccountUsage, rows[1].AccountUsage, rows[2].AccountUsage})
	c.Today = tokens.Tokens{Input: 1_000_000, Output: 34_500, CacheRead: 2_000_000}
	out := Render(rows, c, now)
	for _, want := range []string{
		"ACCOUNT", "TODAY TOK", "5H USED", "7D RESET", "40%", "60%", "100%", "0%", "1h12m", "n/a",
		"logged out → ccm login x",
		"ALL  5h left 0.6/2 acct (30%) · 7d left 1.4/2 (70%) · available now 1 · next reset in 1h12m · excluded: x",
		"⚠ a: rules is no longer shared — run ccm doctor",
		"· today 1.0M/34.5k in/out, 2.0M cache",
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
	rows := []Row{{AccountUsage: ok("a", f(1), nil, nil)}}
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

func TestRenderErrors(t *testing.T) {
	rows := []Row{{AccountUsage: ok("a", f(1), f(1), nil), LinkErr: "y"}}
	c := usage.Aggregate([]usage.AccountUsage{rows[0].AccountUsage})
	c.TodayErr = "x"
	out := Render(rows, c, now)
	for _, want := range []string{"⚠ token scan failed", "⚠ a: link check failed — run ccm doctor"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	b, _ := RenderJSON(rows, c)
	if !strings.Contains(string(b), `"today_error":"x"`) || strings.Contains(string(b), `"today_input"`) == false {
		t.Fatalf("%s", b)
	}
	if !strings.Contains(string(b), `"link_error":"y"`) {
		t.Fatalf("%s", b)
	}
}

func TestHumanize(t *testing.T) {
	cases := map[int64]string{999: "999", 1000: "1.0k", 34_500: "34.5k", 999_949: "999.9k", 999_950: "1.0M", 1_200_000: "1.2M", 999_950_000: "1.0B"}
	for n, want := range cases {
		if got := humanize(n); got != want {
			t.Errorf("%d: %q != %q", n, got, want)
		}
	}
}

func TestCollectNilTodayInspect(t *testing.T) {
	d := Deps{
		Accounts: []registry.Account{{Name: "a", Dir: "/d/a", Email: "e@x"}},
		Now:      func() time.Time { return now },
		Fetch: func(context.Context, registry.Account) usage.AccountUsage {
			return usage.AccountUsage{State: usage.StateAPIError}
		},
	}
	rows, _ := Collect(context.Background(), d)
	if len(rows) != 1 || rows[0].Account != "a" || rows[0].Email != "e@x" || rows[0].LinkWarnings != nil {
		t.Fatalf("%+v", rows)
	}
}

func TestPickBestExpiredRefreshedAndTie(t *testing.T) {
	r := ok("r", f(50), f(50), nil)
	r.State = usage.StateExpiredRefreshed
	got, found := PickBest([]usage.AccountUsage{ok("a", f(60), f(10), nil), r})
	if !found || got.Account != "r" {
		t.Fatalf("expired-refreshed %v %v", got, found)
	}
	got, _ = PickBest([]usage.AccountUsage{ok("first", f(50), f(50), nil), ok("second", f(50), f(50), nil)})
	if got.Account != "first" {
		t.Fatalf("tie %v", got.Account)
	}
}
