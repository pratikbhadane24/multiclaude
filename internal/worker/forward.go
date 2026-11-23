package worker

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// defaultPollEvery is how often the worker is checked for the login's
// callback port.
const defaultPollEvery = time.Second

// listenScript lists TCP ports the worker user's processes listen on.
const listenScript = `lsof -nP -iTCP -sTCP:LISTEN 2>/dev/null || true`

// parsePorts extracts the sorted, unique listening ports from lsof output.
func parsePorts(out string) []int {
	seen := map[int]bool{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[len(fields)-1] != "(LISTEN)" {
			continue
		}
		addr := fields[len(fields)-2]
		if p, err := strconv.Atoi(addr[strings.LastIndex(addr, ":")+1:]); err == nil {
			seen[p] = true
		}
	}
	ports := make([]int, 0, len(seen))
	for p := range seen {
		ports = append(ports, p)
	}
	slices.Sort(ports)
	return ports
}

func (s *Setup) listenPorts(ctx context.Context) ([]int, error) {
	res, err := s.R.Run(ctx, listenScript)
	if err != nil {
		return nil, err
	}
	return parsePorts(res.Stdout), nil
}

// withCallbackForward runs login while tunnelling every port the worker
// starts listening on during it to the same port on this machine. Claude's
// sign-in returns the browser to http://localhost:<random port>/callback,
// which on the hub must reach the claude waiting on the worker.
func (s *Setup) withCallbackForward(ctx context.Context, login func(context.Context) error) error {
	before, err := s.listenPorts(ctx)
	if err != nil {
		return err
	}
	watchCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	var stops []func()
	var mu sync.Mutex
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.forwardNewPorts(watchCtx, before, func(stop func()) {
			mu.Lock()
			stops = append(stops, stop)
			mu.Unlock()
		})
	}()
	err = login(ctx)
	cancel()
	wg.Wait()
	for _, stop := range stops {
		stop()
	}
	return err
}

// forwardNewPorts polls until ctx ends, forwarding each port not in before.
func (s *Setup) forwardNewPorts(ctx context.Context, before []int, keep func(stop func())) {
	every := s.pollEvery
	if every == 0 {
		every = defaultPollEvery
	}
	done := map[int]bool{}
	for _, p := range before {
		done[p] = true
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		ports, err := s.listenPorts(ctx)
		if err != nil {
			continue
		}
		for _, p := range ports {
			if done[p] {
				continue
			}
			done[p] = true
			if stop, err := s.R.Forward(ctx, p); err == nil {
				keep(stop)
			}
		}
	}
}

// Forward tunnels localhost:port on this machine to localhost:port on the
// worker until the returned stop is called.
func (s SSHRemote) Forward(ctx context.Context, port int) (func(), error) {
	spec := fmt.Sprintf("%d:localhost:%d", port, port)
	cmd := exec.Command("ssh", append(sshOpts(), "-N", "-o", "ExitOnForwardFailure=yes", "-L", spec, s.Host)...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("forward port %d: %w", port, err)
	}
	return func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}, nil
}
