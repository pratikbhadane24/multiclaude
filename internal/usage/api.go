// Package usage fetches per-account plan usage and aggregates it.
package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/consts"
	"github.com/pratikbhadane24/multiclaude/internal/keychain"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/run"
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

// decodeField decodes raw into T, returning the zero value and false when the
// field is absent, null, or of the wrong type.
func decodeField[T any](raw json.RawMessage) (T, bool) {
	var v T
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		var zero T
		return zero, false
	}
	return v, true
}

// parseWindow decodes one window; ok=false when raw is not a JSON object.
func parseWindow(raw json.RawMessage) (Window, bool) {
	fields, ok := decodeField[map[string]json.RawMessage](raw)
	if !ok || fields == nil {
		return Window{}, false
	}
	var w Window
	if v, ok := decodeField[*float64](fields["utilization"]); ok {
		w.UsedPct = v
	}
	if s, ok := decodeField[*string](fields["resets_at"]); ok && s != nil {
		if t, err := time.Parse(time.RFC3339Nano, *s); err == nil {
			w.ResetsAt = &t
		}
	}
	return w, true
}

func parseExtra(raw json.RawMessage) *Extra {
	fields, ok := decodeField[map[string]json.RawMessage](raw)
	if !ok || fields == nil {
		return nil
	}
	e := &Extra{}
	if v, ok := decodeField[bool](fields["is_enabled"]); ok {
		e.Enabled = v
	}
	if v, ok := decodeField[*float64](fields["utilization"]); ok {
		e.UsedPct = v
	}
	return e
}

func optWindow(raw json.RawMessage) *Window {
	w, ok := parseWindow(raw)
	if !ok {
		return nil
	}
	return &w
}

// Parse decodes a usage response tolerantly: a wrongly-typed field becomes
// nil/zero for that field only. It errors only when b is not a JSON object.
func Parse(b []byte) (Usage, error) {
	top, ok := decodeField[map[string]json.RawMessage](b)
	if !ok || top == nil {
		return Usage{}, errors.New("parse usage: not a JSON object")
	}
	fiveHour, _ := parseWindow(top["five_hour"])
	sevenDay, _ := parseWindow(top["seven_day"])
	return Usage{
		FiveHour:       fiveHour,
		SevenDay:       sevenDay,
		SevenDayOpus:   optWindow(top["seven_day_opus"]),
		SevenDaySonnet: optWindow(top["seven_day_sonnet"]),
		Extra:          parseExtra(top["extra_usage"]),
	}, nil
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
	// AccountUUID identifies the login behind the credentials; used only to
	// detect two accounts signed in as the same user. Never printed.
	AccountUUID string
	// RateLimited means the usage API answered 429 (now, or at the last
	// attempt when the result is served from the cache).
	RateLimited bool
	// RetryAfter is the server's Retry-After on a 429 (0 when absent).
	RetryAfter time.Duration
	// Freshness, set by the status fetch cache. Stale means Usage is the
	// last good data while the latest fetch failed or is backing off;
	// StaleFor is its age. RetryAt/RetryIn say when the next fetch may run.
	Stale    bool
	StaleFor time.Duration
	RetryAt  *time.Time
	RetryIn  time.Duration
}

// Fetcher fetches usage for accounts.
type Fetcher struct {
	Runner run.Runner
	HTTP   *http.Client
	Now    func() time.Time
	// Refresh renews an account's credentials (wired by the caller).
	Refresh func(ctx context.Context, a registry.Account) error
	// Shared (optional) lets accounts signed in as the same login share one
	// usage request; nil fetches every account on its own.
	Shared *SharedFetches
	url    string
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

// response is the outcome of one usage request.
type response struct {
	code       int
	body       []byte
	retryAfter time.Duration
	err        error
}

func (f Fetcher) get(ctx context.Context, token string) response {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.endpoint(), nil)
	if err != nil {
		return response{err: errors.New("build request")}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", consts.UsageBeta)
	resp, err := f.client().Do(req)
	if err != nil {
		// Deliberately unwrapped: keep error text free of request details.
		return response{err: errors.New(transportErrText(err))}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return response{err: errors.New("read response")}
	}
	return response{code: resp.StatusCode, body: body, retryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
}

// parseRetryAfter reads a Retry-After given in seconds; anything else
// (absent, zero, negative, an HTTP date) is 0.
func parseRetryAfter(v string) time.Duration {
	secs, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// getFirst is the first request of a fetch: shared with other accounts of
// the same login when f.Shared is set.
func (f Fetcher) getFirst(ctx context.Context, uuid, token string) response {
	if f.Shared == nil || uuid == "" {
		return f.get(ctx, token)
	}
	return f.Shared.do(uuid, func() response { return f.get(ctx, token) })
}

// transportErrText maps a transport error to a fixed, detail-free message.
func transportErrText(err error) string {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return "timeout"
	}
	return "request failed"
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
	// ExpiresAt == 0 means unknown: do not refresh proactively (a 401 will).
	if creds.ExpiresAt > 0 && creds.ExpiresAt <= f.now().UnixMilli() {
		if creds, res, ok = f.refreshAndRead(ctx, a, res); !ok {
			return res
		}
		refreshed = true
	}
	res.Plan, res.AccountUUID = creds.SubscriptionType, creds.AccountUUID
	resp := f.getFirst(ctx, creds.AccountUUID, creds.AccessToken)
	if resp.err == nil && resp.code == http.StatusUnauthorized && !refreshed {
		if creds, res, ok = f.refreshAndRead(ctx, a, res); !ok {
			return res
		}
		refreshed = true
		res.Plan, res.AccountUUID = creds.SubscriptionType, creds.AccountUUID
		resp = f.get(ctx, creds.AccessToken)
	}
	return finish(res, refreshed, resp)
}

func finish(res AccountUsage, refreshed bool, resp response) AccountUsage {
	if resp.err != nil {
		res.State, res.Err = StateAPIError, resp.err.Error()
		return res
	}
	if resp.code == http.StatusTooManyRequests {
		res.State, res.Err = StateAPIError, "rate limited"
		res.RateLimited, res.RetryAfter = true, resp.retryAfter
		return res
	}
	if resp.code != http.StatusOK {
		res.State, res.Err = StateAPIError, fmt.Sprintf("HTTP %d", resp.code)
		return res
	}
	u, perr := Parse(resp.body)
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
