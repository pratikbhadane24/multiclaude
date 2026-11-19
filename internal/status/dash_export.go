package status

// FormatTokens renders a token count like the status table (999, 34.5k, 1.2M).
func FormatTokens(n int64) string { return humanize(n) }

// LimitedWindow returns "5h" or "7d" when that window of a usable account is
// fully used, else "".
func LimitedWindow(r Row) string { return limitedWindow(r) }
