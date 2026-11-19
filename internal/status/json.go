package status

import (
	"encoding/json"
	"fmt"
	"time"

	"multiclaude/internal/usage"
)

type jsonWindow struct {
	UsedPct  *float64   `json:"used_pct"`
	ResetsAt *time.Time `json:"resets_at"`
	// Idle marks a window the API reported as null on a usable account.
	Idle bool `json:"idle,omitempty"`
}

type jsonRow struct {
	Account      string      `json:"account"`
	Email        string      `json:"email"`
	Plan         string      `json:"plan"`
	State        string      `json:"state"`
	Error        string      `json:"error,omitempty"`
	FiveHour     *jsonWindow `json:"five_hour"`
	SevenDay     *jsonWindow `json:"seven_day"`
	SevenOpus    *jsonWindow `json:"seven_day_opus"`
	SevenSonnet  *jsonWindow `json:"seven_day_sonnet"`
	Limited      bool        `json:"limited"`
	LinkWarnings []string    `json:"link_warnings"`
	LinkErr      string      `json:"link_error,omitempty"`
	Low          bool        `json:"low,omitempty"`
	// Aliases are accounts signed in as the same login, merged into this row.
	Aliases []string `json:"aliases,omitempty"`
	SameAs  string   `json:"same_as,omitempty"`
	// StaleSeconds is the age of usage kept from an earlier fetch (stale rows only).
	StaleSeconds int64      `json:"stale_seconds,omitempty"`
	RetryAt      *time.Time `json:"retry_at,omitempty"`
	RateLimited  bool       `json:"rate_limited,omitempty"`
}

type jsonCombined struct {
	FiveHourLeft float64    `json:"five_hour_left"`
	SevenDayLeft float64    `json:"seven_day_left"`
	Counted      int        `json:"counted"`
	Excluded     []string   `json:"excluded"`
	AvailableNow int        `json:"available_now"`
	NextResetAt  *time.Time `json:"next_reset_at"`
	// Today is counted once: history is shared by every account.
	TodayTokens int64  `json:"today_tokens"`
	Input       int64  `json:"today_input"`
	Output      int64  `json:"today_output"`
	CacheRead   int64  `json:"today_cache_read"`
	CacheWrite  int64  `json:"today_cache_write"`
	TodayError  string `json:"today_error,omitempty"`
}

func toJSONWindow(w *usage.Window) *jsonWindow {
	if w == nil {
		return nil
	}
	return &jsonWindow{UsedPct: w.UsedPct, ResetsAt: w.ResetsAt}
}

func toJSONRow(r Row) jsonRow {
	jr := jsonRow{
		Account: r.Account, Email: r.Email, Plan: r.Plan, State: string(r.State), Error: r.Err,
		LinkErr:      r.LinkErr,
		LinkWarnings: append([]string{}, r.LinkWarnings...),
		Aliases:      append([]string(nil), r.Aliases...),
		SameAs:       r.SameAs,
		RetryAt:      r.RetryAt,
		RateLimited:  r.RateLimited,
	}
	if isStale(r) {
		jr.StaleSeconds = int64(r.StaleFor / time.Second)
	}
	if r.Usage != nil {
		idle := countableRow(r)
		jr.FiveHour = &jsonWindow{r.Usage.FiveHour.UsedPct, r.Usage.FiveHour.ResetsAt, idle && isIdleWindow(r.Usage.FiveHour)}
		jr.SevenDay = &jsonWindow{r.Usage.SevenDay.UsedPct, r.Usage.SevenDay.ResetsAt, idle && isIdleWindow(r.Usage.SevenDay)}
		jr.SevenOpus = toJSONWindow(r.Usage.SevenDayOpus)
		jr.SevenSonnet = toJSONWindow(r.Usage.SevenDaySonnet)
		jr.Limited = limitedWindow(r) != ""
		jr.Low = !jr.Limited && lowWindow(r) != ""
	}
	return jr
}

// RenderJSON encodes rows and the combined total; it never includes credentials.
func RenderJSON(rows []Row, c usage.Combined) ([]byte, error) {
	out := struct {
		Accounts []jsonRow    `json:"accounts"`
		Combined jsonCombined `json:"combined"`
	}{Accounts: make([]jsonRow, len(rows))}
	for i, r := range rows {
		out.Accounts[i] = toJSONRow(r)
	}
	out.Combined = jsonCombined{
		FiveHourLeft: c.FiveHourLeft, SevenDayLeft: c.SevenDayLeft, Counted: c.Counted,
		Excluded: append([]string{}, c.Excluded...), AvailableNow: c.AvailableNow, NextResetAt: c.NextResetAt,
		TodayTokens: todayTotal(c.Today), Input: c.Today.Input, Output: c.Today.Output,
		CacheRead: c.Today.CacheRead, CacheWrite: c.Today.CacheWrite, TodayError: c.TodayErr,
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("encode status json: %w", err)
	}
	return b, nil
}
