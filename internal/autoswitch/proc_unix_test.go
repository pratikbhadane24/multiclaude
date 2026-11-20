//go:build unix

package autoswitch

import (
	"bytes"
	"os"
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
