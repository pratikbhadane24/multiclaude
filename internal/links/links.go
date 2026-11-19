// Package links manages the symlinks from an account dir back to the shared
// items in ~/.claude. It never writes inside ClaudeDir and never deletes
// account data that differs from the source (it renames it to a backup).
package links

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"multiclaude/internal/consts"
	"multiclaude/internal/paths"
)

// State describes one shared item inside an account dir.
type State string

const (
	StateOK          State = "ok"
	StateMissing     State = "missing"
	StateReplaced    State = "replaced"
	StateWrongTarget State = "wrong-target"
	StateNoSource    State = "no-source"
)

const (
	ActionRelinkedIdentical  = "relinked-identical"
	ActionRelinkedWithBackup = "relinked-with-backup"
	backupInfix              = ".ccm-backup-"
)

// Item is the inspected state of a single shared item.
type Item struct {
	Name  string
	State State
}

// FixResult reports what FixReplaced did.
type FixResult struct {
	Action     string
	BackupPath string
}

func inspectOne(p paths.Paths, accountDir, name string) (State, error) {
	src := filepath.Join(p.ClaudeDir, name)
	dst := filepath.Join(accountDir, name)
	if _, err := os.Lstat(src); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return StateNoSource, nil
		}
		return "", fmt.Errorf("stat source %s: %w", src, err)
	}
	fi, err := os.Lstat(dst)
	if errors.Is(err, fs.ErrNotExist) {
		return StateMissing, nil
	}
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", dst, err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return StateReplaced, nil
	}
	target, err := os.Readlink(dst)
	if err != nil {
		return "", fmt.Errorf("readlink %s: %w", dst, err)
	}
	if target == src {
		return StateOK, nil
	}
	return StateWrongTarget, nil
}

// Inspect returns the state of every shared item, in consts.SharedItems order.
func Inspect(p paths.Paths, accountDir string) ([]Item, error) {
	items := make([]Item, 0, len(consts.SharedItems))
	for _, name := range consts.SharedItems {
		st, err := inspectOne(p, accountDir, name)
		if err != nil {
			return nil, err
		}
		items = append(items, Item{Name: name, State: st})
	}
	return items, nil
}

// LinkMissing creates symlinks only for items whose state is missing and
// returns their names. Existing files and links are never touched.
func LinkMissing(p paths.Paths, accountDir string) ([]string, error) {
	items, err := Inspect(p, accountDir)
	if err != nil {
		return nil, err
	}
	linked := []string{}
	for _, it := range items {
		if it.State != StateMissing {
			continue
		}
		src := filepath.Join(p.ClaudeDir, it.Name)
		if err := os.Symlink(src, filepath.Join(accountDir, it.Name)); err != nil {
			return linked, fmt.Errorf("link %s: %w", it.Name, err)
		}
		linked = append(linked, it.Name)
	}
	return linked, nil
}

// FixReplaced re-links an item whose account copy replaced the symlink. An
// identical copy is dropped; a differing one is renamed to a unique backup.
func FixReplaced(p paths.Paths, accountDir, item string, now time.Time) (FixResult, error) {
	if !isSharedItem(item) {
		return FixResult{}, fmt.Errorf("fix %q: not a shared item", item)
	}
	st, err := inspectOne(p, accountDir, item)
	if err != nil {
		return FixResult{}, err
	}
	if st != StateReplaced {
		return FixResult{}, fmt.Errorf("fix %s: state is %q, not %q; nothing changed", item, st, StateReplaced)
	}
	src := filepath.Join(p.ClaudeDir, item)
	dst := filepath.Join(accountDir, item)
	same, err := sameContent(src, dst)
	if err != nil {
		return FixResult{}, fmt.Errorf("compare %s: %w", item, err)
	}
	if same {
		return relinkIdentical(src, dst)
	}
	return relinkWithBackup(src, dst, now)
}

func isSharedItem(item string) bool {
	for _, n := range consts.SharedItems {
		if n == item {
			return true
		}
	}
	return false
}

func relinkIdentical(src, dst string) (FixResult, error) {
	// Identical to the source, so removing the copy loses nothing.
	if err := os.RemoveAll(dst); err != nil {
		return FixResult{}, fmt.Errorf("remove identical copy %s: %w", dst, err)
	}
	if err := os.Symlink(src, dst); err != nil {
		return FixResult{}, fmt.Errorf("link %s: %w", dst, err)
	}
	return FixResult{Action: ActionRelinkedIdentical}, nil
}

func relinkWithBackup(src, dst string, now time.Time) (FixResult, error) {
	backup, err := uniqueBackupPath(dst, now)
	if err != nil {
		return FixResult{}, err
	}
	if err := os.Rename(dst, backup); err != nil {
		return FixResult{}, fmt.Errorf("backup %s: %w", dst, err)
	}
	if err := os.Symlink(src, dst); err != nil {
		if rerr := os.Rename(backup, dst); rerr != nil {
			return FixResult{}, fmt.Errorf("link %s: %w (restore from %s failed: %v)", dst, err, backup, rerr)
		}
		return FixResult{}, fmt.Errorf("link %s: %w", dst, err)
	}
	return FixResult{Action: ActionRelinkedWithBackup, BackupPath: backup}, nil
}

func uniqueBackupPath(dst string, now time.Time) (string, error) {
	base := dst + backupInfix + strconv.FormatInt(now.Unix(), 10)
	candidate := base
	for n := 1; ; n++ {
		_, err := os.Lstat(candidate)
		if errors.Is(err, fs.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("stat %s: %w", candidate, err)
		}
		candidate = base + "-" + strconv.Itoa(n)
	}
}

// sameContent compares src (links followed) with dst (an actual file or dir).
func sameContent(src, dst string) (bool, error) {
	sfi, err := os.Stat(src)
	if err != nil {
		return false, err
	}
	dfi, err := os.Lstat(dst)
	if err != nil {
		return false, err
	}
	switch {
	case sfi.IsDir() && dfi.IsDir():
		return sameDir(src, dst)
	case sfi.Mode().IsRegular() && dfi.Mode().IsRegular():
		return sameFile(src, dst)
	}
	return false, nil
}

func sameFile(a, b string) (bool, error) {
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

func sameDir(a, b string) (bool, error) {
	ae, err := os.ReadDir(a)
	if err != nil {
		return false, err
	}
	be, err := os.ReadDir(b)
	if err != nil {
		return false, err
	}
	if len(ae) != len(be) {
		return false, nil
	}
	for i := range ae {
		if ae[i].Name() != be[i].Name() {
			return false, nil
		}
		same, err := sameContent(filepath.Join(a, ae[i].Name()), filepath.Join(b, be[i].Name()))
		if err != nil || !same {
			return false, err
		}
	}
	return true, nil
}
