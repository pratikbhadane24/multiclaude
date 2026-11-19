package status

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"multiclaude/internal/fsutil"
	"multiclaude/internal/paths"
	"multiclaude/internal/usage"
)

const (
	cacheDirName  = "cache"
	cacheFileName = "usage.json"
	cacheFileMode = 0o600
	// StateLimited is the cached state of an account with a fully used window.
	StateLimited = "limited"
)

// CacheAccount is one account's cached usage. It never holds credentials.
type CacheAccount struct {
	Name             string     `json:"name"`
	Email            string     `json:"email"`
	Plan             string     `json:"plan"`
	FiveHourUsed     *float64   `json:"five_hour_used"`
	SevenDayUsed     *float64   `json:"seven_day_used"`
	FiveHourResetsAt *time.Time `json:"five_hour_resets_at"`
	SevenDayResetsAt *time.Time `json:"seven_day_resets_at"`
	State            string     `json:"state"`
}

// CacheCombined is the cross-account total kept alongside the accounts.
type CacheCombined struct {
	FiveHourLeft float64    `json:"five_hour_left"`
	SevenDayLeft float64    `json:"seven_day_left"`
	Counted      int        `json:"counted"`
	AvailableNow int        `json:"available_now"`
	NextResetAt  *time.Time `json:"next_reset_at"`
}

// Cache is the on-disk usage snapshot read by the status line.
type Cache struct {
	WrittenAt time.Time      `json:"written_at"`
	Accounts  []CacheAccount `json:"accounts"`
	Combined  CacheCombined  `json:"combined"`
}

// CachePath is where the usage cache lives.
func CachePath(p paths.Paths) string {
	return filepath.Join(p.AccountsRoot, cacheDirName, cacheFileName)
}

func toCacheAccount(r Row) CacheAccount {
	ca := CacheAccount{Name: r.Account, Email: r.Email, Plan: r.Plan, State: string(r.State)}
	if r.Usage == nil {
		return ca
	}
	ca.FiveHourUsed, ca.FiveHourResetsAt = r.Usage.FiveHour.UsedPct, r.Usage.FiveHour.ResetsAt
	ca.SevenDayUsed, ca.SevenDayResetsAt = r.Usage.SevenDay.UsedPct, r.Usage.SevenDay.ResetsAt
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

// WriteCache atomically writes the usage snapshot (dir 0700, file 0600).
func WriteCache(p paths.Paths, rows []Row, c usage.Combined, now time.Time) error {
	data, err := json.Marshal(buildCache(rows, c, now))
	if err != nil {
		return fmt.Errorf("encode usage cache: %w", err)
	}
	return fsutil.WriteAtomic(CachePath(p), data, cacheFileMode)
}

// ReadCache loads the usage snapshot; a missing file wraps os.ErrNotExist.
func ReadCache(p paths.Paths) (Cache, error) {
	var c Cache
	data, err := os.ReadFile(CachePath(p))
	if err != nil {
		return c, fmt.Errorf("read usage cache: %w", err)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return Cache{}, fmt.Errorf("parse usage cache: %w", err)
	}
	return c, nil
}
