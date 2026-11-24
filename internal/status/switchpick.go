package status

import (
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

const (
	// ResetGrace is added to a window's reset time before an account limited
	// by it counts as free again, so a switch never races the server's reset.
	ResetGrace = time.Minute

	limitFiveHour       = "five_hour"
	limitSevenDay       = "seven_day"
	limitSevenDayOpus   = "seven_day_opus"
	limitSevenDaySonnet = "seven_day_sonnet"
)

// MarkLimitedType records, under the cache lock, that the named account hit the
// limit of kind limitType (Claude Code's rateLimitType) until resetsAt. Other
// entries and the account's fetch state are kept; only a successful usage
// fetch started after the mark overwrites it with real numbers.
func MarkLimitedType(p paths.Paths, name, limitType string, resetsAt time.Time) error {
	at := time.Now()
	return updateCache(p, func(old Cache) Cache {
		e, ok := indexByName(old.Accounts)[name]
		if !ok {
			e = CacheAccount{Name: name}
		}
		return withEntry(old, stampMark(limitedEntry(e, limitType, resetsAt.UTC()), at))
	})
}

// limitedEntry returns e with the window named by limitType fully used.
// Unknown kinds (e.g. "overage") mark the five-hour window: the shortest hold.
func limitedEntry(e CacheAccount, limitType string, resetsAt time.Time) CacheAccount {
	out := e
	full, at := fullPct, resetsAt
	switch limitType {
	case limitSevenDay:
		out.SevenDayUsed, out.SevenDayResetsAt = &full, &at
	case limitSevenDayOpus:
		out.SevenDayOpus = &CacheWindow{UsedPct: &full, ResetsAt: &at}
	case limitSevenDaySonnet:
		out.SevenDaySonnet = &CacheWindow{UsedPct: &full, ResetsAt: &at}
	default:
		out.FiveHourUsed, out.FiveHourResetsAt = &full, &at
	}
	out.State = StateLimited
	return out
}

// windowLimitedUntil reports whether a window is exhausted at now and, when
// its reset is known, when it frees up (reset + ResetGrace).
func windowLimitedUntil(used *float64, resets *time.Time, now time.Time) (*time.Time, bool) {
	if used == nil || *used < fullPct {
		return nil, false
	}
	if resets == nil {
		return nil, true
	}
	free := resets.Add(ResetGrace)
	if !now.Before(free) {
		return nil, false
	}
	return &free, true
}

// EntryLimitedUntil reports whether a cached account is limited (5h or 7d
// window exhausted and not yet reset) at now, and the latest known moment it
// frees up (nil when no reset time is known).
func EntryLimitedUntil(e CacheAccount, now time.Time) (*time.Time, bool) {
	five, l5 := windowLimitedUntil(e.FiveHourUsed, e.FiveHourResetsAt, now)
	seven, l7 := windowLimitedUntil(e.SevenDayUsed, e.SevenDayResetsAt, now)
	switch {
	case l5 && l7:
		return later(five, seven), true
	case l5:
		return five, true
	case l7:
		return seven, true
	}
	return nil, false
}

func later(a, b *time.Time) *time.Time {
	if a == nil || b == nil {
		return nil
	}
	if a.After(*b) {
		return a
	}
	return b
}

func earlier(cur, t *time.Time) *time.Time {
	if t == nil {
		return cur
	}
	if cur == nil || t.Before(*cur) {
		v := *t
		return &v
	}
	return cur
}

// cacheScore scores a cached account at now; accounts with no cached usage
// get unknownScore (usable, ranked last).
func cacheScore(e CacheAccount, now time.Time) (Score, bool) {
	if !hasUsage(e) {
		return unknownScore(), true
	}
	five := usage.Window{UsedPct: e.FiveHourUsed, ResetsAt: e.FiveHourResetsAt}
	week := usage.Window{UsedPct: e.SevenDayUsed, ResetsAt: e.SevenDayResetsAt}
	return ScoreWindows(five, week, now)
}

// hasUsage reports whether e carries usage numbers (fetched, or marked).
func hasUsage(e CacheAccount) bool {
	return e.hasLastGood() || e.FiveHourUsed != nil || e.SevenDayUsed != nil
}

// SwitchChoice is the outcome of PickSwitch: the account to continue on, or
// (when none is free) the soonest known moment one frees up.
type SwitchChoice struct {
	Account   registry.Account
	Found     bool
	WaitUntil *time.Time
}

// PickSwitch chooses, from the cache only (no network, no Keychain), the
// registered account to continue a session on. It skips logged-out accounts,
// accounts limited in the cache, and every login listed in avoid (name ->
// limited until) while that time is in the future, together with any account
// signed in as the same login. Duplicate logins are considered once. The
// best Score wins (weekly quota expiring soonest first); accounts with no
// cached usage rank last; ties keep registry order.
func PickSwitch(accts []registry.Account, c Cache, avoid map[string]time.Time, now time.Time) SwitchChoice {
	entries := indexByName(c.Accounts)
	list := loginRows(accts, entries)
	canon := usage.CanonicalIndex(list)
	blocked, wait := avoidedLogins(accts, canon, avoid, now)
	choice, best := SwitchChoice{}, Score{}
	for i, a := range accts {
		if canon[i] != i || blocked[i] || list[i].State == usage.StateLoggedOut {
			continue
		}
		e := entries[a.Name]
		if until, limited := EntryLimitedUntil(e, now); limited {
			wait = earlier(wait, until)
			continue
		}
		score, ok := cacheScore(e, now)
		if !ok {
			continue
		}
		if !choice.Found || score.Better(best) {
			choice, best = SwitchChoice{Account: a, Found: true}, score
		}
	}
	if !choice.Found {
		choice.WaitUntil = wait
	}
	return choice
}

// loginRows turns registry accounts into login-identity rows for duplicate
// detection (cached login id and email; registry email as a fallback).
func loginRows(accts []registry.Account, entries map[string]CacheAccount) []usage.AccountUsage {
	out := make([]usage.AccountUsage, len(accts))
	for i, a := range accts {
		e := entries[a.Name]
		state := usage.StateOK
		if e.State == string(usage.StateLoggedOut) {
			state = usage.StateLoggedOut
		}
		out[i] = usage.AccountUsage{Account: a.Name, Email: firstNonEmpty(e.Email, a.Email),
			AccountUUID: e.LoginID, State: state}
	}
	return out
}

// avoidedLogins marks every row whose login group contains an account still
// avoided at now, and returns the soonest moment an avoided account frees up.
func avoidedLogins(accts []registry.Account, canon []int, avoid map[string]time.Time, now time.Time) (map[int]bool, *time.Time) {
	groups := map[int]bool{}
	var wait *time.Time
	for i, a := range accts {
		if until, ok := avoid[a.Name]; ok && now.Before(until) {
			groups[canon[i]] = true
			wait = earlier(wait, &until)
		}
	}
	out := make(map[int]bool, len(accts))
	for i := range accts {
		out[i] = groups[canon[i]]
	}
	return out, wait
}
