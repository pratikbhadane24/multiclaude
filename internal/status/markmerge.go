package status

import (
	"time"

	"multiclaude/internal/usage"
)

// Limit-mark merge rules. A mark (MarkLimited/MarkLimitedType) stamps the
// entry's MarkedAt. Every later cache write builds its entry from the entry
// read under the cache lock and keeps an active mark unless the new data was
// fetched by a request that STARTED after the mark: usage fetched before the
// limit hit must never make a just-limited account look free.

// stampMark returns e with MarkedAt set to at.
func stampMark(e CacheAccount, at time.Time) CacheAccount {
	out := e
	stamp := at.UTC()
	out.MarkedAt = &stamp
	return out
}

func subWindowLimited(w *CacheWindow, now time.Time) bool {
	if w == nil {
		return false
	}
	_, limited := windowLimitedUntil(w.UsedPct, w.ResetsAt, now)
	return limited
}

func fiveLimited(e CacheAccount, now time.Time) bool {
	_, limited := windowLimitedUntil(e.FiveHourUsed, e.FiveHourResetsAt, now)
	return limited
}

func sevenLimited(e CacheAccount, now time.Time) bool {
	_, limited := windowLimitedUntil(e.SevenDayUsed, e.SevenDayResetsAt, now)
	return limited
}

// markActive reports whether e carries a mark whose window has not yet
// reset (reset + ResetGrace). A mark without a known reset stays active until
// a fetch started after it succeeds.
func markActive(e CacheAccount, now time.Time) bool {
	if e.MarkedAt == nil {
		return false
	}
	return fiveLimited(e, now) || sevenLimited(e, now) ||
		subWindowLimited(e.SevenDayOpus, now) || subWindowLimited(e.SevenDaySonnet, now)
}

// withoutMark returns e with an expired (or any) mark stamp removed.
func withoutMark(e CacheAccount) CacheAccount {
	out := e
	out.MarkedAt = nil
	return out
}

// overlayMark returns base with mark's still-limited windows, its limited
// state and its stamp. Bookkeeping fields (fetch time, backoff, errors) and
// the windows the mark did not exhaust come from base.
func overlayMark(base, mark CacheAccount, now time.Time) CacheAccount {
	out := base
	if fiveLimited(mark, now) {
		out.FiveHourUsed, out.FiveHourResetsAt = mark.FiveHourUsed, mark.FiveHourResetsAt
	}
	if sevenLimited(mark, now) {
		out.SevenDayUsed, out.SevenDayResetsAt = mark.SevenDayUsed, mark.SevenDayResetsAt
	}
	if subWindowLimited(mark.SevenDayOpus, now) {
		out.SevenDayOpus = mark.SevenDayOpus
	}
	if subWindowLimited(mark.SevenDaySonnet, now) {
		out.SevenDaySonnet = mark.SevenDaySonnet
	}
	out.State, out.MarkedAt = StateLimited, mark.MarkedAt
	return out
}

// mergeFetch is the entry to write for one fetch result, built from cur (the
// entry read under the cache lock). started is when the request began. A
// logged-out result clears the entry, mark included: the login is gone.
func mergeFetch(cur CacheAccount, name string, res usage.AccountUsage, started time.Time) CacheAccount {
	next := nextCacheEntry(cur, name, res, started)
	switch {
	case res.State == usage.StateLoggedOut:
		return next
	case !markActive(cur, started):
		return withoutMark(next)
	case started.After(*cur.MarkedAt):
		// Success: fresh data, no mark. Failure: next keeps cur's mark.
		return next
	}
	return overlayMark(next, cur, started)
}

// mergeSnapshot is the entry WriteCache writes for a snapshot row: fresh
// display data with old's fetch state, never dropping old's active mark
// (snapshot rows may predate it).
func mergeSnapshot(fresh, old CacheAccount, now time.Time) CacheAccount {
	out := carryFetchState(fresh, old)
	if !markActive(old, now) {
		return withoutMark(out)
	}
	return overlayMark(out, old, now)
}

// markedLeftovers are old's actively marked entries missing from next, so a
// snapshot without that account still keeps the mark until it expires.
func markedLeftovers(next, old []CacheAccount, now time.Time) []CacheAccount {
	listed := indexByName(next)
	var out []CacheAccount
	for _, o := range old {
		if _, ok := listed[o.Name]; !ok && markActive(o, now) {
			out = append(out, o)
		}
	}
	return out
}
