package status

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/tokens"
	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

func TestJSONTodayIsCombinedOnly(t *testing.T) {
	rows := []Row{{AccountUsage: ok("a", f(1), f(1), nil)}}
	c := usage.Aggregate([]usage.AccountUsage{rows[0].AccountUsage})
	c.Today = tokens.Tokens{Input: 3, Output: 4, CacheRead: 5, CacheWrite: 6}

	b, err := RenderJSON(rows, c)

	var got struct {
		Accounts []map[string]any `json:"accounts"`
		Combined map[string]any   `json:"combined"`
	}
	if err != nil || json.Unmarshal(b, &got) != nil {
		t.Fatalf("%v %s", err, b)
	}
	if _, has := got.Accounts[0]["today_tokens"]; has {
		t.Fatalf("per-account today must be absent: %s", b)
	}
	if got.Combined["today_tokens"] != float64(18) {
		t.Fatalf("combined=%v", got.Combined)
	}
}

func TestRowLineShowsDashForToday(t *testing.T) {
	line := rowLine(Row{AccountUsage: ok("a", f(1), f(1), nil)}, now, false)
	cols := strings.Split(line, "\t")
	if cols[len(cols)-2] != "-" {
		t.Fatalf("cols=%q", cols)
	}
}
