package status

import (
	"fmt"
	"strings"
	"time"

	"multiclaude/internal/usage"
)

const (
	// LowPct is the used percentage from which a window is "low" (still usable).
	LowPct = 95.0

	idleText  = "idle"
	label5h   = "5h"
	label7d   = "7d"
	stateOKTx = "ok"
)

// WindowView is one rate-limit window prepared for display. Idle means the
// API reported no window (nothing used yet); Low is >=95% and <100% used;
// Limited is >=100% used.
type WindowView struct {
	Used, Left, Reset  string
	Idle, Low, Limited bool
}

func countableRow(r Row) bool {
	return (r.State == usage.StateOK || r.State == usage.StateExpiredRefreshed) && r.Usage != nil
}

func buildView(r Row, w usage.Window, now time.Time) WindowView {
	if !countableRow(r) {
		return WindowView{Used: naText, Left: naText, Reset: naText}
	}
	if w.UsedPct == nil {
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
	return buildView(r, w, now)
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

// StateLabel is the STATE column text: limited beats low beats ok.
func StateLabel(r Row) string {
	if r.SameAs != "" {
		return "same as " + r.SameAs
	}
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
