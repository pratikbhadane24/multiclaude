package autoswitch

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	oneDay     = 24 * time.Hour
	maxClockHr = 12
	maxMinute  = 59
	noonHour   = 12
	monthShort = "Jan"
	monthLong  = "January"
	resetsWord = "resets "
	resetWord  = "reset "
)

// resetRE finds an optional "Mon D at " date and a 12-hour clock ("1:20am",
// "5 PM") anywhere in a reset text, so prefixes ("resets", "at") and trailing
// junk (" · /upgrade …") are ignored.
var resetRE = regexp.MustCompile(`(?i)(?:\b([a-z]{3,9})\.?\s+(\d{1,2})\s+at\s+)?\b(\d{1,2})(?::(\d{2}))?\s*([ap])\.?m\b`)

// zoneRE is a "(Area/City)" zone name.
var zoneRE = regexp.MustCompile(`\(([A-Za-z0-9_+\-/]+)\)`)

// resetClock is a parsed reset text: a clock time, optionally on a date.
type resetClock struct {
	hasDate      bool
	month        time.Month
	day          int
	hour, minute int
}

// ParseReset turns a reset text such as "1:20am (Asia/Calcutta)",
// "resets Nov 3 at 10:30am (Asia/Calcutta) · /upgrade …" or "reset at 5pm"
// into the next matching instant after now. A missing or unknown zone means
// local time. ok is false when the text is not understood.
func ParseReset(text string, now time.Time) (time.Time, bool) {
	m := resetRE.FindStringSubmatchIndex(text)
	if m == nil {
		return time.Time{}, false
	}
	c, ok := parseResetMatch(text, m)
	if !ok {
		return time.Time{}, false
	}
	loc := zoneAfter(text[m[1]:])
	ref := now.In(loc)
	if !c.hasDate {
		t := time.Date(ref.Year(), ref.Month(), ref.Day(), c.hour, c.minute, 0, 0, loc)
		if !t.After(now) {
			t = t.Add(oneDay)
		}
		return t, true
	}
	t := time.Date(ref.Year(), c.month, c.day, c.hour, c.minute, 0, 0, loc)
	if !t.After(now) {
		t = t.AddDate(1, 0, 0)
	}
	return t, true
}

// parseResetMatch validates and converts the submatches of resetRE.
func parseResetMatch(text string, m []int) (resetClock, bool) {
	group := func(i int) string {
		if m[2*i] < 0 {
			return ""
		}
		return text[m[2*i]:m[2*i+1]]
	}
	hour, _ := strconv.Atoi(group(3))
	minute := 0
	if s := group(4); s != "" {
		minute, _ = strconv.Atoi(s)
	}
	if hour < 1 || hour > maxClockHr || minute > maxMinute {
		return resetClock{}, false
	}
	c := resetClock{hour: hour % noonHour, minute: minute}
	if strings.EqualFold(group(5), "p") {
		c.hour += noonHour
	}
	if group(1) == "" {
		return c, true
	}
	month, ok := parseMonth(group(1))
	day, _ := strconv.Atoi(group(2))
	if !ok || day < 1 || day > daysIn(month) {
		return resetClock{}, false
	}
	c.hasDate, c.month, c.day = true, month, day
	return c, true
}

// parseMonth reads "Nov" or "November" (any case).
func parseMonth(s string) (time.Month, bool) {
	s = strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
	for _, layout := range []string{monthShort, monthLong} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Month(), true
		}
	}
	return 0, false
}

// daysIn is the most days month can have (February counts 29).
func daysIn(month time.Month) int {
	const leapYear = 2028
	return time.Date(leapYear, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// zoneAfter is the location of the first "(Zone/Name)" in rest, or
// time.Local when there is none or it is unknown.
func zoneAfter(rest string) *time.Location {
	m := zoneRE.FindStringSubmatch(rest)
	if m == nil {
		return time.Local
	}
	loc, err := time.LoadLocation(m[1])
	if err != nil {
		return time.Local
	}
	return loc
}
