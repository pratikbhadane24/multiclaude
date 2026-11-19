// Package tokens totals today's token usage from Claude Code transcripts.
package tokens

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxLineBytes   = 16 * 1024 * 1024
	initialBufSize = 64 * 1024
	syntheticModel = "<synthetic>"
	assistantType  = "assistant"
)

// Tokens is a set of token counters.
type Tokens struct {
	Input, Output, CacheRead, CacheWrite int64
}

func (t Tokens) add(o Tokens) Tokens {
	return Tokens{t.Input + o.Input, t.Output + o.Output, t.CacheRead + o.CacheRead, t.CacheWrite + o.CacheWrite}
}

func (t Tokens) max(o Tokens) Tokens {
	return Tokens{max(t.Input, o.Input), max(t.Output, o.Output), max(t.CacheRead, o.CacheRead), max(t.CacheWrite, o.CacheWrite)}
}

type transcriptLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId"`
	Message   *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input      int64 `json:"input_tokens"`
			Output     int64 `json:"output_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// Today sums token usage for assistant lines timestamped on the same local
// calendar date as now, across *.jsonl files under projectsDir.
func Today(projectsDir string, now time.Time) (Tokens, error) {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	keyed := map[string]Tokens{}
	var unkeyed Tokens

	err := filepath.WalkDir(projectsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(midnight) {
			return nil
		}
		return scanFile(path, now, keyed, &unkeyed)
	})
	if err != nil {
		return Tokens{}, fmt.Errorf("walk %s: %w", projectsDir, err)
	}

	total := unkeyed
	for _, t := range keyed {
		total = total.add(t)
	}
	return total, nil
}

func scanFile(path string, now time.Time, keyed map[string]Tokens, unkeyed *Tokens) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, initialBufSize)
	for {
		raw, ok, err := readLine(r)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if !ok {
			continue
		}
		key, tk, counts := parseLine(raw, now)
		if !counts {
			continue
		}
		if key == "" {
			*unkeyed = unkeyed.add(tk)
			continue
		}
		keyed[key] = keyed[key].max(tk)
	}
}

// readLine reads one line; ok is false when the line exceeded maxLineBytes
// and was skipped (the rest of it is consumed so later lines still count).
func readLine(r *bufio.Reader) (line []byte, ok bool, err error) {
	var buf []byte
	tooLong := false
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, false, err
		}
		if !tooLong {
			buf = append(buf, chunk...)
			if len(buf) > maxLineBytes {
				tooLong, buf = true, nil
			}
		}
		if !isPrefix {
			return buf, !tooLong, nil
		}
	}
}

// parseLine returns the dedupe key ("" if none), usage, and whether the line counts.
func parseLine(raw []byte, now time.Time) (string, Tokens, bool) {
	var l transcriptLine
	if json.Unmarshal(raw, &l) != nil {
		return "", Tokens{}, false
	}
	if l.Type != assistantType || l.Message == nil || l.Message.Usage == nil || l.Message.Model == syntheticModel {
		return "", Tokens{}, false
	}
	ts, err := time.Parse(time.RFC3339Nano, l.Timestamp)
	if err != nil || !sameLocalDay(ts.In(now.Location()), now) {
		return "", Tokens{}, false
	}
	u := l.Message.Usage
	tk := Tokens{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite}
	if l.Message.ID == "" && l.RequestID == "" {
		return "", tk, true
	}
	return l.Message.ID + ":" + l.RequestID, tk, true
}

func sameLocalDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
