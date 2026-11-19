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
	TodayTokens  int64       `json:"today_tokens"`
	Input        int64       `json:"today_input"`
	Output       int64       `json:"today_output"`
	CacheRead    int64       `json:"today_cache_read"`
	CacheWrite   int64       `json:"today_cache_write"`
	TodayError   string      `json:"today_error,omitempty"`
	LinkWarnings []string    `json:"link_warnings"`
	LinkErr      string      `json:"link_error,omitempty"`
}

type jsonCombined struct {
	FiveHourLeft float64    `json:"five_hour_left"`
	SevenDayLeft float64    `json:"seven_day_left"`
	Counted      int        `json:"counted"`
	Excluded     []string   `json:"excluded"`
	AvailableNow int        `json:"available_now"`
	NextResetAt  *time.Time `json:"next_reset_at"`
}

func toJSONWindow(w *usage.Window) *jsonWindow {
	if w == nil {
		return nil
	}
	return &jsonWindow{w.UsedPct, w.ResetsAt}
}

func toJSONRow(r Row) jsonRow {
	jr := jsonRow{
		Account: r.Account, Email: r.Email, Plan: r.Plan, State: string(r.State), Error: r.Err,
		TodayTokens: todayTotal(r.Today), Input: r.Today.Input, Output: r.Today.Output,
		CacheRead: r.Today.CacheRead, CacheWrite: r.Today.CacheWrite, TodayError: r.TodayErr, LinkErr: r.LinkErr,
		LinkWarnings: append([]string{}, r.LinkWarnings...),
	}
	if r.Usage != nil {
		jr.FiveHour = &jsonWindow{r.Usage.FiveHour.UsedPct, r.Usage.FiveHour.ResetsAt}
		jr.SevenDay = &jsonWindow{r.Usage.SevenDay.UsedPct, r.Usage.SevenDay.ResetsAt}
		jr.SevenOpus = toJSONWindow(r.Usage.SevenDayOpus)
		jr.SevenSonnet = toJSONWindow(r.Usage.SevenDaySonnet)
		jr.Limited = limitedWindow(r) != ""
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
	out.Combined = jsonCombined{c.FiveHourLeft, c.SevenDayLeft, c.Counted, append([]string{}, c.Excluded...), c.AvailableNow, c.NextResetAt}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("encode status json: %w", err)
	}
	return b, nil
}
