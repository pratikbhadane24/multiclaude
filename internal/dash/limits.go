package dash

import (
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"multiclaude/internal/handoff"
	"multiclaude/internal/status"
)

const (
	resumeFlag    = "--resume"
	sessionIDFlag = "--session-id"
)

// target is a launch a confirm prompt may start; an empty name launches nothing.
type target struct {
	name string
	// resume continues the latest conversation for this folder.
	resume bool
	// session is the exact conversation to resume ("" = the latest idle one).
	session string
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
	if r.Usage == nil {
		return "?"
	}
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
		return m.launchNew(name)
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
		return m.launchNew(t.name)
	}
	id, ok := t.session, true
	var toast string
	if id == "" {
		id, toast, ok = m.latestSession()
	}
	if !ok {
		return m.withToast(toast), nil
	}
	next, cmd := m.launch(t.name, []string{resumeFlag, id}, "")
	if cmd == nil {
		return next, nil
	}
	return next.withToast(fmt.Sprintf("continuing %s on %s", id, t.name)), cmd
}

// latestSession looks up the newest idle conversation for this folder; on
// failure it returns the toast to show.
func (m Model) latestSession() (id, toast string, ok bool) {
	if m.deps.LatestSession == nil {
		return "", "no conversation to continue", false
	}
	id, err := m.deps.LatestSession()
	var live *handoff.LiveError
	switch {
	case errors.As(err, &live):
		return "", live.Error(), false
	case err != nil:
		return "", "cannot continue: " + err.Error(), false
	}
	return id, "", true
}

// handoffWindow is how long a handoff offer waits for the list to be visible.
const handoffWindow = 2 * time.Minute

// afterSnapshot arms the post-exit handoff offer once a snapshot collected
// after claude exited arrives; an older snapshot triggers a fresh collect
// instead, and a failed one cancels the offer.
func (m Model) afterSnapshot(msg snapshotMsg) (Model, tea.Cmd) {
	if m.handoffFrom == "" || m.handoffReady {
		return m, nil
	}
	if msg.started.Before(m.handoffAfter) {
		return m.refresh()
	}
	if msg.snap.Err != nil {
		return m.clearHandoff(), nil
	}
	m.handoffReady, m.handoffUntil = true, m.deps.Now().Add(handoffWindow)
	return m, nil
}

func (m Model) clearHandoff() Model {
	m.handoffFrom, m.handoffSession, m.handoffReady, m.handoffUntil = "", "", false, time.Time{}
	return m
}

// offerHandoff asks to continue on the best account when the account claude
// exited on is now limited. It waits (up to handoffWindow) until the user is
// back in list mode.
func (m Model) offerHandoff() Model {
	if !m.handoffReady {
		return m
	}
	if m.deps.Now().After(m.handoffUntil) {
		return m.clearHandoff()
	}
	if m.mode != modeList {
		return m
	}
	from, session := m.handoffFrom, m.handoffSession
	m = m.clearHandoff()
	r, ok := m.rowFor(from)
	best := m.bestName()
	if !ok || status.LimitedWindow(r) == "" || best == "" || best == from {
		return m
	}
	text := fmt.Sprintf("Continue this conversation on %s? [y/N]", best)
	return m.ask(confirm{text: text, yes: target{name: best, resume: true, session: session}})
}
