package worker

import (
	"context"
	"io"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/run"
)

func TestParsePorts(t *testing.T) {
	out := "COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME\n" +
		"2.1.330 10 u 20u IPv4 0x1 0t0 TCP 127.0.0.1:52177 (LISTEN)\n" +
		"rapportd 11 u 5u IPv6 0x2 0t0 TCP *:49152 (LISTEN)\n" +
		"node 12 u 7u IPv6 0x3 0t0 TCP [::1]:3000 (LISTEN)\n"

	got := parsePorts(out)

	if !slices.Equal(got, []int{3000, 49152, 52177}) {
		t.Fatalf("ports = %v", got)
	}
}

func TestLoginForwardsOnlyNewPorts(t *testing.T) {
	r := newFakeRemote()
	var calls atomic.Int32
	r.onScript = func(script string) (run.Result, bool) {
		if !strings.Contains(script, "lsof") {
			return run.Result{}, false
		}
		if calls.Add(1) == 1 {
			return run.Result{Stdout: "x 1 u 1u IPv4 0 0t0 TCP *:49152 (LISTEN)\n"}, true
		}
		return run.Result{Stdout: "x 1 u 1u IPv4 0 0t0 TCP *:49152 (LISTEN)\ny 2 u 2u IPv4 0 0t0 TCP 127.0.0.1:52177 (LISTEN)\n"}, true
	}
	s := &Setup{R: r, Out: io.Discard, pollEvery: 10 * time.Millisecond}

	err := s.withCallbackForward(context.Background(), func(context.Context) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !slices.Equal(r.forwards, []int{52177}) || !slices.Equal(r.stopped, []int{52177}) {
		t.Fatalf("forwards=%v stopped=%v", r.forwards, r.stopped)
	}
}
