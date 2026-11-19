package status

import "time"

// FormatDuration renders d like the status table does (45m, 1h12m, 3d4h).
func FormatDuration(d time.Duration) string { return fmtDuration(d) }

// FormatPct renders a used percentage like the status table ("n/a" for nil).
func FormatPct(p *float64) string { return fmtPct(p) }
