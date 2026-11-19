// Package history merges an account's own conversation history into the
// shared copy in ~/.claude and replaces it with a symlink. No data is ever
// deleted unless a byte-identical copy already exists in main.
package history

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"multiclaude/internal/consts"
	"multiclaude/internal/paths"
)

const (
	conflictInfix = ".from-"
	transcriptExt = ".jsonl"
	projectsItem  = "projects"
	compareChunk  = 64 * 1024
)

// MergeResult summarizes one MergeIntoMain call.
type MergeResult struct {
	// Moved counts chat transcripts (*.jsonl directly under a project dir)
	// that were moved into main.
	Moved int
	// Conflicts counts entries kept as <name>.from-<account>-<unix>[.jsonl].
	Conflicts int
	// Linked is true when the account dir was replaced by a symlink.
	Linked bool
}

// MergeIntoMain merges the real directory <accountDir>/<item> into
// ~/.claude/<item> and replaces it with a symlink. It is a no-op when the
// account entry is missing or already a symlink. When main has no such
// directory the account's one is moved there whole. Otherwise entries missing
// from main are moved, identical files dropped, and differing files, links
// and type mismatches kept in main under a unique ".from-" name. It refuses
// (before changing anything) an account dir that is, contains or aliases main.
func MergeIntoMain(p paths.Paths, accountDir, item string, now time.Time) (MergeResult, error) {
	if !slices.Contains(consts.HistoryItems, item) {
		return MergeResult{}, fmt.Errorf("merge %q: not a history item", item)
	}
	if !filepath.IsAbs(accountDir) {
		return MergeResult{}, fmt.Errorf("merge %s: account dir %q is not absolute", item, accountDir)
	}
	main := filepath.Join(p.ClaudeDir, item)
	acct := filepath.Join(accountDir, item)
	if fi, err := os.Lstat(acct); errors.Is(err, fs.ErrNotExist) || (err == nil && !fi.IsDir()) {
		return MergeResult{}, nil
	} else if err != nil {
		return MergeResult{}, fmt.Errorf("stat %s: %w", acct, err)
	}
	mfi, err := os.Lstat(main)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return adopt(item, acct, main)
	case err != nil:
		return MergeResult{}, fmt.Errorf("stat %s: %w", main, err)
	case !mfi.IsDir():
		return MergeResult{}, fmt.Errorf("main %s is not a directory", item)
	}
	if err := checkNoOverlap(acct, main); err != nil {
		return MergeResult{}, err
	}
	m := merger{item: item, tag: conflictInfix + filepath.Base(accountDir) + "-" + strconv.FormatInt(now.Unix(), 10)}
	if err := m.dir(acct, main, 0); err != nil {
		return m.res, err
	}
	if err := os.Remove(acct); err != nil {
		return m.res, fmt.Errorf("account %s not empty after merge: %w", acct, err)
	}
	return m.res, m.link(acct, main)
}

// adopt moves the whole account dir to main when main has none.
func adopt(item, acct, main string) (MergeResult, error) {
	if err := os.Rename(acct, main); err != nil {
		return MergeResult{}, moveErr("move", acct, err)
	}
	m := merger{item: item}
	if item == projectsItem {
		entries, _ := os.ReadDir(main)
		for _, e := range entries {
			if e.IsDir() {
				m.res.Moved += countTranscripts(filepath.Join(main, e.Name()))
			}
		}
	}
	return m.res, m.link(acct, main)
}

// checkNoOverlap fails when the two dirs are the same, or one contains the
// other, judged by identity (symlinks resolved), not by spelling.
func checkNoOverlap(acct, main string) error {
	a, err := os.Stat(acct)
	if err != nil {
		return fmt.Errorf("stat %s: %w", acct, err)
	}
	m, err := os.Stat(main)
	if err != nil {
		return fmt.Errorf("stat %s: %w", main, err)
	}
	ra, err1 := filepath.EvalSymlinks(acct)
	rm, err2 := filepath.EvalSymlinks(main)
	if err := errors.Join(err1, err2); err != nil {
		return fmt.Errorf("resolve history dirs: %w", err)
	}
	if os.SameFile(a, m) || within(ra, rm) || within(rm, ra) {
		return fmt.Errorf("refusing to merge: %s and %s are the same or nested", acct, main)
	}
	return nil
}

// within reports whether child equals or lies under parent.
func within(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

type merger struct {
	item, tag string
	res       MergeResult
}

func (m *merger) link(acct, main string) error {
	if err := os.Symlink(main, acct); err != nil {
		return fmt.Errorf("link %s: %w", acct, err)
	}
	m.res.Linked = true
	return nil
}

// dir moves every child of src into dst, then leaves src empty. depth 0 is
// the item root (children are project dirs); depth 1 holds the transcripts.
func (m *merger) dir(src, dst string, depth int) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	for _, e := range entries {
		if err := m.entry(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()), depth); err != nil {
			return err
		}
	}
	return nil
}

func (m *merger) entry(s, d string, depth int) error {
	sfi, err := os.Lstat(s)
	if err != nil {
		return fmt.Errorf("stat %s: %w", s, err)
	}
	if sfi.Mode()&os.ModeSymlink != 0 {
		return m.keep(s, d) // never move a link: it may resolve into main
	}
	dfi, err := os.Lstat(d)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Rename(s, d); err != nil {
			return moveErr("move", s, err)
		}
		m.count(d, sfi, depth)
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", d, err)
	}
	if os.SameFile(sfi, dfi) {
		return fmt.Errorf("refusing to merge: %s and %s are the same file", s, d)
	}
	switch {
	case sfi.IsDir() && dfi.IsDir():
		if err := m.dir(s, d, depth+1); err != nil {
			return err
		}
		return os.Remove(s)
	case sfi.Mode().IsRegular() && dfi.Mode().IsRegular():
		same, err := sameBytes(s, d, sfi, dfi)
		if err != nil {
			return err
		}
		if same {
			return os.Remove(s) // identical copy already in main
		}
	}
	return m.keep(s, d)
}

// count adds the transcripts moved by renaming s to d.
func (m *merger) count(d string, sfi fs.FileInfo, depth int) {
	if m.item != projectsItem {
		return
	}
	switch {
	case depth == 0 && sfi.IsDir():
		m.res.Moved += countTranscripts(d)
	case depth == 1 && sfi.Mode().IsRegular() && strings.HasSuffix(d, transcriptExt):
		m.res.Moved++
	}
}

// keep renames s next to d under a unique conflict name, keeping a .jsonl
// extension so a kept transcript stays resumable.
func (m *merger) keep(s, d string) error {
	stem, ext := d, ""
	if strings.HasSuffix(d, transcriptExt) {
		stem, ext = strings.TrimSuffix(d, transcriptExt), transcriptExt
	}
	target := stem + m.tag + ext
	for n := 1; ; n++ {
		if _, err := os.Lstat(target); errors.Is(err, fs.ErrNotExist) {
			break
		} else if err != nil {
			return fmt.Errorf("stat %s: %w", target, err)
		}
		target = stem + m.tag + "-" + strconv.Itoa(n) + ext
	}
	if err := os.Rename(s, target); err != nil {
		return moveErr("keep", s, err)
	}
	m.res.Conflicts++
	return nil
}

func moveErr(op, path string, err error) error {
	if errors.Is(err, syscall.EXDEV) {
		return fmt.Errorf("%s %s: %w (both dirs must be on the same volume)", op, path, err)
	}
	return fmt.Errorf("%s %s: %w", op, path, err)
}

// sameBytes compares two regular files, checking sizes first and streaming.
func sameBytes(a, b string, afi, bfi fs.FileInfo) (bool, error) {
	if afi.Size() != bfi.Size() {
		return false, nil
	}
	fa, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer fa.Close()
	fb, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer fb.Close()
	bufA, bufB := make([]byte, compareChunk), make([]byte, compareChunk)
	for {
		na, errA := io.ReadFull(fa, bufA)
		nb, errB := io.ReadFull(fb, bufB)
		if na != nb || !bytes.Equal(bufA[:na], bufB[:nb]) {
			return false, nil
		}
		if errA != nil || errB != nil {
			if isEOF(errA) && isEOF(errB) {
				return true, nil
			}
			return false, errors.Join(nonEOF(errA), nonEOF(errB))
		}
	}
}

func isEOF(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

func nonEOF(err error) error {
	if isEOF(err) {
		return nil
	}
	return err
}

// countTranscripts counts *.jsonl files directly in a project dir.
func countTranscripts(projectDir string) int {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), transcriptExt) {
			n++
		}
	}
	return n
}
