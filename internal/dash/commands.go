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
	ctx, collect, now := m.ctx, m.deps.Collect, m.deps.Now
	return func() tea.Msg {
		return snapshotMsg{snap: collect(ctx), at: now()}
	}
}

func (m Model) tickCmd() tea.Cmd {
	return m.deps.Tick(m.deps.Interval, func(time.Time) tea.Msg { return tickMsg{} })
}

// execCmd hands the terminal to c; label names it in the result toast.
func (m Model) execCmd(c *exec.Cmd, label string) tea.Cmd {
	return m.deps.Exec(c, func(err error) tea.Msg { return execDoneMsg{label: label, err: err} })
}

func (m Model) launch(name string) (Model, tea.Cmd) {
	c, err := m.deps.Launch(name)
	if err != nil {
		return m.withToast(fmt.Sprintf("launch %s: %v", name, err)), nil
	}
	return m.withToast("launched claude as " + name), m.execCmd(c, "claude ("+name+")")
}

func (m Model) launchBest() (Model, tea.Cmd) {
	name := m.bestName()
	if name == "" {
		return m.withToast("no usable account right now"), nil
	}
	return m.launch(name)
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
		return toastMsg{text: strings.TrimSpace(text)}
	}
}

// oneLine joins output lines with " · ".
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(strings.TrimSpace(s), "\n", " · ")), " ")
}
