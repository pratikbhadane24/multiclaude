package status

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"multiclaude/internal/paths"
	"multiclaude/internal/usage"
)

const (
	cacheDirName  = "cache"
	cacheFileName = "usage.json"
	cacheDirMode  = 0o700
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

// WriteCache atomically writes the usage snapshot (dir 0700, file 0600).
func WriteCache(p paths.Paths, rows []Row, c usage.Combined, now time.Time) error {
	cache := Cache{
		WrittenAt: now.UTC(),
		Accounts:  make([]CacheAccount, len(rows)),
		Combined:  CacheCombined{c.FiveHourLeft, c.SevenDayLeft, c.Counted, c.AvailableNow, c.NextResetAt},
	}
	for i, r := range rows {
		cache.Accounts[i] = toCacheAccount(r)
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("encode usage cache: %w", err)
	}
	dir := filepath.Dir(CachePath(p))
	if err := os.MkdirAll(dir, cacheDirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	return writeFileAtomic(dir, CachePath(p), data)
}

func writeFileAtomic(dir, dest string, data []byte) (err error) {
	tmp, err := os.CreateTemp(dir, ".usage-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(cacheFileMode); err == nil {
		_, err = tmp.Write(data)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err = os.Rename(tmp.Name(), dest); err != nil {
		return fmt.Errorf("replace %s: %w", dest, err)
	}
	return nil
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
