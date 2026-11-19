package usage

import (
	"math"
	"testing"
	"time"
)

func f64(v float64) *float64 { return &v }

func au(name string, st State, five, seven *float64, reset *time.Time) AccountUsage {
	u := &Usage{FiveHour: Window{UsedPct: five, ResetsAt: reset}, SevenDay: Window{UsedPct: seven}}
	return AccountUsage{Account: name, State: st, Usage: u}
}

func TestAggregate(t *testing.T) {
	reset := time.Date(2026, 11, 19, 20, 0, 0, 0, time.UTC)
	got := Aggregate([]AccountUsage{
		au("a", StateOK, f64(40), f64(10), nil),
		au("b", StateOK, f64(100), f64(50), &reset),
		{Account: "x", State: StateAPIError},
	})
	if math.Abs(got.FiveHourLeft-0.6) > 1e-9 || math.Abs(got.SevenDayLeft-1.4) > 1e-9 {
		t.Fatalf("left %+v", got)
	}
	if got.Counted != 2 || len(got.Excluded) != 1 || got.Excluded[0] != "x" || got.AvailableNow != 1 {
		t.Fatalf("got %+v", got)
	}
	if got.NextResetAt == nil || !got.NextResetAt.Equal(reset) {
		t.Fatalf("next %v", got.NextResetAt)
	}
}

func TestAggregateOverUsedClampedAndNilSkipped(t *testing.T) {
	got := Aggregate([]AccountUsage{
		au("a", StateExpiredRefreshed, f64(130), nil, nil),
		{Account: "n", State: StateOK},
	})
	// seven_day is nil: idle, fully free; five_hour over-used clamps to 0.
	if got.FiveHourLeft != 0 || got.SevenDayLeft != 1 || got.AvailableNow != 0 || got.NextResetAt != nil {
		t.Fatalf("got %+v", got)
	}
}
