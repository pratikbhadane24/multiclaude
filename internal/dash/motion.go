package dash

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pratikbhadane24/multiclaude/internal/status"
)

const (
	// frameInterval is the animation frame period (20 fps).
	frameInterval = 50 * time.Millisecond
	// tweenFrames is how many frames one bar takes to reach its new value.
	tweenFrames = 12
	// staggerFrames delays each row after the one above it, up to maxStagger rows.
	staggerFrames = 1
	maxStagger    = 8
	// spinnerEvery is the number of frames one spinner glyph stays up.
	spinnerEvery = 2

	gauge5h     = "5h/"
	gauge7d     = "7d/"
	gaugeLeft5h = "left/5h"
	gaugeLeft7d = "left/7d"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// frameMsg advances the animation by one frame.
type frameMsg struct{}

// gauge is one animated value on screen (a percentage) and how many frames
// it waits before it starts moving.
type gauge struct {
	id     string
	target float64
	delay  int
}

// motion is the animation state. Frames only run while something moves, so
// a resting dashboard schedules no timers and redraws nothing.
type motion struct {
	frame int
	// left counts down the frames until every gauge rests on its target.
	left, total int
	// from holds each gauge's value when the current tween began.
	from map[string]float64
	// ticking is true while a frame timer is scheduled.
	ticking bool
}

func (f motion) advance() motion {
	f.ticking = false
	f.frame++
	f.left = max(f.left-1, 0)
	return f
}

// retarget starts a tween from the values currently shown.
func (f motion) retarget(from map[string]float64, rows int) motion {
	f.from = from
	f.total = tweenFrames + min(rows, maxStagger)*staggerFrames
	f.left = f.total
	return f
}

// value is where g's bar is drawn this frame.
func (f motion) value(g gauge) float64 {
	if f.left <= 0 {
		return g.target
	}
	t := float64(f.total-f.left-g.delay) / tweenFrames
	start := f.from[g.id]
	return start + (g.target-start)*easeOutCubic(min(max(t, 0), 1))
}

func (f motion) spinner() string {
	return spinnerFrames[f.frame/spinnerEvery%len(spinnerFrames)]
}

func easeOutCubic(t float64) float64 {
	inv := 1 - t
	return 1 - inv*inv*inv
}

// busy reports whether anything on screen is moving.
func (m Model) busy() bool {
	return m.loading || len(m.running) > 0 || m.fx.left > 0
}

// animate schedules the next frame while the dashboard is busy; at most one
// frame timer is ever pending.
func (m Model) animate(cmd tea.Cmd) (Model, tea.Cmd) {
	if !m.deps.Motion || m.fx.ticking || !m.busy() {
		return m, cmd
	}
	m.fx.ticking = true
	frame := m.deps.Tick(frameInterval, func(time.Time) tea.Msg { return frameMsg{} })
	return m, tea.Batch(cmd, frame)
}

func rowDelay(i int) int { return min(i, maxStagger) * staggerFrames }

// rowGauge is the gauge behind one usage bar; an idle window rests at zero.
func (m Model) rowGauge(prefix string, r status.Row, v status.WindowView) gauge {
	p, _ := usedFraction(v)
	delay := 0
	for i, row := range m.snap.Rows {
		if row.Account == r.Account {
			delay = rowDelay(i)
		}
	}
	return gauge{id: prefix + r.Account, target: p, delay: delay}
}

func leftGauge(id string, left float64, counted int) gauge {
	if counted == 0 {
		return gauge{id: id}
	}
	return gauge{id: id, target: left / float64(counted) * fullPct}
}

// gauges lists every animated value of the current snapshot.
func (m Model) gauges() []gauge {
	now, c := m.deps.Now(), m.snap.Combined
	out := []gauge{
		leftGauge(gaugeLeft5h, c.FiveHourLeft, c.Counted),
		leftGauge(gaugeLeft7d, c.SevenDayLeft, c.Counted),
	}
	for _, r := range m.snap.Rows {
		out = append(out,
			m.rowGauge(gauge5h, r, status.Window5h(r, now)),
			m.rowGauge(gauge7d, r, status.Window7d(r, now)))
	}
	return out
}

// retargeted starts easing every bar from where it is drawn now towards the
// values of the snapshot about to be applied.
func (m Model) retargeted(rows int) Model {
	if !m.deps.Motion {
		return m
	}
	from := make(map[string]float64)
	for _, g := range m.gauges() {
		from[g.id] = m.fx.value(g)
	}
	m.fx = m.fx.retarget(from, rows)
	return m
}
