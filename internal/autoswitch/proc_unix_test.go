//go:build unix

package autoswitch

import (
	"bytes"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

func devNull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestStartProcessExitCode(t *testing.T) {
	var out bytes.Buffer
	p, err := StartProcess(Stdio{In: devNull(t), Out: &out, Err: &out}, "/bin/sh", []string{"-c", "echo hi; exit 3"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if code := p.Wait(); code != 3 || out.String() != "hi\n" {
		t.Fatalf("code=%d out=%q", code, out.String())
	}
}

func TestStartProcessSignalReachesGroup(t *testing.T) {
	// The shell's child (sleep) is in the same group, so the group signal
	// ends both and Wait returns promptly.
	p, err := StartProcess(Stdio{In: devNull(t)}, "/bin/sh", []string{"-c", "sleep 30; exit 0"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() { done <- p.Wait() }()
	time.Sleep(50 * time.Millisecond)

	if err := p.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	select {
	case code := <-done:
		if code != 128+int(syscall.SIGTERM) {
			t.Fatalf("code=%d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit")
	}
}

func TestStartProcessErrors(t *testing.T) {
	if _, err := StartProcess(Stdio{In: devNull(t)}, "/nonexistent/claude", nil, nil); err == nil {
		t.Fatal("want start error")
	}
	if _, err := StartProcess(Stdio{}, "/bin/true", nil, nil); err == nil {
		t.Fatal("want stdin error")
	}
}

// waitCode runs p.Wait in the background and fails after 5s.
func waitCode(t *testing.T, p Proc) int {
	t.Helper()
	done := make(chan int, 1)
	go func() { done <- p.Wait() }()
	select {
	case code := <-done:
		return code
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return: stopped child not handled")
	}
	return 0
}

// stopScript stops itself the way claude does on Ctrl-Z, then finishes.
const stopScript = "kill -STOP $$; echo resumed; exit 4"

func TestWaitContinuesAStoppedChild(t *testing.T) {
	// Arrange: no terminal, so the child is simply continued.
	var out bytes.Buffer
	p, err := StartProcess(Stdio{In: devNull(t), Out: &out, Err: &out}, "/bin/sh", []string{"-c", stopScript}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Act
	code := waitCode(t, p)

	// Assert
	if code != 4 || out.String() != "resumed\n" {
		t.Fatalf("code=%d out=%q", code, out.String())
	}
}

// recordingJobs is a jobControl fake that records calls.
type recordingJobs struct {
	mu      sync.Mutex
	stopped []int
	done    int
}

func (j *recordingJobs) Stopped(pgid int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.stopped = append(j.stopped, pgid)
}

func (j *recordingJobs) Done() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.done++
}

func TestWaitHandsStopsToJobControl(t *testing.T) {
	// Arrange
	jobs := &recordingJobs{}
	var out bytes.Buffer
	p, err := startProcess(Stdio{In: devNull(t), Out: &out, Err: &out}, "/bin/sh",
		[]string{"-c", "kill -TTIN $$; kill -STOP $$; exit 0"}, nil, false, jobs)
	if err != nil {
		t.Fatal(err)
	}
	pid := p.(*osProc).pid

	// Act
	code := waitCode(t, p)

	// Assert
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if code != 0 || len(jobs.stopped) != 2 || jobs.stopped[0] != pid || jobs.done != 1 {
		t.Fatalf("code=%d stopped=%v pid=%d done=%d", code, jobs.stopped, pid, jobs.done)
	}
}

func TestStartProcessFileStdoutIsInherited(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	p, err := StartProcess(Stdio{In: devNull(t), Out: f, Err: f}, "/bin/sh", []string{"-c", "echo direct"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if code := waitCode(t, p); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if data, _ := os.ReadFile(f.Name()); string(data) != "direct\n" {
		t.Fatalf("out=%q", data)
	}
}
