package dash

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"multiclaude/internal/status"
)

const (
	barWidth       = 8
	narrowBarWidth = 4
	// wideWidth is the terminal width from which full-size bars are drawn.
	wideWidth = 100
	fullPct   = 100.0
	idleText  = "idle"
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

// windowStyle colors a window: red when limited, yellow when low, dim when
// idle or unavailable, else green.
func windowStyle(v status.WindowView) lipgloss.Style {
	switch {
	case v.Limited:
		return styleBad
	case v.Low:
		return styleWarn
	case v.Idle || v.Used == naText:
		return styleDim
	}
	return styleOK
}

// usedFraction parses the view's "42%" text; ok is false for idle or n/a.
func usedFraction(v status.WindowView) (float64, bool) {
	if v.Idle {
		return 0, false
	}
	p, err := strconv.ParseFloat(strings.TrimSuffix(v.Used, "%"), 64)
	return p, err == nil
}

// bar renders "████░░░░ 50%" for a window view, w cells of bar; an idle
// window is an empty bar with "idle".
func bar(v status.WindowView, w int) string {
	p, ok := usedFraction(v)
	if !ok {
		text := v.Used
		if v.Idle {
			text = idleText
		}
		return strings.Repeat(barEmpty, w) + " " + text
	}
	used := math.Min(math.Max(p, 0), fullPct)
	filled := int(math.Round(used / fullPct * float64(w)))
	return strings.Repeat(barFull, filled) + strings.Repeat(barEmpty, w-filled) + " " + v.Used
}

// stateText is the short STATE cell: the shared label without its detail
// (arrow hints, error text); an idle account says so. Usage kept from an
// earlier fetch shows "ok · stale 4m" in yellow.
func stateText(r status.Row) (string, lipgloss.Style) {
	label := status.StateLabel(r)
	for _, cut := range []string{" →", ":", " ·"} {
		label, _, _ = strings.Cut(label, cut)
	}
	short, _, _ := strings.Cut(label, " (")
	switch {
	case r.SameAs != "":
		return "= " + r.SameAs, styleDim
	case status.StaleSuffix(r) != "":
		return short + status.StaleSuffix(r), styleWarn
	case r.Usage == nil && r.RateLimited:
		return short, styleBad
	case strings.HasPrefix(label, "limited"), strings.HasPrefix(label, "api error"):
		return label, styleBad
	case strings.HasPrefix(label, "low"), label == "logged out":
		return label, styleWarn
	case label == "ok" && status.Window5h(r, time.Time{}).Idle:
		return idleText, styleDim
	}
	return label, styleOK
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
