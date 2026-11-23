package usage

import "time"

// elapsed reports whether w's reset time is at or before now: the API has
// reset the window, so its cached percentage no longer applies.
func (w Window) elapsed(now time.Time) bool {
	return w.ResetsAt != nil && !now.Before(*w.ResetsAt)
}

// hasData reports whether w carries a percentage worth showing (not idle).
func (w Window) hasData() bool {
	return w.UsedPct != nil && !(*w.UsedPct == 0 && w.ResetsAt == nil)
}

func dropElapsed(w Window, now time.Time) Window {
	if w.elapsed(now) {
		return Window{}
	}
	return w
}

func dropElapsedPtr(w *Window, now time.Time) *Window {
	if w == nil || w.elapsed(now) {
		return nil
	}
	return w
}

// WithoutElapsed returns a copy of u where every window that has reset by now
// is idle (main windows) or absent (model sub-windows). u is not changed.
func (u Usage) WithoutElapsed(now time.Time) Usage {
	out := u
	out.FiveHour = dropElapsed(u.FiveHour, now)
	out.SevenDay = dropElapsed(u.SevenDay, now)
	out.SevenDayOpus = dropElapsedPtr(u.SevenDayOpus, now)
	out.SevenDaySonnet = dropElapsedPtr(u.SevenDaySonnet, now)
	return out
}

// HasWindowData reports whether any window of u carries a percentage. Usage
// with none left says nothing an earlier fetch could have gotten wrong.
func (u Usage) HasWindowData() bool {
	for _, w := range []*Window{&u.FiveHour, &u.SevenDay, u.SevenDayOpus, u.SevenDaySonnet} {
		if w != nil && w.hasData() {
			return true
		}
	}
	return false
}
