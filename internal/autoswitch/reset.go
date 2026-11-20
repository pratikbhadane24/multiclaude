package autoswitch

import (
	"strings"
	"time"
)

// clockLayouts are the time-of-day forms Claude Code prints ("1:20am", "3pm").
var clockLayouts = []string{"3:04pm", "3pm"}

const (
	dateSep   = " at "
	dayLayout = "Jan 2"
	oneDay    = 24 * time.Hour
)

// ParseReset turns a reset text such as "1:20am (Asia/Calcutta)" or
// "Nov 3 at 10:30am (Asia/Calcutta)" into the next matching instant after
// now. A missing or unknown zone means local time. ok is false when the text
// is not understood.
func ParseReset(text string, now time.Time) (time.Time, bool) {
	body, loc := splitZone(strings.TrimSpace(text))
	datePart, clockPart, hasDate := strings.Cut(body, dateSep)
	if !hasDate {
		clockPart = body
	}
	clock, ok := parseClock(clockPart)
	if !ok {
		return time.Time{}, false
	}
	ref := now.In(loc)
	if !hasDate {
		t := time.Date(ref.Year(), ref.Month(), ref.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
		if !t.After(now) {
			t = t.Add(oneDay)
		}
		return t, true
	}
	day, err := time.Parse(dayLayout, strings.TrimSpace(datePart))
	if err != nil {
		return time.Time{}, false
	}
	t := time.Date(ref.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
	if !t.After(now) {
		t = t.AddDate(1, 0, 0)
	}
	return t, true
}

// splitZone removes a trailing "(Zone/Name)" and returns its location
// (time.Local when absent or unknown).
func splitZone(text string) (string, *time.Location) {
	open := strings.LastIndex(text, "(")
	if open < 0 || !strings.HasSuffix(text, ")") {
		return text, time.Local
	}
	loc, err := time.LoadLocation(text[open+1 : len(text)-1])
	if err != nil {
		loc = time.Local
	}
	return strings.TrimSpace(text[:open]), loc
}

func parseClock(s string) (time.Time, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, layout := range clockLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
