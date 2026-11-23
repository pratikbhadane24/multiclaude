package dash

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	// panelChrome is the columns a panel adds around its content ("│ " + " │").
	panelChrome = 4
	// panelBorders is the lines a panel adds (top and bottom border).
	panelBorders  = 2
	minPanelWidth = 12
	cardGap       = "   "
	cardBar       = "▎"
)

// panel draws lines in a rounded box w cells wide with title set in the top
// border.
func panel(title string, lines []string, w int) []string {
	if w < minPanelWidth {
		return lines
	}
	inner := w - panelChrome
	title = pad(title, min(lipgloss.Width(title), inner-1))
	rule := strings.Repeat("─", w-panelChrome-lipgloss.Width(title)-1)
	out := make([]string, 0, len(lines)+panelBorders)
	out = append(out, styleBorder.Render("╭─ ")+title+styleBorder.Render(" "+rule+"╮"))
	for _, l := range lines {
		out = append(out, styleBorder.Render("│ ")+pad(l, inner)+styleBorder.Render(" │"))
	}
	return append(out, styleBorder.Render("╰"+strings.Repeat("─", w-2)+"╯"))
}

// card is one summary figure in the header: a small label over its value.
type card struct{ label, value string }

// cardLines lays cards side by side as two lines (labels, then values),
// each card as wide as its widest line.
func cardLines(cards []card) []string {
	labels := make([]string, len(cards))
	values := make([]string, len(cards))
	for i, c := range cards {
		w := max(lipgloss.Width(c.label), lipgloss.Width(c.value))
		edge := styleAccent.Render(cardBar)
		labels[i] = edge + pad(styleDim.Render(c.label), w)
		values[i] = edge + pad(c.value, w)
	}
	return []string{strings.Join(labels, cardGap), strings.Join(values, cardGap)}
}

// spread puts left and right at the two ends of a w-cell line.
func spread(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < len(colGap) {
		return left + colGap + right
	}
	return left + strings.Repeat(" ", gap) + right
}

// window returns the [lo, hi) slice of n rows, at most size long, that keeps
// row sel visible.
func window(n, sel, size int) (lo, hi int) {
	if size >= n {
		return 0, n
	}
	lo = min(max(sel-size+1, 0), n-size)
	return lo, lo + size
}
