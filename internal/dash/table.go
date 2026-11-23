package dash

import (
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"multiclaude/internal/status"
	"multiclaude/internal/tokens"
)

const (
	colGap       = "  "
	markerWidth  = 2
	minNameWidth = 4
	maxNameWidth = 24
	maxMailWidth = 26
	maxPlanWidth = 8
	// barColSuffix is the room for " 100%" after a bar.
	barColSuffix = 5
	resetWidth   = 8
	stateWidth   = 18
	markerText   = "▌ "
	bestText     = "★"
	// tableChrome is the lines the accounts panel adds to its rows (two
	// borders and the column titles).
	tableChrome = panelBorders + 1
	planMax     = "max"
)

// column is one table column; prio decides which columns survive on a
// narrow terminal (lower is kept first).
type column struct {
	title string
	width int
	prio  int
	// cell renders one styled cell on base (the row background).
	cell cellFunc
}

type cellFunc func(r status.Row, m Model, base lipgloss.Style) string

// textCell is a cell of one text in one style.
func textCell(get func(r status.Row, m Model) (string, lipgloss.Style)) cellFunc {
	return func(r status.Row, m Model, base lipgloss.Style) string {
		text, st := get(r, m)
		return st.Inherit(base).Render(text)
	}
}

// pctCell is a usage bar, drawn at its animated value.
func pctCell(prefix string, get func(r status.Row, now time.Time) status.WindowView) cellFunc {
	return func(r status.Row, m Model, base lipgloss.Style) string {
		v := get(r, m.deps.Now())
		return barShown(v, m.barWidth(), m.fx.value(m.rowGauge(prefix, r, v)), base)
	}
}

// innerWidth is the room inside a full-width panel.
func (m Model) innerWidth() int { return max(m.width-panelChrome, minPanelWidth) }

// barWidth narrows the bars on small terminals so TODAY still fits.
func (m Model) barWidth() int {
	if m.width < wideWidth {
		return narrowBarWidth
	}
	return barWidth
}

func emptyTokens(t tokens.Tokens) bool { return t == tokens.Tokens{} }

func widest(rows []status.Row, get func(status.Row) string, lo, hi int) int {
	w := lo
	for _, r := range rows {
		w = max(w, lipgloss.Width(get(r)))
	}
	return min(w, hi)
}

// columns returns every column in display order.
func (m Model) columns() []column {
	rows := m.snap.Rows
	return []column{
		{"NAME", widest(rows, func(r status.Row) string { return rowName(r) + " " + bestText }, minNameWidth, maxNameWidth), 0, nameCell},
		{"EMAIL", widest(rows, func(r status.Row) string { return r.Email }, len("EMAIL"), maxMailWidth), 8, textCell(func(r status.Row, _ Model) (string, lipgloss.Style) {
			return r.Email, styleDim
		})},
		{"PLAN", widest(rows, func(r status.Row) string { return r.Plan }, len("PLAN"), maxPlanWidth), 5, textCell(planCell)},
		{"5H USED", m.barWidth() + barColSuffix, 1, pctCell(gauge5h, status.Window5h)},
		{"7D USED", m.barWidth() + barColSuffix, 2, pctCell(gauge7d, status.Window7d)},
		{"5H RESET", resetWidth, 4, textCell(resetCell)},
		{"STATE", stateWidth, 3, stateCell},
	}
}

func nameCell(r status.Row, m Model, base lipgloss.Style) string {
	name := styleBold.Inherit(base).Render(rowName(r))
	if r.Account != m.bestName() {
		return name
	}
	return name + styleGold.Inherit(base).Render(" "+bestText)
}

func planCell(r status.Row, _ Model) (string, lipgloss.Style) {
	if r.Plan == planMax {
		return r.Plan, styleAccent
	}
	return r.Plan, plain
}

// stateCell is the state with a glyph in its tone ("● ok", "✖ limited (5h)").
func stateCell(r status.Row, _ Model, base lipgloss.Style) string {
	text, t := stateTone(r)
	if r.SameAs != "" {
		return t.style().Inherit(base).Render(text)
	}
	return t.style().Inherit(base).Render(t.glyph() + " " + text)
}

// rowName is the account name with its merged duplicates ("main (= admin)")
// or, on a shown duplicate, the account it repeats ("admin (= main)").
func rowName(r status.Row) string {
	if r.SameAs != "" {
		return r.Account + " (= " + r.SameAs + ")"
	}
	return r.DisplayName()
}

func resetCell(r status.Row, m Model) (string, lipgloss.Style) {
	v := status.Window5h(r, m.deps.Now())
	if v.Idle || v.Reset == naText {
		return v.Reset, styleDim
	}
	return v.Reset, plain
}

// fitColumns keeps columns in priority order until the next one would not
// fit in width; display order is preserved.
func fitColumns(cols []column, width int) []column {
	order := make([]int, len(cols))
	for i := range cols {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cols[a].prio - cols[b].prio })
	keep := make(map[int]bool, len(cols))
	used := markerWidth - len(colGap)
	for _, i := range order {
		if used+len(colGap)+cols[i].width > width {
			break
		}
		keep[i], used = true, used+len(colGap)+cols[i].width
	}
	out := make([]column, 0, len(keep))
	for i, c := range cols {
		if keep[i] {
			out = append(out, c)
		}
	}
	return out
}

// tableLines renders the column header and one line per account.
func (m Model) tableLines() []string { return m.tableWindow(0, len(m.snap.Rows)) }

// tableWindow renders the column header and rows [lo, hi).
func (m Model) tableWindow(lo, hi int) []string {
	width := m.innerWidth()
	cols := fitColumns(m.columns(), width)
	titles := make([]string, len(cols))
	for i, c := range cols {
		titles[i] = pad(c.title, c.width)
	}
	lines := []string{styleDim.Render(strings.Repeat(" ", markerWidth) + strings.Join(titles, colGap))}
	for i := lo; i < hi; i++ {
		lines = append(lines, m.rowLine(cols, i, width))
	}
	return lines
}

// rowLine renders one account; the selected row is lit across the full width.
func (m Model) rowLine(cols []column, i, width int) string {
	base, marker := plain, strings.Repeat(" ", markerWidth)
	if i == m.selected {
		base, marker = styleSel, styleAccent.Inherit(styleSel).Render(markerText)
	}
	cells := make([]string, len(cols))
	for j, c := range cols {
		cells[j] = padOn(c.cell(m.snap.Rows[i], m, base), c.width, base)
	}
	return padOn(marker+strings.Join(cells, base.Render(colGap)), width, base)
}
