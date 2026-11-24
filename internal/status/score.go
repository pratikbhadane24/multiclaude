package status

import (
	"fmt"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

const (
	// MinRunwayPct is the smallest remaining share of either window for an
	// account to be preferred: below it a pick would hit a limit within
	// minutes, so it is used only when nothing else is free.
	MinRunwayPct = 10.0

	weekLength = 7 * 24 * time.Hour
	// minPressureSpan keeps the pressure finite for a reset that is moments away.
	minPressureSpan = time.Hour
)

// Score ranks how good an account is to run on now. It prefers accounts with
// runway in both windows, then the one whose weekly quota is most at risk of
// expiring unused (weekly % left per hour until the weekly reset), then the
// one with the most five-hour headroom.
type Score struct {
	Runway             bool
	Pressure           float64
	FiveLeft, WeekLeft float64
	WeekResetsAt       *time.Time
	unknown            bool
	now                time.Time
}

// unknownScore is an account with no cached usage: usable, ranked last.
func unknownScore() Score { return Score{unknown: true} }

// windowLeft is the share of w still free at now: an idle window or one past
// its reset is fully free.
func windowLeft(w usage.Window, now time.Time) float64 {
	if w.UsedPct == nil || (w.ResetsAt != nil && !now.Before(*w.ResetsAt)) {
		return fullPct
	}
	return max(fullPct-*w.UsedPct, 0)
}

// weekSpan is how long the weekly quota left has before it expires; an idle,
// reset or unknown window is assumed to have a full week ahead.
func weekSpan(w usage.Window, now time.Time) (time.Duration, *time.Time) {
	if w.UsedPct == nil || w.ResetsAt == nil || !now.Before(*w.ResetsAt) {
		return weekLength, nil
	}
	return max(w.ResetsAt.Sub(now), minPressureSpan), w.ResetsAt
}

// ScoreWindows scores the five-hour and seven-day windows at now; ok is false
// when either window is exhausted.
func ScoreWindows(five, week usage.Window, now time.Time) (Score, bool) {
	fiveLeft, weekLeft := windowLeft(five, now), windowLeft(week, now)
	if fiveLeft <= 0 || weekLeft <= 0 {
		return Score{}, false
	}
	span, resets := weekSpan(week, now)
	return Score{
		Runway:       min(fiveLeft, weekLeft) >= MinRunwayPct,
		Pressure:     weekLeft / span.Hours(),
		FiveLeft:     fiveLeft,
		WeekLeft:     weekLeft,
		WeekResetsAt: resets,
		now:          now,
	}, true
}

// ScoreUsage scores u at now; ok is false when u is nil or exhausted.
func ScoreUsage(u *usage.Usage, now time.Time) (Score, bool) {
	if u == nil {
		return Score{}, false
	}
	return ScoreWindows(u.FiveHour, u.SevenDay, now)
}

// Better reports whether s should be picked over o.
func (s Score) Better(o Score) bool {
	switch {
	case s.unknown != o.unknown:
		return o.unknown
	case s.Runway != o.Runway:
		return s.Runway
	case s.Pressure != o.Pressure:
		return s.Pressure > o.Pressure
	}
	return s.FiveLeft > o.FiveLeft
}

// Reason explains the score in a few words ("52% weekly left, resets in 7h40m").
func (s Score) Reason() string {
	if s.unknown {
		return "no usage known yet"
	}
	if s.WeekResetsAt == nil {
		return fmt.Sprintf("%.0f%% weekly left, %.0f%% 5h left", s.WeekLeft, s.FiveLeft)
	}
	return fmt.Sprintf("%.0f%% weekly left, resets in %s", s.WeekLeft, fmtDuration(s.WeekResetsAt.Sub(s.now)))
}
