package dash

import (
	"strings"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/doctor"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/status"
)

const argsConfigError = "launch args: config error — run ccm config show"

const mainDirText = "~/.claude (main)"

// healthGroups maps doctor check names to the short labels in the details pane.
var healthGroups = []struct {
	label string
	match func(name string) bool
}{
	{"links", func(n string) bool { return strings.HasPrefix(n, "link ") || n == "shared links" }},
	{"MCP", func(n string) bool { return n == "MCP drift" }},
	{"keychain", func(n string) bool { return n == "keychain" }},
	{"T3", func(n string) bool { return n == "T3 instance" }},
}

func (m Model) selectedRow() (status.Row, bool) {
	if m.selected < 0 || m.selected >= len(m.snap.Rows) {
		return status.Row{}, false
	}
	return m.snap.Rows[m.selected], true
}

func (m Model) accountDir(name string) string {
	acct, ok := registry.Find(m.snap.Accounts, name)
	if !ok || acct.IsMain() || acct.Dir == "" {
		return mainDirText
	}
	return acct.Dir
}

// detailLines describes the selected account.
func (m Model) detailLines() []string {
	r, ok := m.selectedRow()
	if !ok {
		return []string{styleDim.Render("No accounts yet. Press a to add one.")}
	}
	now := m.deps.Now()
	state, st := stateText(r)
	if full := status.StateLabel(r); full != state && r.SameAs == "" && state != idleText {
		state = full
	}
	if note := status.FetchNote(r); note != "" {
		state += sep + note
	}
	title := []string{styleHeader.Render(r.Account), r.Email, r.Plan, m.accountDir(r.Account)}
	return []string{
		joinNonEmpty(title),
		styleDim.Render("state ") + st.Render(state) + sep + styleDim.Render("refreshed "+age(now, m.lastRefresh)+" ago"),
		styleDim.Render("5h ") + windowText(status.Window5h(r, now), resetAt(r, true), now) + styleDim.Render(sep+"7d ") + windowText(status.Window7d(r, now), resetAt(r, false), now),
		m.bestDetail(r),
		m.todayDetail(),
		m.argsLine(r.Account),
		styleDim.Render("health ") + m.healthText(r.Account),
	}
}

func joinNonEmpty(parts []string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

func resetAt(r status.Row, five bool) *time.Time {
	if r.Usage == nil {
		return nil
	}
	if five {
		return r.Usage.FiveHour.ResetsAt
	}
	return r.Usage.SevenDay.ResetsAt
}

func windowText(v status.WindowView, reset *time.Time, now time.Time) string {
	if v.NoLimit {
		return styleDim.Render("no limit on this plan")
	}
	if v.Idle {
		return styleDim.Render("idle (no window running)")
	}
	return windowStyle(v).Render(v.Used+" used") + ", resets " + resetIn(reset, now)
}

// argsLine is the details line for the launch args; a config that cannot be
// read says so instead of showing nothing.
func (m Model) argsLine(name string) string {
	args, err := m.argsFor(name)
	if err != nil {
		return styleBad.Render(argsConfigError)
	}
	return styleDim.Render("launch args ") + argsText(args)
}

// todayDetail is today's combined token use: history is shared by every
// account, so there is no per-account figure.
func (m Model) todayDetail() string {
	const label = "today (all accounts) "
	if m.snap.TodayErr != "" {
		return styleDim.Render(label) + styleBad.Render("error: "+m.snap.TodayErr)
	}
	t := m.snap.Today
	return styleDim.Render(label) + "in " + status.FormatTokens(t.Input) + sep + "out " + status.FormatTokens(t.Output) +
		sep + "cache read " + status.FormatTokens(t.CacheRead) + sep + "cache write " + status.FormatTokens(t.CacheWrite)
}

func argsText(args []string) string {
	if len(args) == 0 {
		return styleDim.Render("(none)")
	}
	text := strings.Join(args, " ")
	if strings.Contains(text, skipPermsArg) {
		return styleBadge.Render(text)
	}
	return text
}

// healthText summarises the last doctor run for one account.
func (m Model) healthText(name string) string {
	if m.doctorAt.IsZero() {
		return styleDim.Render("not checked yet (press d)")
	}
	parts := make([]string, 0, len(healthGroups)+1)
	for _, g := range healthGroups {
		if text, ok := groupHealth(m.doctorChecks, name, g.match); ok {
			parts = append(parts, g.label+" "+text)
		}
	}
	parts = append(parts, styleDim.Render("doctor "+age(m.deps.Now(), m.doctorAt)+" ago"))
	return strings.Join(parts, sep)
}

// groupHealth returns "ok" or the first problem among an account's checks.
func groupHealth(checks []doctor.Check, name string, match func(string) bool) (string, bool) {
	found := false
	for _, c := range checks {
		if c.Account != name || !match(c.Name) {
			continue
		}
		found = true
		if c.Status == doctor.StatusWarn || c.Status == doctor.StatusFail {
			return styleWarn.Render(c.Detail), true
		}
	}
	return styleOK.Render("ok"), found
}

// bestDetail says which account ★ marks and why ("★ best (this one): 52%
// weekly left, resets in 7h40m").
func (m Model) bestDetail(r status.Row) string {
	best, score, ok := m.bestScored()
	if !ok {
		return styleDim.Render(bestText+" best ") + styleWarn.Render("none free")
	}
	who := best.Account
	if who == r.Account {
		who = "this one"
	}
	return styleGold.Render(bestText) + styleDim.Render(" best ") + who + styleDim.Render(sep+score.Reason())
}
