package dash

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/pratikbhadane24/multiclaude/internal/status"
)

const (
	barWidth       = 8
	narrowBarWidth = 4
	// wideWidth is the terminal width from which full-size bars are drawn.
	wideWidth = 100
	fullPct   = 100.0
	idleText  = "idle"
	// noLimitBar replaces the percentage of a window the plan does not have.
	noLimitBar = "—"
	// A bar is heavy filled cells, an optional half cell, then a light track,
	// so it still reads without color.
	barFull   = "━"
	barHalf   = "╸"
	barEmpty  = "─"
	naText    = "n/a"
	secPerMin = 60
	ellipsis  = "…"
)

// Colors adapt to light and dark terminals; lipgloss downgrades them on
// terminals without true color.
var (
	colorOK     = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#4ADE80"}
	colorWarn   = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"}
	colorBad    = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	colorDim    = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#8B93A1"}
	colorAccent = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#A78BFA"}
	colorInfo   = lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#67E8F9"}
	colorGold   = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FCD34D"}
	colorBorder = lipgloss.AdaptiveColor{Light: "#C4C9D2", Dark: "#4B5263"}
	colorTrack  = lipgloss.AdaptiveColor{Light: "#D1D5DB", Dark: "#3F4654"}
	colorSelBg  = lipgloss.AdaptiveColor{Light: "#EDE9FE", Dark: "#2E2A47"}

	// usageRamp colors a usage bar by position: calm on the left, hot on the right.
	usageRamp = []lipgloss.Color{"#34D399", "#4ADE80", "#A3E635", "#FACC15", "#FBBF24", "#FB923C", "#F97316", "#EF4444"}
	// logoRamp is cycled over the logo letters while the dashboard is busy.
	logoRamp = []lipgloss.Color{"#A78BFA", "#818CF8", "#60A5FA", "#38BDF8", "#22D3EE", "#38BDF8", "#60A5FA", "#818CF8"}
)

var (
	styleOK     = lipgloss.NewStyle().Foreground(colorOK)
	styleWarn   = lipgloss.NewStyle().Foreground(colorWarn)
	styleBad    = lipgloss.NewStyle().Foreground(colorBad)
	styleDim    = lipgloss.NewStyle().Foreground(colorDim)
	styleBold   = lipgloss.NewStyle().Bold(true)
	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(colorInfo)
	styleAccent = lipgloss.NewStyle().Foreground(colorAccent)
	styleBorder = lipgloss.NewStyle().Foreground(colorBorder)
	styleSel    = lipgloss.NewStyle().Background(colorSelBg)
	styleBadge  = lipgloss.NewStyle().Bold(true).Foreground(colorBad)
	styleKey    = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	styleGold   = lipgloss.NewStyle().Bold(true).Foreground(colorGold)
	plain       = lipgloss.NewStyle()
)

// tone is the mood of a state: it picks the color and the glyph.
type tone int

const (
	toneOK tone = iota
	toneWarn
	toneBad
	toneDim
)

func (t tone) style() lipgloss.Style {
	return [...]lipgloss.Style{styleOK, styleWarn, styleBad, styleDim}[t]
}

func (t tone) glyph() string {
	return [...]string{"●", "▲", "✖", "○"}[t]
}

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

// rampColor is the usage color at pos (0 = empty end, 1 = full end).
func rampColor(pos float64) lipgloss.TerminalColor {
	i := int(pos * float64(len(usageRamp)))
	return usageRamp[min(max(i, 0), len(usageRamp)-1)]
}

func solid(c lipgloss.TerminalColor) func(float64) lipgloss.TerminalColor {
	return func(float64) lipgloss.TerminalColor { return c }
}

// meter draws w cells with pct percent of them filled, to half-cell
// precision; color picks each filled cell's color by its position.
func meter(pct float64, w int, color func(pos float64) lipgloss.TerminalColor, base lipgloss.Style) string {
	halves := int(math.Round(min(max(pct, 0), fullPct) / fullPct * float64(w) * 2))
	full, half := halves/2, halves%2
	var b strings.Builder
	for i := range full + half {
		cell := barFull
		if i == full {
			cell = barHalf
		}
		b.WriteString(base.Foreground(color(float64(i) / float64(w))).Render(cell))
	}
	if rest := w - full - half; rest > 0 {
		b.WriteString(base.Foreground(colorTrack).Render(strings.Repeat(barEmpty, rest)))
	}
	return b.String()
}

// bar renders "━━━━──── 50%" for a window view, w cells of bar; an idle
// window is an empty bar with "idle".
func bar(v status.WindowView, w int) string {
	p, _ := usedFraction(v)
	return barShown(v, w, p, plain)
}

// barShown is bar with the fill drawn at shown percent (the animated value);
// the text always says the real figure. base carries the row background.
func barShown(v status.WindowView, w int, shown float64, base lipgloss.Style) string {
	text, color := v.Used, rampColor
	if _, ok := usedFraction(v); !ok {
		shown = 0
		switch {
		case v.NoLimit:
			text = noLimitBar
		case v.Idle:
			text = idleText
		}
	}
	if v.Limited {
		color = solid(colorBad)
	}
	return meter(shown, w, color, base) + windowStyle(v).Inherit(base).Render(" "+text)
}

// stateTone is the short STATE cell: the shared label without its detail
// (arrow hints, error text); an idle account says so. Usage kept from an
// earlier fetch shows "ok · stale 4m" as a warning.
func stateTone(r status.Row) (string, tone) {
	label := status.StateLabel(r)
	for _, cut := range []string{" →", ":", " ·"} {
		label, _, _ = strings.Cut(label, cut)
	}
	short, _, _ := strings.Cut(label, " (")
	switch {
	case r.SameAs != "":
		return "= " + r.SameAs, toneDim
	case status.StaleSuffix(r) != "":
		return short + status.StaleSuffix(r), toneWarn
	case r.Usage == nil && r.RateLimited:
		return short, toneBad
	case strings.HasPrefix(label, "limited"), strings.HasPrefix(label, "api error"):
		return label, toneBad
	case strings.HasPrefix(label, "low"), label == "logged out":
		return label, toneWarn
	case label == "ok" && idleFiveHour(r):
		return idleText, toneDim
	}
	return label, toneOK
}

func stateText(r status.Row) (string, lipgloss.Style) {
	text, t := stateTone(r)
	return text, t.style()
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

// pad truncates or right-pads s (which may be styled) to exactly w cells.
func pad(s string, w int) string { return padOn(s, w, plain) }

// padOn is pad with the padding drawn in base (a row background).
func padOn(s string, w int, base lipgloss.Style) string {
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, ellipsis)
	}
	if gap := w - lipgloss.Width(s); gap > 0 {
		s += base.Render(strings.Repeat(" ", gap))
	}
	return s
}

// idleFiveHour reports an account with no five-hour window running (a plan
// without one is not idle).
func idleFiveHour(r status.Row) bool {
	v := status.Window5h(r, time.Time{})
	return v.Idle && !v.NoLimit
}
