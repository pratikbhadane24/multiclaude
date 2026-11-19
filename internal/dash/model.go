package dash

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"multiclaude/internal/doctor"
	"multiclaude/internal/status"
)

type mode int

const (
	modeList mode = iota
	modeHelp
	modeDoctor
	modeAddName
	modeAddEmail
	modeConfirm
)

const (
	defaultWidth  = 80
	defaultHeight = 24
	inputWidth    = 40
)

// Model is the dashboard state. It is a value: Update returns a new Model.
type Model struct {
	ctx  context.Context
	deps Deps

	snap Snapshot
	// args caches each row's launch args, loaded once per snapshot.
	args    map[string]accountArgs
	loaded  bool
	loading bool
	// pendingCollect asks for one more collection once the one in flight
	// ends (A pressed meanwhile); collections never overlap.
	pendingCollect bool
	lastRefresh    time.Time
	selected       int

	width, height int
	mode          mode
	toast         string

	doctorChecks []doctor.Check
	doctorAt     time.Time
	pane         viewport.Model

	input   textinput.Model
	addName string

	// running holds in-flight background jobs (doctor, sync, t3-sync).
	running map[string]bool

	// prompt is the open [y/N] question (modeConfirm).
	prompt confirm
	// handoffFrom is the account claude just exited on; the first snapshot
	// collected after handoffAfter decides whether to offer a handoff.
	handoffFrom string
	// handoffSession is the session id that launch started ("" = look up the
	// latest idle one).
	handoffSession string
	handoffAfter   time.Time
	// handoffReady is set once a fresh snapshot landed: the offer is then
	// shown as soon as the list is visible, until handoffUntil.
	handoffReady bool
	handoffUntil time.Time
}

// New returns a dashboard model; ctx bounds in-process work.
func New(ctx context.Context, d Deps) Model {
	in := textinput.New()
	in.Width = inputWidth
	return Model{
		ctx:     ctx,
		deps:    withDefaults(d),
		loading: true, // Init starts the first collection

		width:  defaultWidth,
		height: defaultHeight,
		input:  in,
		pane:   viewport.New(defaultWidth, defaultHeight),
	}
}

// Init starts the first collection and the auto-refresh timer.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.collectCmd(), m.tickCmd())
}

// Update handles one message, then shows a pending handoff offer if the list
// is visible.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.step(msg)
	if nm, ok := next.(Model); ok {
		return nm.offerHandoff(), cmd
	}
	return next, cmd
}

func (m Model) step(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.resized(msg.Width, msg.Height), nil
	case snapshotMsg:
		if msg.showDuplicates != m.deps.ShowDuplicates {
			return m.afterStaleSnapshot()
		}
		m.pendingCollect = false
		return m.withSnapshot(msg).afterSnapshot(msg)
	case tickMsg:
		return m.onTick()
	case execDoneMsg:
		return m.onExecDone(msg)
	case doctorMsg:
		return m.withJob(jobDoctor, false).withDoctor(msg.checks), nil
	case toastMsg:
		return m.withJob(msg.job, false).withToast(msg.text), nil
	case tea.KeyMsg:
		return m.onKey(msg)
	}
	return m.forward(msg)
}

// forward passes other messages (e.g. cursor blink) to the active widget.
func (m Model) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.mode {
	case modeAddName, modeAddEmail:
		m.input, cmd = m.input.Update(msg)
	case modeDoctor, modeHelp:
		m.pane, cmd = m.pane.Update(msg)
	}
	return m, cmd
}

// resized applies a window size; a zero size (some ptys) keeps the defaults.
func (m Model) resized(w, h int) Model {
	if w <= 0 || h <= 0 {
		return m
	}
	m.width, m.height = w, h
	m.pane.Width, m.pane.Height = w, max(h-paneChrome, 1)
	return m
}

func (m Model) withToast(text string) Model {
	m.toast = text
	return m
}

// accountArgs is one account's launch args, or why they could not be read.
type accountArgs struct {
	args []string
	err  error
}

// loadArgs reads every row's launch args (one config load each).
func (m Model) loadArgs(rows []status.Row) map[string]accountArgs {
	out := make(map[string]accountArgs, len(rows))
	for _, r := range rows {
		args, err := m.deps.ArgsFor(r.Account)
		out[r.Account] = accountArgs{args: args, err: err}
	}
	return out
}

// argsFor returns name's launch args as of the last snapshot.
func (m Model) argsFor(name string) ([]string, error) {
	if c, ok := m.args[name]; ok {
		return c.args, c.err
	}
	return m.deps.ArgsFor(name)
}

func (m Model) withSnapshot(msg snapshotMsg) Model {
	m.loading = false
	if msg.snap.Err != nil && m.loaded {
		// Keep the last good rows; just say what went wrong.
		return m.withToast("refresh failed: " + msg.snap.Err.Error())
	}
	name := m.selectedName()
	m.snap, m.loaded, m.lastRefresh = msg.snap, true, msg.at
	m.args = m.loadArgs(msg.snap.Rows)
	m.selected = 0
	for i, r := range m.snap.Rows {
		if r.Account == name {
			m.selected = i
		}
	}
	if msg.snap.CacheErr != nil {
		return m.withToast("usage cache: " + msg.snap.CacheErr.Error())
	}
	return m
}

// afterStaleSnapshot drops a snapshot collected with the old duplicates
// setting and, if A asked for it, starts the one collection that follows.
func (m Model) afterStaleSnapshot() (Model, tea.Cmd) {
	if !m.pendingCollect {
		return m, nil // a fresh collect is already in flight
	}
	m.pendingCollect, m.loading = false, true
	return m, m.collectCmd()
}

func (m Model) refresh() (Model, tea.Cmd) { return m.refreshWith(false) }

// refreshWith starts a collection unless one is in flight; force (the r key)
// refetches recent results too, but never an account that is backing off.
func (m Model) refreshWith(force bool) (Model, tea.Cmd) {
	if m.loading {
		return m, nil
	}
	m.loading = true
	return m, m.collectWith(force)
}

func (m Model) onTick() (tea.Model, tea.Cmd) {
	next, cmd := m.refresh()
	return next, tea.Batch(cmd, m.tickCmd())
}

func (m Model) onExecDone(msg execDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m = m.withToast(msg.label + " exited: " + msg.err.Error())
	} else {
		m = m.withToast(msg.label + " finished")
	}
	if msg.account != "" {
		m.handoffFrom, m.handoffAfter, m.handoffSession = msg.account, m.deps.Now(), msg.session
	}
	return m.refresh()
}

func (m Model) withDoctor(checks []doctor.Check) Model {
	m.doctorChecks, m.doctorAt = checks, m.deps.Now()
	_, text := doctor.Summary(checks)
	m.pane.SetContent(text)
	m.pane.GotoTop()
	m.mode = modeDoctor
	m.toast = ""
	return m
}

func (m Model) selectedName() string {
	if m.selected < 0 || m.selected >= len(m.snap.Rows) {
		return ""
	}
	return m.snap.Rows[m.selected].Account
}

// withJob returns m with job marked running or finished (copying the set).
func (m Model) withJob(job string, on bool) Model {
	if job == "" {
		return m
	}
	next := make(map[string]bool, len(m.running)+1)
	for k, v := range m.running {
		next[k] = v
	}
	if on {
		next[job] = true
	} else {
		delete(next, job)
	}
	m.running = next
	return m
}
