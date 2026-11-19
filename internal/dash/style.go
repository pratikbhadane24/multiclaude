package dash

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"multiclaude/internal/status"
	"multiclaude/internal/usage"
)

const (
	barWidth       = 8
	narrowBarWidth = 4
	// wideWidth is the terminal width from which full-size bars are drawn.
	wideWidth = 100
	fullPct   = 100.0
	warnPct   = 70.0
	dangerPct = 90.0
	barFull   = "█"
	barEmpty  = "░"
	naText    = "n/a"
	secPerMin = 60
)

var (
	styleOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleBad    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleDim    = lipgloss.NewStyle().Faint(true)
	styleBold   = lipgloss.NewStyle().Bold(true)
	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleSel    = lipgloss.NewStyle().Reverse(true)
	styleBadge  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
	styleKey    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	plain       = lipgloss.NewStyle()
)

// usageStyle colors a used percentage.
func usageStyle(p *float64) lipgloss.Style {
	switch {
	case p == nil:
		return styleDim
	case *p >= dangerPct:
		return styleBad
	case *p >= warnPct:
		return styleWarn
	}
	return styleOK
}

// bar renders "████░░░░ 50%" for a used percentage, w cells of bar.
func bar(p *float64, w int) string {
	if p == nil {
		return strings.Repeat(barEmpty, w) + " " + naText
	}
	used := math.Min(math.Max(*p, 0), fullPct)
	filled := int(math.Round(used / fullPct * float64(w)))
	return strings.Repeat(barFull, filled) + strings.Repeat(barEmpty, w-filled) + " " + status.FormatPct(p)
}

// stateLabel returns the short state text and its color.
func stateLabel(r status.Row) (string, lipgloss.Style) {
	if lim := status.LimitedWindow(r); lim != "" {
		return "limited " + lim, styleBad
	}
	switch r.State {
	case usage.StateOK, usage.StateExpiredRefreshed:
		return okLabel(r)
	case usage.StateLoggedOut:
		return "logged out", styleWarn
	}
	return "api error", styleBad
}

// okLabel refines a usable account: low (a window ≥ 90% used), idle (no
// 5-hour window running), else ok.
func okLabel(r status.Row) (string, lipgloss.Style) {
	if r.Usage == nil {
		return "ok", styleOK
	}
	for _, w := range []usage.Window{r.Usage.FiveHour, r.Usage.SevenDay} {
		if w.UsedPct != nil && *w.UsedPct >= dangerPct {
			return "low", styleWarn
		}
	}
	if r.Usage.FiveHour.ResetsAt == nil {
		return "idle", styleDim
	}
	return "ok", styleOK
}

// age renders how long ago t was: 12s, 3m, 1h4m.
func age(now, t time.Time) string {
	d := max(now.Sub(t), 0)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return status.FormatDuration(d)
}

// resetIn renders "in 1h12m (14:30)" or n/a.
func resetIn(t *time.Time, now time.Time) string {
	if t == nil {
		return naText
	}
	return "in " + status.FormatDuration(t.Sub(now)) + " (" + t.Local().Format("Mon 15:04") + ")"
}

// pad truncates or right-pads s to exactly w cells.
func pad(s string, w int) string {
	if lipgloss.Width(s) > w {
		r := []rune(s)
		for len(r) > 0 && lipgloss.Width(string(r))+1 > w {
			r = r[:len(r)-1]
		}
		s = string(r) + "…"
	}
	return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
}
