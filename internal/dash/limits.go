package dash

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"multiclaude/internal/status"
)

const resumeFlag = "--resume"

// target is a launch a confirm prompt may start; an empty name launches nothing.
type target struct {
	name string
	// resume continues the latest conversation for this folder.
	resume bool
}

// confirm is a [y/N] question: y runs yes; n or ⏎ runs no; esc runs nothing.
type confirm struct {
	text    string
	yes, no target
}

func (m Model) rowFor(name string) (status.Row, bool) {
	for _, r := range m.snap.Rows {
		if r.Account == name {
			return r, true
		}
	}
	return status.Row{}, false
}

// limitedReset renders when r's limited window resets ("?" when unknown).
func (m Model) limitedReset(r status.Row, lim string) string {
	w := r.Usage.FiveHour
	if lim == "7d" {
		w = r.Usage.SevenDay
	}
	if w.ResetsAt == nil {
		return "?"
	}
	return status.FormatDuration(w.ResetsAt.Sub(m.deps.Now()))
}

// enter launches name, first asking to use the best account instead when
// name is limited and another account is free.
func (m Model) enter(name string) (Model, tea.Cmd) {
	r, _ := m.rowFor(name)
	lim := status.LimitedWindow(r)
	best := m.bestName()
	if lim == "" || best == "" || best == name {
		return m.launch(name, nil)
	}
	text := fmt.Sprintf("%s is limited (resets in %s). Start on %s? [y/N]", name, m.limitedReset(r, lim), best)
	return m.ask(confirm{text: text, yes: target{name: best}, no: target{name: name}}), nil
}

func (m Model) ask(c confirm) Model {
	m.mode, m.prompt, m.toast = modeConfirm, c, ""
	return m
}

func (m Model) onConfirmKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	c := m.prompt
	switch k.String() {
	case "y", "Y":
		return m.closePrompt().start(c.yes)
	case "n", "N", "enter":
		return m.closePrompt().start(c.no)
	case "esc", "q":
		return m.closePrompt(), nil
	}
	return m, nil
}

func (m Model) closePrompt() Model {
	m.mode, m.prompt = modeList, confirm{}
	return m
}

// start runs t: a plain launch, or `--resume <latest session>` on t.name.
func (m Model) start(t target) (Model, tea.Cmd) {
	if t.name == "" {
		return m, nil
	}
	if !t.resume {
		return m.launch(t.name, nil)
	}
	if m.deps.LatestSession == nil {
		return m.withToast("no conversation to continue"), nil
	}
	id, err := m.deps.LatestSession()
	if err != nil {
		return m.withToast("cannot continue: " + err.Error()), nil
	}
	next, cmd := m.launch(t.name, []string{resumeFlag, id})
	if cmd == nil {
		return next, nil
	}
	return next.withToast(fmt.Sprintf("continuing %s on %s", id, t.name)), cmd
}

// afterSnapshot decides the post-exit handoff once a snapshot collected after
// claude exited arrives; an older snapshot triggers a fresh collect instead.
func (m Model) afterSnapshot(msg snapshotMsg) (Model, tea.Cmd) {
	from := m.handoffFrom
	if from == "" || msg.snap.Err != nil {
		return m, nil
	}
	if msg.started.Before(m.handoffAfter) {
		return m.refresh()
	}
	m.handoffFrom = ""
	r, ok := m.rowFor(from)
	best := m.bestName()
	if !ok || status.LimitedWindow(r) == "" || best == "" || best == from || m.mode != modeList {
		return m, nil
	}
	text := fmt.Sprintf("Continue this conversation on %s? [y/N]", best)
	return m.ask(confirm{text: text, yes: target{name: best, resume: true}}), nil
}
