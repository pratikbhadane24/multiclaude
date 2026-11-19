package dash

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"multiclaude/internal/registry"
)

const noAccountText = "no account yet — press a to add one"

// keyHelp lists every key in footer order: key, short label, help text.
var keyHelp = []struct{ key, label, help string }{
	{"↑↓", "select", "move the selection (also j / k)"},
	{"⏎", "launch", "run claude as the selected account here; you return here when it exits. A limited account offers the best one instead; if it hits its limit while you work, you're offered to continue the chat on another account"},
	{"b", "best", "run claude as the account with the most headroom"},
	{"n", "new window", "open the selected account in a new Terminal window"},
	{"l", "login", "log the selected account in (browser)"},
	{"a", "add", "add a new account (asks for name and email)"},
	{"d", "doctor", "check the setup and show problems"},
	{"s", "sync", "link shared config and sync MCP servers"},
	{"t", "t3-sync", "add every account to T3 Code"},
	{"A", "duplicates", "show or hide accounts signed in as the same login as another (hidden ones are merged into the first, shown as main (= admin))"},
	{"r", "refresh", "refresh usage now (auto every 30s)"},
	{"?", "help", "show or hide this help"},
	{"q", "quit", "leave the dashboard (also Ctrl-C)"},
}

func (m Model) onKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch m.mode {
	case modeHelp:
		return m.onHelpKey(k)
	case modeDoctor:
		return m.onDoctorKey(k)
	case modeAddName, modeAddEmail:
		return m.onInputKey(k)
	case modeConfirm:
		return m.onConfirmKey(k)
	}
	return m.onListKey(k)
}

func (m Model) onListKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.toast = ""
	switch k.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		m.selected = max(m.selected-1, 0)
	case "down", "j":
		m.selected = max(min(m.selected+1, len(m.snap.Rows)-1), 0)
	case "?":
		m.mode = modeHelp
		m.pane.SetContent(strings.Join(m.helpLines(), "\n"))
		m.pane.GotoTop()
	case "A":
		return m.toggleDuplicates()
	case "r":
		next, cmd := m.refresh()
		return next.withToast("refreshing…"), cmd
	case "b":
		return m.launchBest()
	case "a":
		return m.startAdd()
	case "d":
		return m.startJob(jobDoctor, "running doctor…", m.doctorCmd())
	case "s":
		return m.startJob(jobSync, "syncing…", m.textCmd(jobSync, m.deps.Sync))
	case "t":
		return m.startJob(jobT3Sync, "updating T3 Code…", m.textCmd(jobT3Sync, m.deps.T3Sync))
	default:
		return m.onAccountKey(k.String())
	}
	return m, nil
}

// toggleDuplicates flips duplicate rows and re-collects. A collection already
// in flight is never joined by a second one: its snapshot is dropped when it
// arrives (it no longer matches the setting) and one fresh collect follows.
func (m Model) toggleDuplicates() (tea.Model, tea.Cmd) {
	m.deps.ShowDuplicates = !m.deps.ShowDuplicates
	text := "hiding duplicate logins…"
	if m.deps.ShowDuplicates {
		text = "showing duplicate logins…"
	}
	if m.loading {
		m.pendingCollect = true
		return m.withToast(text), nil
	}
	m.loading = true
	return m.withToast(text), m.collectCmd()
}

// onAccountKey handles keys that act on the selected account.
func (m Model) onAccountKey(k string) (tea.Model, tea.Cmd) {
	if k != "enter" && k != "n" && k != "l" {
		return m, nil
	}
	name := m.selectedName()
	if name == "" {
		return m.withToast(noAccountText), nil
	}
	switch k {
	case "enter":
		return m.enter(name)
	case "n":
		return m.withToast("opening a new Terminal window…"), m.newWindow(name)
	}
	return m.withToast("logging in " + name + "…"), m.execCmd(m.deps.Self("login", name), "login "+name)
}

// startJob runs a background job unless the same job is still running.
func (m Model) startJob(job, toast string, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if m.running[job] {
		return m.withToast(job + " is already running…"), nil
	}
	return m.withJob(job, true).withToast(toast), cmd
}

func (m Model) onHelpKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "?", "esc", "q", "enter":
		m.mode = modeList
		return m, nil
	}
	var cmd tea.Cmd
	m.pane, cmd = m.pane.Update(k)
	return m, cmd
}

func (m Model) onDoctorKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc", "q", "d":
		m.mode = modeList
		return m, nil
	}
	var cmd tea.Cmd
	m.pane, cmd = m.pane.Update(k)
	return m, cmd
}

func (m Model) startAdd() (tea.Model, tea.Cmd) {
	m.mode = modeAddName
	m.input = newInput("name (a-z, 0-9, -)")
	return m, textinput.Blink
}

func newInput(placeholder string) textinput.Model {
	in := textinput.New()
	in.Placeholder = placeholder
	in.Width = inputWidth
	in.Focus()
	return in
}

func (m Model) onInputKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		m.mode = modeList
		return m.withToast("add cancelled"), nil
	case tea.KeyEnter:
		return m.submitInput()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func (m Model) submitInput() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(m.input.Value())
	if m.mode == modeAddName {
		if err := registry.ValidateName(value); err != nil {
			return m.withToast(err.Error()), nil
		}
		m.addName, m.mode, m.toast = value, modeAddEmail, ""
		m.input = newInput("email of the Claude account")
		return m, textinput.Blink
	}
	if !strings.Contains(value, "@") {
		return m.withToast("enter the account's email address"), nil
	}
	m.mode = modeList
	c := m.deps.Self("add", m.addName, "--email", value)
	return m.withToast("adding " + m.addName + "…"), m.execCmd(c, "add "+m.addName)
}
