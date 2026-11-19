// Package history merges an account's own conversation history into the
// shared copy in ~/.claude and replaces it with a symlink. No data is ever
// deleted unless a byte-identical copy already exists in main.
package history

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"multiclaude/internal/consts"
	"multiclaude/internal/paths"
)

const (
	conflictInfix = ".from-"
	transcriptExt = ".jsonl"
	subagentsDir  = "subagents"
)

// MergeResult summarizes one MergeIntoMain call.
type MergeResult struct {
	// Moved counts chat transcripts (*.jsonl, excluding subagent logs) that
	// were moved into main.
	Moved int
	// Conflicts counts differing entries kept as <name>.from-<account>-<unix>.
	Conflicts int
	// Linked is true when the account dir was replaced by a symlink.
	Linked bool
}

// MergeIntoMain merges the real directory <accountDir>/<item> into
// ~/.claude/<item> and replaces it with a symlink. It is a no-op when the
// account entry is missing or already a symlink, or when main has no such
// directory. Entries missing from main are moved; identical files are
// dropped; differing ones are kept in main under a unique ".from-" name.
func MergeIntoMain(p paths.Paths, accountDir, item string, now time.Time) (MergeResult, error) {
	if !slices.Contains(consts.HistoryItems, item) {
		return MergeResult{}, fmt.Errorf("merge %q: not a history item", item)
	}
	main := filepath.Join(p.ClaudeDir, item)
	acct := filepath.Join(accountDir, item)
	if fi, err := os.Lstat(main); err != nil || !fi.IsDir() {
		return MergeResult{}, nil
	}
	fi, err := os.Lstat(acct)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !fi.IsDir()) {
		return MergeResult{}, nil
	}
	if err != nil {
		return MergeResult{}, fmt.Errorf("stat %s: %w", acct, err)
	}
	m := merger{tag: conflictInfix + filepath.Base(accountDir) + "-" + strconv.FormatInt(now.Unix(), 10)}
	if err := m.dir(acct, main); err != nil {
		return m.res, err
	}
	if err := os.Remove(acct); err != nil {
		return m.res, fmt.Errorf("account %s not empty after merge: %w", acct, err)
	}
	if err := os.Symlink(main, acct); err != nil {
		return m.res, fmt.Errorf("link %s: %w", acct, err)
	}
	m.res.Linked = true
	return m.res, nil
}

type merger struct {
	tag string
	res MergeResult
}

// dir moves every child of src into dst, then leaves src empty.
func (m *merger) dir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	for _, e := range entries {
		if err := m.entry(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (m *merger) entry(s, d string) error {
	dfi, err := os.Lstat(d)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Rename(s, d); err != nil {
			return fmt.Errorf("move %s: %w", s, err)
		}
		m.res.Moved += countChats(d)
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", d, err)
	}
	sfi, err := os.Lstat(s)
	if err != nil {
		return fmt.Errorf("stat %s: %w", s, err)
	}
	switch {
	case sfi.IsDir() && dfi.IsDir():
		if err := m.dir(s, d); err != nil {
			return err
		}
		return os.Remove(s)
	case sfi.Mode().IsRegular() && dfi.Mode().IsRegular():
		same, err := sameBytes(s, d)
		if err != nil {
			return err
		}
		if same {
			return os.Remove(s) // identical copy already in main
		}
	}
	return m.keep(s, d)
}

// keep renames s next to d under a unique conflict name.
func (m *merger) keep(s, d string) error {
	target := d + m.tag
	for n := 1; ; n++ {
		if _, err := os.Lstat(target); errors.Is(err, fs.ErrNotExist) {
			break
		} else if err != nil {
			return fmt.Errorf("stat %s: %w", target, err)
		}
		target = d + m.tag + "-" + strconv.Itoa(n)
	}
	if err := os.Rename(s, target); err != nil {
		return fmt.Errorf("keep %s: %w", s, err)
	}
	m.res.Conflicts++
	return nil
}

func sameBytes(a, b string) (bool, error) {
	ab, err := os.ReadFile(a)
	if err != nil {
		return false, err
	}
	bb, err := os.ReadFile(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ab, bb), nil
}

// countChats counts chat transcripts at or under path.
func countChats(path string) int {
	n := 0
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == subagentsDir {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() && strings.HasSuffix(d.Name(), transcriptExt) {
			n++
		}
		return nil
	})
	return n
}
