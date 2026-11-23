package usage

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/run"
)

func TestFetchCarriesAccountUUIDAndNullWindows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"five_hour":null,"seven_day":{"utilization":3}}`))
	}))
	defer srv.Close()
	body := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"expiresAt":%d,"accountUuid":"uuid-9"}}`, testToken, farFuture())
	r := &run.Fake{Respond: func(run.Call) (run.Result, error) { return run.Result{Stdout: body}, nil }}
	var n int32
	res := newFetcher(r, srv.URL, &n).Fetch(context.Background(), acct)
	if res.State != StateOK || res.AccountUUID != "uuid-9" || res.Usage == nil || res.Usage.FiveHour.UsedPct != nil {
		t.Fatalf("got %+v", res)
	}
	if strings.Contains(fmt.Sprintf("%+v", res.Err), "uuid-9") {
		t.Fatal("uuid leaked")
	}
}
