package status

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/fsutil"
	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

const (
	cacheDirName  = "cache"
	cacheFileName = "usage.json"
	cacheFileMode = 0o600
	// StateLimited is the cached state of an account with a fully used window.
	StateLimited = "limited"
)

// CacheWindow is a cached optional per-model window.
type CacheWindow struct {
	UsedPct  *float64   `json:"used_pct"`
	ResetsAt *time.Time `json:"resets_at"`
}

// CacheAccount is one account's cached usage plus its fetch state. Usage
// fields always hold the last good data: a failed fetch never replaces them.
// It never holds credentials.
type CacheAccount struct {
	Name             string       `json:"name"`
	Email            string       `json:"email"`
	Plan             string       `json:"plan"`
	FiveHourUsed     *float64     `json:"five_hour_used"`
	SevenDayUsed     *float64     `json:"seven_day_used"`
	FiveHourResetsAt *time.Time   `json:"five_hour_resets_at"`
	SevenDayResetsAt *time.Time   `json:"seven_day_resets_at"`
	SevenDayOpus     *CacheWindow `json:"seven_day_opus,omitempty"`
	SevenDaySonnet   *CacheWindow `json:"seven_day_sonnet,omitempty"`
	State            string       `json:"state"`
	// Stale marks usage kept from an earlier fetch because the latest failed.
	Stale bool `json:"stale,omitempty"`
	// LoginID is the login's account UUID, so cached duplicates still merge.
	LoginID string `json:"account_uuid,omitempty"`
	// FetchedAt is the last successful fetch (nil: no good data).
	FetchedAt    *time.Time `json:"fetched_at,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
	RateLimited  bool       `json:"rate_limited,omitempty"`
	BackoffUntil *time.Time `json:"backoff_until,omitempty"`
	BackoffStep  int        `json:"backoff_step,omitempty"`
	// MarkedAt is when a limit hit was marked (MarkLimited/MarkLimitedType).
	// Until the marked window resets, only data from a fetch started after
	// MarkedAt replaces the marked window (see markmerge.go).
	MarkedAt *time.Time `json:"marked_at,omitempty"`
}

// hasLastGood reports whether e carries usage from a successful fetch.
func (e CacheAccount) hasLastGood() bool { return e.FetchedAt != nil }

// CacheCombined is the cross-account total kept alongside the accounts.
type CacheCombined struct {
	FiveHourLeft float64    `json:"five_hour_left"`
	SevenDayLeft float64    `json:"seven_day_left"`
	Counted      int        `json:"counted"`
	AvailableNow int        `json:"available_now"`
	NextResetAt  *time.Time `json:"next_reset_at"`
}

// Cache is the on-disk usage snapshot read by the status line, and the
// per-account fetch state shared by every ccm process.
type Cache struct {
	WrittenAt time.Time      `json:"written_at"`
	Accounts  []CacheAccount `json:"accounts"`
	Combined  CacheCombined  `json:"combined"`
}

// CachePath is where the usage cache lives.
func CachePath(p paths.Paths) string {
	return filepath.Join(p.AccountsRoot, cacheDirName, cacheFileName)
}

func toCacheWindow(w *usage.Window) *CacheWindow {
	if w == nil {
		return nil
	}
	return &CacheWindow{UsedPct: w.UsedPct, ResetsAt: w.ResetsAt}
}

func fromCacheWindow(w *CacheWindow) *usage.Window {
	if w == nil {
		return nil
	}
	return &usage.Window{UsedPct: w.UsedPct, ResetsAt: w.ResetsAt}
}

// toCacheAccount is a row's display snapshot (no fetch state).
func toCacheAccount(r Row) CacheAccount {
	ca := CacheAccount{Name: r.Account, Email: r.Email, Plan: r.Plan, State: string(r.State),
		LoginID: r.AccountUUID, Stale: r.Stale && r.Usage != nil}
	if r.Usage == nil {
		return ca
	}
	ca.FiveHourUsed, ca.FiveHourResetsAt = r.Usage.FiveHour.UsedPct, r.Usage.FiveHour.ResetsAt
	ca.SevenDayUsed, ca.SevenDayResetsAt = r.Usage.SevenDay.UsedPct, r.Usage.SevenDay.ResetsAt
	ca.SevenDayOpus, ca.SevenDaySonnet = toCacheWindow(r.Usage.SevenDayOpus), toCacheWindow(r.Usage.SevenDaySonnet)
	if limitedWindow(r) != "" {
		ca.State = StateLimited
	}
	return ca
}

// buildCache assembles the snapshot. A merged duplicate login gets its own
// entry (same usage, its own name) so the status line finds every account.
func buildCache(rows []Row, c usage.Combined, now time.Time) Cache {
	cache := Cache{
		WrittenAt: now.UTC(),
		Accounts:  make([]CacheAccount, 0, len(rows)),
		Combined:  CacheCombined{c.FiveHourLeft, c.SevenDayLeft, c.Counted, c.AvailableNow, c.NextResetAt},
	}
	listed := map[string]bool{}
	for _, r := range rows {
		listed[r.Account] = true
	}
	for _, r := range rows {
		ca := toCacheAccount(r)
		cache.Accounts = append(cache.Accounts, ca)
		for _, alias := range r.Aliases {
			if !listed[alias] {
				dup := ca
				dup.Name = alias
				cache.Accounts = append(cache.Accounts, dup)
			}
		}
	}
	return cache
}

// carryFetchState keeps old's fetch state on a fresh snapshot entry, and
// keeps old's last good usage when the snapshot row is an error without data.
func carryFetchState(fresh, old CacheAccount) CacheAccount {
	if fresh.State == string(usage.StateAPIError) && old.hasLastGood() {
		kept := old
		kept.Name, kept.Stale = fresh.Name, true
		return kept
	}
	out := fresh
	out.FetchedAt, out.LastError, out.RateLimited = old.FetchedAt, old.LastError, old.RateLimited
	out.BackoffUntil, out.BackoffStep = old.BackoffUntil, old.BackoffStep
	if out.LoginID == "" {
		out.LoginID = old.LoginID
	}
	return out
}

func indexByName(list []CacheAccount) map[string]CacheAccount {
	out := make(map[string]CacheAccount, len(list))
	for _, a := range list {
		out[a.Name] = a
	}
	return out
}

// WriteCache writes the usage snapshot (dir 0700, file 0600) under the cache
// lock, merged with the fetch state and limit marks other processes recorded.
func WriteCache(p paths.Paths, rows []Row, c usage.Combined, now time.Time) error {
	return updateCache(p, func(old Cache) Cache {
		next := buildCache(rows, c, now)
		prev := indexByName(old.Accounts)
		for i, a := range next.Accounts {
			if o, ok := prev[a.Name]; ok {
				next.Accounts[i] = mergeSnapshot(a, o, now)
			}
		}
		next.Accounts = append(next.Accounts, markedLeftovers(next.Accounts, old.Accounts, now)...)
		return next
	})
}

// updateCache applies change to the current cache (empty when missing or
// unreadable) and writes the result atomically, holding the cache lock.
func updateCache(p paths.Paths, change func(Cache) Cache) error {
	unlock, err := lockCache(p)
	if err != nil {
		return err
	}
	defer unlock()
	old, err := ReadCache(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, errCorruptCache) {
		return err
	}
	data, err := json.Marshal(change(old))
	if err != nil {
		return fmt.Errorf("encode usage cache: %w", err)
	}
	return fsutil.WriteAtomic(CachePath(p), data, cacheFileMode)
}

// errCorruptCache marks a cache file that is not valid JSON.
var errCorruptCache = errors.New("corrupt usage cache")

// ReadCache loads the usage snapshot; a missing file wraps os.ErrNotExist.
func ReadCache(p paths.Paths) (Cache, error) {
	var c Cache
	data, err := os.ReadFile(CachePath(p))
	if err != nil {
		return c, fmt.Errorf("read usage cache: %w", err)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return Cache{}, fmt.Errorf("parse usage cache: %w (%w)", errCorruptCache, err)
	}
	return c, nil
}
