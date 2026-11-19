// Package usage fetches per-account plan usage and aggregates it.
package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"multiclaude/internal/consts"
	"multiclaude/internal/keychain"
	"multiclaude/internal/registry"
	"multiclaude/internal/run"
)

const (
	httpTimeout  = 10 * time.Second
	maxBodyBytes = 1 << 20
)

// Window is one rate-limit window.
type Window struct {
	UsedPct  *float64
	ResetsAt *time.Time
}

// Extra is the optional extra-usage block.
type Extra struct {
	Enabled bool
	UsedPct *float64
}

// Usage is the parsed usage API response.
type Usage struct {
	FiveHour, SevenDay           Window
	SevenDayOpus, SevenDaySonnet *Window
	Extra                        *Extra
}

type rawWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

type rawExtra struct {
	IsEnabled   bool     `json:"is_enabled"`
	Utilization *float64 `json:"utilization"`
}

type rawUsage struct {
	FiveHour       *rawWindow `json:"five_hour"`
	SevenDay       *rawWindow `json:"seven_day"`
	SevenDayOpus   *rawWindow `json:"seven_day_opus"`
	SevenDaySonnet *rawWindow `json:"seven_day_sonnet"`
	Extra          *rawExtra  `json:"extra_usage"`
}

func (r *rawWindow) window() Window {
	if r == nil {
		return Window{}
	}
	w := Window{UsedPct: r.Utilization}
	if r.ResetsAt != nil {
		if t, err := time.Parse(time.RFC3339Nano, *r.ResetsAt); err == nil {
			w.ResetsAt = &t
		}
	}
	return w
}

func (r *rawWindow) optWindow() *Window {
	if r == nil {
		return nil
	}
	w := r.window()
	return &w
}

// Parse decodes a usage response tolerantly; it errors only for non-objects.
func Parse(b []byte) (Usage, error) {
	var raw *rawUsage
	if err := json.Unmarshal(b, &raw); err != nil {
		return Usage{}, fmt.Errorf("parse usage: %w", err)
	}
	if raw == nil {
		return Usage{}, errors.New("parse usage: not a JSON object")
	}
	u := Usage{
		FiveHour:       raw.FiveHour.window(),
		SevenDay:       raw.SevenDay.window(),
		SevenDayOpus:   raw.SevenDayOpus.optWindow(),
		SevenDaySonnet: raw.SevenDaySonnet.optWindow(),
	}
	if raw.Extra != nil {
		u.Extra = &Extra{Enabled: raw.Extra.IsEnabled, UsedPct: raw.Extra.Utilization}
	}
	return u, nil
}

// State describes the outcome of fetching one account's usage.
type State string

const (
	StateOK               State = "ok"
	StateLoggedOut        State = "logged-out"
	StateExpiredRefreshed State = "expired-refreshed"
	StateAPIError         State = "api-error"
)

// AccountUsage is the per-account result. It never contains the token.
type AccountUsage struct {
	Account, Email, Plan string
	State                State
	Err                  string
	Usage                *Usage
}

// Fetcher fetches usage for accounts.
type Fetcher struct {
	Runner run.Runner
	HTTP   *http.Client
	Now    func() time.Time
	// Refresh renews an account's credentials (wired by the caller).
	Refresh func(ctx context.Context, a registry.Account) error
	url     string
}

// WithURL returns a copy of f targeting url (for tests).
func (f Fetcher) WithURL(u string) Fetcher {
	f.url = u
	return f
}

func (f Fetcher) endpoint() string {
	if f.url == "" {
		return consts.UsageURL
	}
	return f.url
}

func (f Fetcher) client() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	return &http.Client{Timeout: httpTimeout}
}

func (f Fetcher) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f Fetcher) get(ctx context.Context, token string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.endpoint(), nil)
	if err != nil {
		return 0, nil, errors.New("build request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", consts.UsageBeta)
	resp, err := f.client().Do(req)
	if err != nil {
		// Deliberately unwrapped: keep error text free of request details.
		return 0, nil, errors.New("request failed")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return 0, nil, errors.New("read response")
	}
	return resp.StatusCode, body, nil
}

// refreshAndRead refreshes then re-reads creds. ok=false means res is final.
func (f Fetcher) refreshAndRead(ctx context.Context, a registry.Account, res AccountUsage) (*keychain.Creds, AccountUsage, bool) {
	if f.Refresh == nil || f.Refresh(ctx, a) != nil {
		res.State, res.Err = StateAPIError, "refresh failed"
		return nil, res, false
	}
	c, err := keychain.Read(ctx, f.Runner, a)
	if err != nil {
		res.State, res.Err = StateAPIError, "bad credentials"
		return nil, res, false
	}
	if c == nil {
		res.State = StateLoggedOut
		return nil, res, false
	}
	return c, res, true
}

// Fetch returns usage for one account. At most one refresh is attempted.
func (f Fetcher) Fetch(ctx context.Context, a registry.Account) AccountUsage {
	res := AccountUsage{Account: a.Name, Email: a.Email}
	creds, err := keychain.Read(ctx, f.Runner, a)
	if err != nil {
		res.State, res.Err = StateAPIError, "bad credentials"
		return res
	}
	if creds == nil {
		res.State = StateLoggedOut
		return res
	}
	refreshed := false
	ok := true
	if creds.ExpiresAt <= f.now().UnixMilli() {
		if creds, res, ok = f.refreshAndRead(ctx, a, res); !ok {
			return res
		}
		refreshed = true
	}
	res.Plan = creds.SubscriptionType
	code, body, err := f.get(ctx, creds.AccessToken)
	if err == nil && code == http.StatusUnauthorized && !refreshed {
		if creds, res, ok = f.refreshAndRead(ctx, a, res); !ok {
			return res
		}
		refreshed = true
		res.Plan = creds.SubscriptionType
		code, body, err = f.get(ctx, creds.AccessToken)
	}
	return finish(res, refreshed, code, body, err)
}

func finish(res AccountUsage, refreshed bool, code int, body []byte, err error) AccountUsage {
	if err != nil {
		res.State, res.Err = StateAPIError, err.Error()
		return res
	}
	if code != http.StatusOK {
		res.State, res.Err = StateAPIError, fmt.Sprintf("HTTP %d", code)
		return res
	}
	u, perr := Parse(body)
	if perr != nil {
		res.State, res.Err = StateAPIError, "bad response"
		return res
	}
	res.Usage = &u
	res.State = StateOK
	if refreshed {
		res.State = StateExpiredRefreshed
	}
	return res
}
