package worker

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"
)

// DefaultStatusTimeout bounds how long one worker may take to answer.
const DefaultStatusTimeout = 10 * time.Second

// Health is one worker's state as shown by `ccm worker status`. It never
// holds credentials.
type Health struct {
	Name        string          `json:"name"`
	Reachable   bool            `json:"reachable"`
	Load1       float64         `json:"load1"`
	Cores       int             `json:"cores"`
	FreeMemGB   float64         `json:"freeMemGB"`
	ClaudeProcs int             `json:"claudeProcs"`
	CCMVersion  string          `json:"ccmVersion"`
	Accounts    map[string]bool `json:"accounts"`
	Err         string          `json:"error,omitempty"`
}

// healthScript prints key=value lines; free memory counts free, inactive
// and speculative pages (what macOS can hand out without swapping).
const healthScript = `export PATH="$HOME/.local/bin:$PATH"
echo "load=$(sysctl -n vm.loadavg | awk '{print $2}')"
echo "cores=$(sysctl -n hw.ncpu)"
ps=$(sysctl -n hw.pagesize)
echo "freemem=$(vm_stat | awk -v ps="$ps" '/Pages (free|inactive|speculative)/ {gsub("\\.","",$NF); n+=$NF} END {printf "%.1f", n*ps/1073741824}')"
echo "procs=$(pgrep -x claude | wc -l | tr -d ' ')"
echo "ccm=$(ccm version 2>/dev/null)"
echo "accounts=$(ccm list --json 2>/dev/null | tr -d '\n')"`

// Collect checks every worker in parallel, each bounded by timeout, and
// returns results in input order. An unreachable worker never blocks the
// others.
func Collect(ctx context.Context, ws []Worker, mk func(host string) Remote, timeout time.Duration) []Health {
	out := make([]Health, len(ws))
	var wg sync.WaitGroup
	for i, w := range ws {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = collectOne(ctx, w, mk(w.SSHHost), timeout)
		}()
	}
	wg.Wait()
	return out
}

func collectOne(ctx context.Context, w Worker, r Remote, timeout time.Duration) Health {
	h := Health{Name: w.Name}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type answer struct {
		out string
		err error
	}
	ch := make(chan answer, 1)
	go func() {
		res, err := r.Run(ctx, healthScript)
		ch <- answer{res.Stdout, err}
	}()
	select {
	case <-ctx.Done():
		h.Err = "unreachable (timed out)"
		return h
	case a := <-ch:
		if a.err != nil {
			h.Err = a.err.Error()
			return h
		}
		return parseHealth(h, a.out)
	}
}

func parseHealth(h Health, out string) Health {
	kv := parseKV(out)
	h.Reachable = true
	h.Load1, _ = strconv.ParseFloat(kv["load"], 64)
	h.Cores, _ = strconv.Atoi(kv["cores"])
	h.FreeMemGB, _ = strconv.ParseFloat(kv["freemem"], 64)
	h.ClaudeProcs, _ = strconv.Atoi(kv["procs"])
	h.CCMVersion = kv["ccm"]
	var rows []ListEntry
	if json.Unmarshal([]byte(kv["accounts"]), &rows) == nil {
		h.Accounts = make(map[string]bool, len(rows))
		for _, r := range rows {
			h.Accounts[r.Name] = r.LoggedIn
		}
	}
	return h
}
