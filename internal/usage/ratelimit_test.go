package usage

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/run"
)

func rateLimitedServer(retryAfter string, calls *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(calls, 1)
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error"}}`))
	}))
}

func TestFetch429IsRateLimitedWithRetryAfter(t *testing.T) {
	cases := map[string]time.Duration{"": 0, "0": 0, "120": 2 * time.Minute, "soon": 0, "-5": 0}
	for header, want := range cases {
		var calls, n int32
		srv := rateLimitedServer(header, &calls)

		res := newFetcher(keychainRunner(farFuture()), srv.URL, &n).Fetch(context.Background(), acct)
		srv.Close()

		if res.State != StateAPIError || !res.RateLimited || res.Err != "rate limited" || res.RetryAfter != want {
			t.Errorf("Retry-After %q: got %+v", header, res)
		}
		if calls != 1 || n != 0 {
			t.Errorf("Retry-After %q: calls=%d refreshes=%d", header, calls, n)
		}
	}
}

func TestFetchOtherErrorIsNotRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	var n int32

	res := newFetcher(keychainRunner(farFuture()), srv.URL, &n).Fetch(context.Background(), acct)

	if res.State != StateAPIError || res.RateLimited || res.Err != "HTTP 503" {
		t.Fatalf("got %+v", res)
	}
}

func uuidRunner(uuid string) *run.Fake {
	body := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"expiresAt":%d,"accountUuid":%q}}`, testToken, farFuture(), uuid)
	return &run.Fake{Respond: func(run.Call) (run.Result, error) { return run.Result{Stdout: body}, nil }}
}

func TestSharedFetchesOneRequestPerLogin(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(20 * time.Millisecond)
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":7}}`))
	}))
	defer srv.Close()
	var n int32
	f := newFetcher(uuidRunner("same-login"), srv.URL, &n)
	f.Shared = NewSharedFetches()
	names := []string{"main", "admin", "third"}
	results := make([]AccountUsage, len(names))
	var wg sync.WaitGroup

	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			results[i] = f.Fetch(context.Background(), registry.Account{Name: name, Dir: "/d/" + name})
		}(i, name)
	}
	wg.Wait()
	results = append(results, f.Fetch(context.Background(), registry.Account{Name: "late", Dir: "/d/late"}))

	if calls != 1 {
		t.Fatalf("http calls=%d want 1", calls)
	}
	for i, r := range results {
		if r.State != StateOK || r.Usage == nil || *r.Usage.FiveHour.UsedPct != 7 {
			t.Fatalf("result %d: %+v", i, r)
		}
	}
	if results[1].Account != "admin" {
		t.Fatalf("shared result lost its own account name: %+v", results[1])
	}
}

func TestSharedFetchesDistinctLoginsAndNoUUID(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	var n int32
	shared := NewSharedFetches()
	for _, uuid := range []string{"a", "b", "", ""} {
		f := newFetcher(uuidRunner(uuid), srv.URL, &n)
		f.Shared = shared
		f.Fetch(context.Background(), acct)
	}

	if calls != 4 {
		t.Fatalf("http calls=%d want 4", calls)
	}
}
