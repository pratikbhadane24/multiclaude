package status

import (
	"fmt"
	"math"
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

// humanize renders 999, 34.5k, 1.2M, 1.0B, promoting a unit when rounding
// would otherwise print 1000.0 of the smaller one.
func humanize(n int64) string {
	units := []struct {
		div    float64
		suffix string
	}{{kilo, "k"}, {mega, "M"}, {giga, "B"}}
	v := float64(n)
	if v < kilo {
		return fmt.Sprintf("%d", n)
	}
	idx := 0
	for idx < len(units)-1 && math.Round(v/units[idx].div*10)/10 >= kilo {
		idx++
	}
	return fmt.Sprintf("%.1f%s", v/units[idx].div, units[idx].suffix)
}
