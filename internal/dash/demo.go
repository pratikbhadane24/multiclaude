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
	d.Collect = func(context.Context) Snapshot { return demoSnapshot(now()) }
	d.Launch = func(name string, _ []string) (*exec.Cmd, error) {
		return exec.Command(demoEcho, "demo: would run claude as", name), nil
	}
	d.Self = func(args ...string) *exec.Cmd {
		return exec.Command(demoEcho, append([]string{"demo: would run ccm"}, args...)...)
	}
	d.LatestSession = func() (string, error) { return "demo-session", nil }
	d.Run = func(context.Context, string, []string) error { return nil }
	d.Doctor = func(context.Context) []doctor.Check { return demoChecks() }
	d.Sync = func(context.Context) (string, error) { return "work: up to date\nside: up to date\n", nil }
	d.T3Sync = func(context.Context) (string, error) { return "T3 Code already up to date.\n", nil }
	d.ArgsFor = func(name string) []string {
		if name == "main" {
			return []string{"--dangerously-skip-permissions"}
		}
		return nil
	}
	return d
}

func demoPtr(v float64) *float64 { return &v }

func demoWindow(used float64, now time.Time, in time.Duration) usage.Window {
	t := now.Add(in)
	return usage.Window{UsedPct: demoPtr(used), ResetsAt: &t}
}

func demoSnapshot(now time.Time) Snapshot {
	rows := []status.Row{
		demoRow("main", "me@example.com", "max", usage.StateOK, &usage.Usage{
			FiveHour: demoWindow(42, now, 2*time.Hour+10*time.Minute),
			SevenDay: demoWindow(63, now, 3*24*time.Hour),
		}),
		demoRow("work", "me@work.example", "max", usage.StateOK, &usage.Usage{
			FiveHour: demoWindow(8, now, 4*time.Hour),
			SevenDay: demoWindow(21, now, 5*24*time.Hour),
		}),
		demoRow("side", "side@example.com", "pro", usage.StateOK, &usage.Usage{
			FiveHour: demoWindow(100, now, 47*time.Minute),
			SevenDay: demoWindow(92, now, 26*time.Hour),
		}),
		demoRow("old", "old@example.com", "", usage.StateLoggedOut, nil),
	}
	accts := []registry.Account{
		{Name: "main"},
		{Name: "work", Dir: "/Users/demo/.claude-accounts/work"},
		{Name: "side", Dir: "/Users/demo/.claude-accounts/side"},
		{Name: "old", Dir: "/Users/demo/.claude-accounts/old"},
	}
	list := make([]usage.AccountUsage, len(rows))
	for i, r := range rows {
		list[i] = r.AccountUsage
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
