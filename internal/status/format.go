package status

import (
	"fmt"
	"time"
)

const (
	naText       = "n/a"
	hoursPerDay  = 24
	kilo         = 1_000.0
	mega         = 1_000_000.0
	giga         = 1_000_000_000.0
	minutesPerHr = 60
)

func fmtPct(p *float64) string {
	if p == nil {
		return naText
	}
	return fmt.Sprintf("%.0f%%", *p)
}

func fmtLeft(p *float64) string {
	if p == nil {
		return naText
	}
	return fmt.Sprintf("%.0f%%", max(fullPct-*p, 0))
}

// fmtDuration renders 45m, 1h12m or 3d4h; negatives clamp to 0m.
func fmtDuration(d time.Duration) string {
	d = max(d, 0)
	hours := int(d.Hours())
	mins := int(d.Minutes()) % minutesPerHr
	switch {
	case hours >= hoursPerDay:
		return fmt.Sprintf("%dd%dh", hours/hoursPerDay, hours%hoursPerDay)
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func fmtReset(t *time.Time, now time.Time) string {
	if t == nil {
		return naText
	}
	return fmtDuration(t.Sub(now))
}

func humanize(n int64) string {
	v := float64(n)
	switch {
	case v >= giga:
		return fmt.Sprintf("%.1fB", v/giga)
	case v >= mega:
		return fmt.Sprintf("%.1fM", v/mega)
	case v >= kilo:
		return fmt.Sprintf("%.1fk", v/kilo)
	}
	return fmt.Sprintf("%d", n)
}
