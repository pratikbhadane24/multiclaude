package dash

import (
	"context"
	"os/exec"
	"time"

	"multiclaude/internal/doctor"
	"multiclaude/internal/registry"
	"multiclaude/internal/status"
	"multiclaude/internal/tokens"
	"multiclaude/internal/usage"
)

const demoEcho = "/bin/echo"

// Demo returns d with every side effect replaced by canned data: no
// Keychain, network, osascript or claude is ever touched.
func Demo(d Deps) Deps {
	d = withDefaults(d)
	now := d.Now
	d.Collect = func(_ context.Context, show bool) Snapshot { return demoSnapshot(now(), show) }
	d.Launch = func(name string, _ []string) (*exec.Cmd, string, error) {
		return exec.Command(demoEcho, "demo: would run claude as", name), "", nil
	}
	d.Self = func(args ...string) *exec.Cmd {
		return exec.Command(demoEcho, append([]string{"demo: would run ccm"}, args...)...)
	}
	d.LatestSession = func() (string, error) { return "demo-session", nil }
	d.Run = func(context.Context, string, []string) error { return nil }
	d.Doctor = func(context.Context) []doctor.Check { return demoChecks() }
	d.Sync = func(context.Context) (string, error) { return "work: up to date\nside: up to date\n", nil }
	d.T3Sync = func(context.Context) (string, error) { return "T3 Code already up to date.\n", nil }
	d.ArgsFor = func(name string) ([]string, error) {
		if name == "main" {
			return []string{"--dangerously-skip-permissions"}, nil
		}
		return nil, nil
	}
	return d
}

func demoPtr(v float64) *float64 { return &v }

func demoWindow(used float64, now time.Time, in time.Duration) usage.Window {
	t := now.Add(in)
	return usage.Window{UsedPct: demoPtr(used), ResetsAt: &t}
}

func demoSnapshot(now time.Time, showDuplicates bool) Snapshot {
	main := demoRow("main", "me@example.com", "max", usage.StateOK, &usage.Usage{
		FiveHour: demoWindow(42, now, 2*time.Hour+10*time.Minute),
		SevenDay: demoWindow(63, now, 3*24*time.Hour),
	})
	admin := demoRow("admin", "me@example.com", "max", usage.StateOK, main.Usage)
	rows := []status.Row{main}
	if showDuplicates {
		admin.SameAs = "main"
		rows = append(rows, admin)
	} else {
		rows[0].Aliases = []string{"admin"}
	}
	rows = append(rows,
		demoRow("work", "me@work.example", "max", usage.StateOK, &usage.Usage{
			FiveHour: demoWindow(8, now, 4*time.Hour),
			SevenDay: demoWindow(21, now, 5*24*time.Hour),
		}),
		demoRow("fresh", "fresh@example.com", "pro", usage.StateOK, &usage.Usage{
			FiveHour: usage.Window{},
			SevenDay: demoWindow(4, now, 6*24*time.Hour),
		}),
		demoRow("tight", "tight@example.com", "pro", usage.StateOK, &usage.Usage{
			FiveHour: demoWindow(97, now, 35*time.Minute),
			SevenDay: demoWindow(71, now, 2*24*time.Hour),
		}),
		demoRow("side", "side@example.com", "pro", usage.StateOK, &usage.Usage{
			FiveHour: demoWindow(100, now, 47*time.Minute),
			SevenDay: demoWindow(92, now, 26*time.Hour),
		}),
		demoRow("old", "old@example.com", "", usage.StateLoggedOut, nil),
	)
	accts := []registry.Account{
		{Name: "main"},
		{Name: "admin", Dir: "/Users/demo/.claude-accounts/admin"},
		{Name: "work", Dir: "/Users/demo/.claude-accounts/work"},
		{Name: "fresh", Dir: "/Users/demo/.claude-accounts/fresh"},
		{Name: "tight", Dir: "/Users/demo/.claude-accounts/tight"},
		{Name: "side", Dir: "/Users/demo/.claude-accounts/side"},
		{Name: "old", Dir: "/Users/demo/.claude-accounts/old"},
	}
	list := make([]usage.AccountUsage, 0, len(rows)+1)
	for _, r := range rows {
		if r.SameAs == "" {
			list = append(list, r.AccountUsage)
		}
	}
	combined := usage.Aggregate(list)
	combined.Today = tokens.Tokens{Input: 20_500, Output: 61_900, CacheRead: 3_850_000, CacheWrite: 241_000}
	return Snapshot{Rows: rows, Combined: combined, Accounts: accts, Today: combined.Today}
}

func demoRow(name, email, plan string, st usage.State, u *usage.Usage) status.Row {
	return status.Row{AccountUsage: usage.AccountUsage{Account: name, Email: email, Plan: plan, State: st, Usage: u}}
}

func demoChecks() []doctor.Check {
	return []doctor.Check{
		{Name: "real claude", Status: doctor.StatusOK},
		{Account: "work", Name: "link CLAUDE.md", Status: doctor.StatusOK},
		{Account: "work", Name: "MCP drift", Status: doctor.StatusOK},
		{Account: "work", Name: "keychain", Status: doctor.StatusOK},
		{Account: "old", Name: "keychain", Status: doctor.StatusWarn, Detail: "logged out → ccm login old"},
		{Account: "side", Name: "T3 instance", Status: doctor.StatusWarn, Detail: "not in T3 → ccm t3-sync"},
	}
}
