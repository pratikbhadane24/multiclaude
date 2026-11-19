package dash

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const osascriptPath = "/usr/bin/osascript"

// shellQuote single-quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// appleScriptString returns s as a double-quoted AppleScript string literal.
func appleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// NewWindowArgv is the argv that opens a new Terminal.app window running
// `ccm <name>`.
func NewWindowArgv(selfPath, name string) []string {
	shell := shellQuote(selfPath) + " " + shellQuote(name)
	script := `tell application "Terminal" to do script ` + appleScriptString(shell)
	return []string{osascriptPath, "-e", script}
}

func (m Model) collectCmd() tea.Cmd {
	ctx, collect, now, show := m.ctx, m.deps.Collect, m.deps.Now, m.deps.ShowDuplicates
	started := now()
	return func() tea.Msg {
		return snapshotMsg{snap: collect(ctx, show), started: started, at: now(), showDuplicates: show}
	}
}

func (m Model) tickCmd() tea.Cmd {
	return m.deps.Tick(m.deps.Interval, func(time.Time) tea.Msg { return tickMsg{} })
}

// execCmd hands the terminal to c; label names it in the result toast.
func (m Model) execCmd(c *exec.Cmd, label string) tea.Cmd {
	return m.deps.Exec(c, func(err error) tea.Msg { return execDoneMsg{label: label, err: err} })
}

// launch hands the terminal to claude as name with extra args; its exit is
// reported with the account so a handoff can be offered.
func (m Model) launch(name string, extra []string, session string) (Model, tea.Cmd) {
	c, warning, err := m.deps.Launch(name, extra)
	if err != nil {
		return m.withToast(fmt.Sprintf("launch %s: %v", name, err)), nil
	}
	label := "claude (" + name + ")"
	cmd := m.deps.Exec(c, func(err error) tea.Msg {
		return execDoneMsg{label: label, account: name, session: session, err: err}
	})
	toast := "launched claude as " + name
	if warning != "" {
		toast = "ignoring config: " + warning + " (default args not applied)"
	}
	return m.withToast(toast), cmd
}

func (m Model) launchBest() (Model, tea.Cmd) {
	name := m.bestName()
	if name == "" {
		return m.withToast("no usable account right now"), nil
	}
	return m.launchNew(name)
}

// userPicksSession reports whether launch args already choose a conversation.
func userPicksSession(args []string) bool {
	for _, a := range args {
		switch strings.SplitN(a, "=", 2)[0] {
		case "--resume", "-r", "--continue", "-c", "--session-id":
			return true
		}
	}
	return false
}

// launchNew starts a new conversation as name, with an explicit session id so
// a later handoff resumes exactly that chat (unless the configured args
// already choose one).
func (m Model) launchNew(name string) (Model, tea.Cmd) {
	if m.deps.NewSessionID == nil {
		return m.launch(name, nil, "")
	}
	if args, _ := m.deps.ArgsFor(name); userPicksSession(args) {
		return m.launch(name, nil, "")
	}
	id := m.deps.NewSessionID()
	return m.launch(name, []string{sessionIDFlag, id}, id)
}

func (m Model) newWindow(name string) tea.Cmd {
	ctx, run, argv := m.ctx, m.deps.Run, NewWindowArgv(m.deps.SelfPath, name)
	return func() tea.Msg {
		if err := run(ctx, argv[0], argv[1:]); err != nil {
			return toastMsg{text: "new window: " + err.Error()}
		}
		return toastMsg{text: "opened " + name + " in a new Terminal window"}
	}
}

func (m Model) doctorCmd() tea.Cmd {
	ctx, run := m.ctx, m.deps.Doctor
	return func() tea.Msg { return doctorMsg{checks: run(ctx)} }
}

// textCmd runs an in-process command and toasts its output.
func (m Model) textCmd(label string, fn func(ctx context.Context) (string, error)) tea.Cmd {
	ctx := m.ctx
	return func() tea.Msg {
		out, err := fn(ctx)
		text := label + ": " + oneLine(out)
		if err != nil {
			text = label + " failed: " + err.Error() + " " + oneLine(out)
		}
		return toastMsg{job: label, text: strings.TrimSpace(text)}
	}
}

// oneLine joins output lines with " · ".
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(strings.TrimSpace(s), "\n", " · ")), " ")
}
