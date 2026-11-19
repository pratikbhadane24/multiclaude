package status

import "time"

// fmtWait renders a short wait or age in whole minutes, at least 1m.
func fmtWait(d time.Duration) string {
	return fmtDuration(max(d.Round(time.Minute), time.Minute))
}

// isStale reports whether r shows usage kept from an earlier fetch.
func isStale(r Row) bool { return r.Stale && r.Usage != nil }

// StaleSuffix is " · stale 4m" for a row showing usage kept from an earlier
// fetch (the latest failed or is backing off), else "".
func StaleSuffix(r Row) string {
	if !isStale(r) {
		return ""
	}
	return " · stale " + fmtWait(r.StaleFor)
}

func retrySuffix(r Row, sep string) string {
	if r.RetryIn <= 0 {
		return ""
	}
	return sep + "retry in " + fmtWait(r.RetryIn)
}

// rateLimitedLabel is the state of a rate-limited row with no usage at all.
func rateLimitedLabel(r Row) string {
	if r.RetryIn <= 0 {
		return "rate limited"
	}
	return "rate limited (retry in " + fmtWait(r.RetryIn) + ")"
}

// FetchNote explains why a row's usage is stale: "rate limited, retry in 7m"
// or "last fetch failed (timeout), retry in 2m"; "" for fresh rows.
func FetchNote(r Row) string {
	if !isStale(r) {
		return ""
	}
	if r.RateLimited {
		return "rate limited" + retrySuffix(r, ", ")
	}
	return "last fetch failed (" + r.Err + ")" + retrySuffix(r, ", ")
}
