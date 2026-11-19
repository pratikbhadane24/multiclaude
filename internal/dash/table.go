package dash

import (
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"multiclaude/internal/status"
	"multiclaude/internal/tokens"
)

const (
	colGap       = "  "
	markerWidth  = 2
	minNameWidth = 4
	maxNameWidth = 16
	maxMailWidth = 26
	maxPlanWidth = 8
	// barColSuffix is the room for " 100%" after a bar.
	barColSuffix = 5
	resetWidth   = 8
	stateWidth   = 10
	markerText   = "› "
	bestText     = "★"
)

// column is one table column; prio decides which columns survive on a
// narrow terminal (lower is kept first).
type column struct {
	title string
	width int
	prio  int
	cell  func(r status.Row, m Model) (string, lipgloss.Style)
}

func pctCell(get func(r status.Row) *float64) func(status.Row, Model) (string, lipgloss.Style) {
	return func(r status.Row, m Model) (string, lipgloss.Style) {
		p := get(r)
		return bar(p, m.barWidth()), usageStyle(p)
	}
}

// barWidth narrows the bars on small terminals so TODAY still fits.
func (m Model) barWidth() int {
	if m.width < wideWidth {
		return narrowBarWidth
	}
	return barWidth
}

func emptyTokens(t tokens.Tokens) bool { return t == tokens.Tokens{} }

func fiveUsed(r status.Row) *float64 {
	if r.Usage == nil {
		return nil
	}
	return r.Usage.FiveHour.UsedPct
}

func sevenUsed(r status.Row) *float64 {
	if r.Usage == nil {
		return nil
	}
	return r.Usage.SevenDay.UsedPct
}

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
		{"NAME", widest(rows, func(r status.Row) string { return r.Account + " " + bestText }, minNameWidth, maxNameWidth), 0, nameCell},
		{"EMAIL", widest(rows, func(r status.Row) string { return r.Email }, len("EMAIL"), maxMailWidth), 8, func(r status.Row, _ Model) (string, lipgloss.Style) {
			return r.Email, plain
		}},
		{"PLAN", widest(rows, func(r status.Row) string { return r.Plan }, len("PLAN"), maxPlanWidth), 5, func(r status.Row, _ Model) (string, lipgloss.Style) {
			return r.Plan, plain
		}},
		{"5H USED", m.barWidth() + barColSuffix, 1, pctCell(fiveUsed)},
		{"7D USED", m.barWidth() + barColSuffix, 2, pctCell(sevenUsed)},
		{"5H RESET", resetWidth, 4, resetCell},
		{"STATE", stateWidth, 3, func(r status.Row, _ Model) (string, lipgloss.Style) { return stateLabel(r) }},
	}
}

func nameCell(r status.Row, m Model) (string, lipgloss.Style) {
	if r.Account == m.bestName() {
		return r.Account + " " + bestText, styleBold
	}
	return r.Account, styleBold
}

func resetCell(r status.Row, m Model) (string, lipgloss.Style) {
	if r.Usage == nil || r.Usage.FiveHour.ResetsAt == nil {
		return "-", styleDim
	}
	return status.FormatDuration(r.Usage.FiveHour.ResetsAt.Sub(m.deps.Now())), plain
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
func (m Model) tableLines() []string {
	cols := fitColumns(m.columns(), m.width)
	titles := make([]string, len(cols))
	for i, c := range cols {
		titles[i] = pad(c.title, c.width)
	}
	lines := []string{styleDim.Render(strings.Repeat(" ", markerWidth) + strings.Join(titles, colGap))}
	for i, r := range m.snap.Rows {
		cells := make([]string, len(cols))
		for j, c := range cols {
			text, st := c.cell(r, m)
			cells[j] = st.Render(pad(text, c.width))
		}
		marker := strings.Repeat(" ", markerWidth)
		if i == m.selected {
			marker = styleSel.Render(markerText)
		}
		lines = append(lines, marker+strings.Join(cells, colGap))
	}
	return lines
}
