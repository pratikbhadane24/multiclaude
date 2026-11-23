package usage

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/run"
)

const testToken = "sk-secret-token"

var testNow = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func keychainRunner(expiresAt int64) *run.Fake {
	body := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"expiresAt":%d,"subscriptionType":"max","rateLimitTier":"t"}}`, testToken, expiresAt)
	return &run.Fake{Respond: func(run.Call) (run.Result, error) { return run.Result{Stdout: body}, nil }}
}

func newFetcher(r run.Runner, url string, refreshes *int32) Fetcher {
	f := Fetcher{Runner: r, Now: func() time.Time { return testNow }}
	f.Refresh = func(context.Context, registry.Account) error { atomic.AddInt32(refreshes, 1); return nil }
	return f.WithURL(url)
}

var acct = registry.Account{Name: "work", Dir: "/tmp/x", Email: "a@b.c"}

func farFuture() int64 { return testNow.Add(time.Hour).UnixMilli() }

func TestParseTolerant(t *testing.T) {
	u, err := Parse([]byte(`{"five_hour":{"utilization":42.5,"resets_at":"2026-11-19T20:00:00Z"},"seven_day":null,"iguana_necktie":{}}`))
	if err != nil || u.FiveHour.UsedPct == nil || *u.FiveHour.UsedPct != 42.5 || u.SevenDay.UsedPct != nil {
		t.Fatalf("got %+v %v", u, err)
	}
	if u.FiveHour.ResetsAt == nil || !u.FiveHour.ResetsAt.Equal(time.Date(2026, 11, 19, 20, 0, 0, 0, time.UTC)) {
		t.Fatalf("reset %v", u.FiveHour.ResetsAt)
	}
}

func TestParseBadResetAndExtras(t *testing.T) {
	u, err := Parse([]byte(`{"five_hour":{"utilization":1,"resets_at":"nope"},"seven_day_opus":{"utilization":5},"extra_usage":{"is_enabled":true,"utilization":3}}`))
	if err != nil || u.FiveHour.ResetsAt != nil || u.SevenDayOpus == nil || u.Extra == nil || !u.Extra.Enabled {
		t.Fatalf("got %+v %v", u, err)
	}
}

func TestParseRejectsNonObject(t *testing.T) {
	for _, in := range []string{`[]`, `nope`, `null`} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Fatalf("%q: expected error", in)
		}
	}
}

func TestFetchOK(t *testing.T) {
	var gotAuth, gotBeta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotBeta = r.Header.Get("Authorization"), r.Header.Get("anthropic-beta")
		w.Write([]byte(`{"five_hour":{"utilization":10}}`))
	}))
	defer srv.Close()
	var n int32
	res := newFetcher(keychainRunner(farFuture()), srv.URL, &n).Fetch(context.Background(), acct)
	if res.State != StateOK || res.Plan != "max" || res.Email != "a@b.c" || res.Usage == nil || n != 0 {
		t.Fatalf("got %+v refreshes=%d", res, n)
	}
	if gotAuth != "Bearer "+testToken || gotBeta != "oauth-2025-04-20" {
		t.Fatalf("headers %q %q", gotAuth, gotBeta)
	}
	if strings.Contains(fmt.Sprintf("%+v", res), testToken) {
		t.Fatal("token leaked")
	}
}

func TestFetch401Then200(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	var n int32
	res := newFetcher(keychainRunner(farFuture()), srv.URL, &n).Fetch(context.Background(), acct)
	if res.State != StateExpiredRefreshed || n != 1 {
		t.Fatalf("got %+v refreshes=%d", res, n)
	}
}

func TestFetch401Then401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer srv.Close()
	var n int32
	res := newFetcher(keychainRunner(farFuture()), srv.URL, &n).Fetch(context.Background(), acct)
	if res.State != StateAPIError || res.Err != "HTTP 401" || n != 1 {
		t.Fatalf("got %+v refreshes=%d", res, n)
	}
}

func TestFetchExpiredRefreshesOnlyOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer srv.Close()
	var n int32
	res := newFetcher(keychainRunner(testNow.UnixMilli()), srv.URL, &n).Fetch(context.Background(), acct)
	if res.State != StateAPIError || n != 1 {
		t.Fatalf("got %+v refreshes=%d", res, n)
	}
}

func TestFetchExpiredThenOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	defer srv.Close()
	var n int32
	res := newFetcher(keychainRunner(testNow.UnixMilli()-1), srv.URL, &n).Fetch(context.Background(), acct)
	if res.State != StateExpiredRefreshed || n != 1 {
		t.Fatalf("got %+v refreshes=%d", res, n)
	}
}

func TestFetch500HTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("<html>boom</html>"))
	}))
	defer srv.Close()
	var n int32
	res := newFetcher(keychainRunner(farFuture()), srv.URL, &n).Fetch(context.Background(), acct)
	if res.State != StateAPIError || res.Err != "HTTP 500" {
		t.Fatalf("got %+v", res)
	}
}

func TestFetchLoggedOutNoHTTP(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&hits, 1) }))
	defer srv.Close()
	var n int32
	r := &run.Fake{Respond: func(run.Call) (run.Result, error) { return run.Result{Code: 44}, nil }}
	res := newFetcher(r, srv.URL, &n).Fetch(context.Background(), acct)
	if res.State != StateLoggedOut || atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("got %+v hits=%d", res, hits)
	}
}

func TestFetchBadCredentials(t *testing.T) {
	r := &run.Fake{Respond: func(run.Call) (run.Result, error) { return run.Result{Stdout: "garbage-" + testToken}, nil }}
	var n int32
	res := newFetcher(r, "http://127.0.0.1:1", &n).Fetch(context.Background(), acct)
	if res.State != StateAPIError || res.Err != "bad credentials" {
		t.Fatalf("got %+v", res)
	}
	if strings.Contains(fmt.Sprintf("%+v", res), testToken) {
		t.Fatal("token leaked")
	}
}

func TestFetchRefreshFailure(t *testing.T) {
	f := newFetcher(keychainRunner(1), "http://127.0.0.1:1", new(int32))
	f.Refresh = func(context.Context, registry.Account) error { return fmt.Errorf("boom %s", testToken) }
	res := f.Fetch(context.Background(), acct)
	if res.State != StateAPIError || res.Err != "refresh failed" {
		t.Fatalf("got %+v", res)
	}
}

func TestParseWrongTypedFieldsAreNil(t *testing.T) {
	u, err := Parse([]byte(`{"five_hour":{"utilization":"5","resets_at":5},"seven_day":"x","seven_day_opus":7,"extra_usage":"y","seven_day_sonnet":{"utilization":2}}`))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if u.FiveHour.UsedPct != nil || u.FiveHour.ResetsAt != nil || u.SevenDay.UsedPct != nil || u.SevenDayOpus != nil || u.Extra != nil {
		t.Fatalf("got %+v", u)
	}
	if u.SevenDaySonnet == nil || u.SevenDaySonnet.UsedPct == nil || *u.SevenDaySonnet.UsedPct != 2 {
		t.Fatalf("sonnet %+v", u.SevenDaySonnet)
	}
}

func TestParseWrongTypedExtraFields(t *testing.T) {
	u, err := Parse([]byte(`{"extra_usage":{"is_enabled":"yes","utilization":"3"}}`))
	if err != nil || u.Extra == nil || u.Extra.Enabled || u.Extra.UsedPct != nil {
		t.Fatalf("got %+v %v", u, err)
	}
}

func TestFetchTimeoutError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) }))
	defer srv.Close()
	var n int32
	f := newFetcher(keychainRunner(farFuture()), srv.URL, &n)
	f.HTTP = &http.Client{Timeout: 50 * time.Millisecond}
	res := f.Fetch(context.Background(), acct)
	if res.State != StateAPIError || res.Err != "timeout" {
		t.Fatalf("got %+v", res)
	}
}

func TestFetchContextDeadlineIsTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) }))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var n int32
	res := newFetcher(keychainRunner(farFuture()), srv.URL, &n).Fetch(ctx, acct)
	if res.Err != "timeout" {
		t.Fatalf("got %+v", res)
	}
}

func TestFetchConnectionRefusedIsRequestFailed(t *testing.T) {
	var n int32
	res := newFetcher(keychainRunner(farFuture()), "http://127.0.0.1:1", &n).Fetch(context.Background(), acct)
	if res.Err != "request failed" || strings.Contains(fmt.Sprintf("%+v", res), "127.0.0.1") {
		t.Fatalf("got %+v", res)
	}
}

func TestFetchUnknownExpiryDoesNotRefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	defer srv.Close()
	var n int32
	res := newFetcher(keychainRunner(0), srv.URL, &n).Fetch(context.Background(), acct)
	if res.State != StateOK || atomic.LoadInt32(&n) != 0 {
		t.Fatalf("got %+v refreshes=%d", res, n)
	}
}

func TestFetchUnknownExpiry401StillRefreshes(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	var n int32
	res := newFetcher(keychainRunner(0), srv.URL, &n).Fetch(context.Background(), acct)
	if res.State != StateExpiredRefreshed || atomic.LoadInt32(&n) != 1 {
		t.Fatalf("got %+v refreshes=%d", res, n)
	}
}
