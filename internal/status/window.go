package status

import (
	"fmt"
	"strings"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

const (
	// LowPct is the used percentage from which a window is "low" (still usable).
	LowPct = 95.0

	idleText = "idle"
	// NoLimitText is the reset text of a window the plan does not have.
	NoLimitText = "no limit"
	planMax     = "max"
	label5h     = "5h"
	label7d     = "7d"
	stateOKTx   = "ok"
)

// WindowView is one rate-limit window prepared for display. Idle means the
// API reported no window (nothing used yet); Low is >=95% and <100% used;
// Limited is >=100% used. NoLimit marks a five-hour window the plan does not
// have (Max reports none): shown as "no limit", never as idle.
type WindowView struct {
	Used, Left, Reset           string
	Idle, Low, Limited, NoLimit bool
}

func countableRow(r Row) bool {
	return (r.State == usage.StateOK || r.State == usage.StateExpiredRefreshed) && r.Usage != nil
}

// isIdleWindow is a window with nothing running: not reported at all, or 0%
// used with no reset time.
func isIdleWindow(w usage.Window) bool {
	return w.UsedPct == nil || (*w.UsedPct == 0 && w.ResetsAt == nil)
}

func buildView(r Row, w usage.Window, now time.Time) WindowView {
	if !countableRow(r) {
		return WindowView{Used: naText, Left: naText, Reset: naText}
	}
	if isIdleWindow(w) {
		return WindowView{Used: "0%", Left: "100%", Reset: idleText, Idle: true}
	}
	used := *w.UsedPct
	return WindowView{
		Used:    fmtPct(w.UsedPct),
		Left:    fmtLeft(w.UsedPct),
		Reset:   fmtReset(w.ResetsAt, now),
		Low:     used >= LowPct && used < fullPct,
		Limited: used >= fullPct,
	}
}

// Window5h is the row's five-hour window as display text and flags.
func Window5h(r Row, now time.Time) WindowView {
	var w usage.Window
	if r.Usage != nil {
		w = r.Usage.FiveHour
	}
	v := buildView(r, w, now)
	if v.Idle && w.UsedPct == nil && r.Plan == planMax {
		v.Reset, v.NoLimit = NoLimitText, true
	}
	return v
}

// Window7d is the row's seven-day window as display text and flags.
func Window7d(r Row, now time.Time) WindowView {
	var w usage.Window
	if r.Usage != nil {
		w = r.Usage.SevenDay
	}
	return buildView(r, w, now)
}

// limitedWindow returns "5h" or "7d" when that window is fully used.
func limitedWindow(r Row) string {
	return severeWindow(r, func(v WindowView) bool { return v.Limited })
}

// lowWindow returns "5h" or "7d" when that window is low (and none limited).
func lowWindow(r Row) string {
	return severeWindow(r, func(v WindowView) bool { return v.Low })
}

func severeWindow(r Row, pick func(WindowView) bool) string {
	if pick(Window5h(r, time.Time{})) {
		return label5h
	}
	if pick(Window7d(r, time.Time{})) {
		return label7d
	}
	return ""
}

// StateLabel is the STATE column text: limited beats low beats ok. Usage
// kept from an earlier fetch gets a " · stale 4m" suffix.
func StateLabel(r Row) string {
	if r.SameAs != "" {
		return "same as " + r.SameAs
	}
	return baseLabel(r) + StaleSuffix(r)
}

func baseLabel(r Row) string {
	if lim := limitedWindow(r); lim != "" {
		return "limited (" + lim + ")"
	}
	if low := lowWindow(r); low != "" {
		return "low (" + low + ")"
	}
	switch r.State {
	case usage.StateOK:
		return stateOKTx
	case usage.StateExpiredRefreshed:
		return "ok (token refreshed)"
	case usage.StateLoggedOut:
		return "logged out → ccm login " + r.Account
	}
	if r.RateLimited {
		return rateLimitedLabel(r)
	}
	if r.Err != "" {
		return "api error: " + r.Err
	}
	return "api error"
}

// DisplayName is the account name, with merged duplicate logins appended:
// "main (= admin)" or "main (= a, b)".
func (r Row) DisplayName() string {
	if len(r.Aliases) == 0 {
		return r.Account
	}
	return fmt.Sprintf("%s (= %s)", r.Account, strings.Join(r.Aliases, ", "))
}
