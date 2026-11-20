package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"multiclaude/internal/autoswitch"
	"multiclaude/internal/config"
	dashboard "multiclaude/internal/dash"
	"multiclaude/internal/handoff"
	"multiclaude/internal/status"
)

// cliProc is a fake claude: it exits at once with code, or (when it holds a
// limit) waits for a signal.
type cliProc struct {
	done chan int
	once sync.Once
	pid  int
}

// Pid is the fake process id (fakePid(i) for the i-th start).
func (p *cliProc) Pid() int { return p.pid }

// fakePid is the pid of the i-th fake claude start.
func fakePid(i int) int { return 4200 + i }

func (p *cliProc) exit(code int) { p.once.Do(func() { p.done <- code }) }
func (p *cliProc) Wait() int     { return <-p.done }
func (p *cliProc) Signal(sig syscall.Signal) error {
	p.exit(128 + int(sig))
	return nil
}

type procCall struct {
	bin  string
	args []string
	env  []string
}

// procRecorder records starts; onStart decides how each run behaves.
type procRecorder struct {
	mu      sync.Mutex
	calls   []procCall
	onStart func(i int, c procCall, p *cliProc)
}

func (r *procRecorder) start(bin string, args, env []string) (autoswitch.Proc, error) {
	r.mu.Lock()
	i := len(r.calls)
	c := procCall{bin, slices.Clone(args), env}
	r.calls = append(r.calls, c)
	r.mu.Unlock()
	p := &cliProc{done: make(chan int, 1), pid: fakePid(i)}
	if r.onStart != nil {
		r.onStart(i, c, p)
	} else {
		p.exit(0)
	}
	return p, nil
}

func enableAutoSwitch(t *testing.T, e *cliEnv, cfg config.Config) *procRecorder {
	t.Helper()
	next := cfg
	next.AutoSwitch = true
	e.setConfig(t, next)
	rec := &procRecorder{}
	e.app.StartProc = rec.start
	return rec
}

// useCwd points the working directory at a temp folder and returns its real path.
func useCwd(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := getwd
	getwd = func() (string, error) { return dir, nil }
	t.Cleanup(func() { getwd = prev })
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func flagValue(args []string, flag string) string {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}

func TestConfigAutoSwitchSetShowClear(t *testing.T) {
	e := newCLI(t, "")

	if code := e.main("config", "set", "auto-switch", "on"); code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	if code := e.main("config", "set", "auto-switch-prompt", "keep", "going"); code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	c, _ := config.Load(e.p)
	if !c.AutoSwitch || c.AutoSwitchPrompt != "keep going" {
		t.Fatalf("cfg=%+v", c)
	}
	raw, _ := os.ReadFile(filepath.Join(e.p.AccountsRoot, "config.json"))
	if !strings.Contains(string(raw), `"autoSwitch": true`) {
		t.Fatalf("raw=%s", raw)
	}
	e.out.Reset()
	e.main("config", "show")
	if out := e.out.String(); !strings.Contains(out, "auto-switch: on") || !strings.Contains(out, "auto-switch-prompt: keep going") {
		t.Fatalf("out=%s", out)
	}

	e.main("config", "set", "auto-switch", "off")
	e.main("config", "clear", "auto-switch-prompt")
	c, _ = config.Load(e.p)
	if c.AutoSwitch || c.AutoSwitchPrompt != "" {
		t.Fatalf("cfg=%+v", c)
	}
}

func TestConfigAutoSwitchRejectsBadValue(t *testing.T) {
	e := newCLI(t, "")
	if code := e.main("config", "set", "auto-switch", "maybe"); code != ExitUsage {
		t.Fatalf("code=%d", code)
	}
	if code := e.main("config", "set", "auto-switch"); code != ExitUsage {
		t.Fatalf("code=%d", code)
	}
}

func TestAutoSwitchOffLaunchesPlainly(t *testing.T) {
	e := newCLI(t, "")
	rec := &procRecorder{}
	e.app.StartProc = rec.start

	e.main("work")

	if len(rec.calls) != 0 || len(e.callsTo(e.p.RealClaude)) != 1 {
		t.Fatalf("supervised=%d plain=%d", len(rec.calls), len(e.callsTo(e.p.RealClaude)))
	}
}

func TestAutoSwitchNewLaunchPinsSessionID(t *testing.T) {
	e := newCLI(t, "")
	useCwd(t)
	rec := enableAutoSwitch(t, e, config.Config{DefaultArgs: []string{"--d"}})

	code := e.main("work", "--model", "opus")

	if code != ExitOK || len(rec.calls) != 1 {
		t.Fatalf("code=%d calls=%+v err=%s", code, rec.calls, e.err)
	}
	c := rec.calls[0]
	if c.args[0] != "--d" || c.args[1] != "--model" || len(flagValue(c.args, "--session-id")) != 36 {
		t.Fatalf("args=%q", c.args)
	}
	if !envHas(c.env, "CLAUDE_CONFIG_DIR="+e.workDir) || len(e.callsTo(e.p.RealClaude)) != 0 {
		t.Fatalf("env/plain launch wrong: %v", e.fake.Calls)
	}
}

func TestAutoSwitchContinueResolvesLatest(t *testing.T) {
	e := newCLI(t, "")
	cwd := useCwd(t)
	rec := enableAutoSwitch(t, e, config.Config{})
	id := "11111111-2222-4333-8444-555555555555"
	mustWrite(t, filepath.Join(e.workDir, "projects", handoff.Slug(cwd), id+".jsonl"), `{"cwd":"x"}`+"\n")

	e.main("work", "--continue")

	if len(rec.calls) != 1 || !slices.Equal(rec.calls[0].args, []string{"--resume", id}) {
		t.Fatalf("calls=%+v err=%s", rec.calls, e.err)
	}
}

func TestAutoSwitchUnknownSessionFallsBackToPlainLaunch(t *testing.T) {
	e := newCLI(t, "")
	useCwd(t)
	rec := enableAutoSwitch(t, e, config.Config{})

	e.main("work", "--resume")

	if len(rec.calls) != 0 || len(e.callsTo(e.p.RealClaude)) != 1 || !strings.Contains(e.err.String(), "auto-switch off for this launch") {
		t.Fatalf("supervised=%d err=%s", len(rec.calls), e.err)
	}
}

// writeUsageCache stores a cached usage entry per account.
func writeUsageCache(t *testing.T, e *cliEnv, entries ...status.CacheAccount) {
	t.Helper()
	data, err := json.Marshal(status.Cache{Accounts: entries})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, status.CachePath(e.p), string(data))
}

func TestAutoSwitchLimitResumesOnNextAccount(t *testing.T) {
	// Arrange
	e := newCLI(t, "")
	cwd := useCwd(t)
	rec := enableAutoSwitch(t, e, config.Config{DefaultArgs: []string{"--d"}})
	fetched, low := testNow.Add(-time.Minute), 10.0
	writeUsageCache(t, e,
		status.CacheAccount{Name: "main", FiveHourUsed: &low, FetchedAt: &fetched, State: "ok"},
		status.CacheAccount{Name: "work", FiveHourUsed: &low, FetchedAt: &fetched, State: "ok"},
	)
	limitLine := `{"type":"assistant","isApiErrorMessage":true,"error":"rate_limit","message":{"content":[{"type":"text","text":"You've hit your 5-hour limit · resets 3pm (UTC)"}]}}` + "\n"
	rec.onStart = func(i int, c procCall, p *cliProc) {
		if i > 0 {
			p.exit(0)
			return
		}
		id := flagValue(c.args, "--session-id")
		mustWrite(t, filepath.Join(e.workDir, "projects", handoff.Slug(cwd), id+".jsonl"), limitLine)
	}

	// Act
	code := e.main("work")

	// Assert
	if code != ExitOK || len(rec.calls) != 2 {
		t.Fatalf("code=%d calls=%d err=%s", code, len(rec.calls), e.err)
	}
	id := flagValue(rec.calls[0].args, "--session-id")
	second := rec.calls[1]
	want := []string{"--d", "--resume", id, autoswitch.BuildResumePrompt("work", "main", false, "")}
	if !slices.Equal(second.args, want) || envHasKey(second.env, "CLAUDE_CONFIG_DIR") {
		t.Fatalf("relaunch args=%q env=%v", second.args, second.env)
	}
	if !strings.Contains(e.err.String(), "ccm: work hit its limit — continuing on main") {
		t.Fatalf("err=%s", e.err)
	}
	cache, _ := status.ReadCache(e.p)
	for _, a := range cache.Accounts {
		if a.Name == "work" && (a.State != status.StateLimited || *a.FiveHourUsed != 100) {
			t.Fatalf("work not marked: %+v", a)
		}
	}
}

func TestDashLaunchRunsSupervisorWhenAutoSwitchOn(t *testing.T) {
	e := newCLI(t, "")
	enableAutoSwitch(t, e, config.Config{})

	c, warning, err := e.app.LaunchCmd("work", []string{"--session-id", "x"})

	if err != nil || warning != "" || c.Path != "/bin/ccm" || !slices.Equal(c.Args[1:], []string{"run", "work", "--session-id", "x"}) {
		t.Fatalf("cmd=%v warning=%q err=%v", c, warning, err)
	}
}

func TestDashCollectReportsAutoSwitch(t *testing.T) {
	e := newCLI(t, "")
	enableAutoSwitch(t, e, config.Config{})
	snap := e.app.dashCollect(context.Background(), dashboard.CollectOptions{})

	if !snap.AutoSwitch {
		t.Fatal("AutoSwitch not reported")
	}
}
