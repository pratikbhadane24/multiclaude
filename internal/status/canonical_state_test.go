package status

import (
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

func TestPickBestReturnsHealthyDuplicateOfErroredAccount(t *testing.T) {
	bad := usage.AccountUsage{Account: "main", Email: "a@x.io", State: usage.StateAPIError}
	good := ok("admin", f(10), f(10), nil)
	good.Email = "a@x.io"
	got, found := PickBest([]usage.AccountUsage{bad, good}, now)
	if !found || got.Account != "admin" {
		t.Fatalf("%v %v", got, found)
	}
}
