// Package install places the launcher shim and the ccm symlink.
package install

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"multiclaude/internal/paths"
)

const (
	shimName = "ccm-shim"
	linkName = "ccm"
	execMode = 0o755

	privateMode = 0o700
)

// Result describes what Install changed.
type Result struct {
	ShimPath, CCMLink        string
	ShimUpdated, LinkCreated bool
}

// Install copies the sibling ccm-shim to p.ShimPath and symlinks
// <localBin>/ccm to the resolved selfExe.
func Install(p paths.Paths, selfExe, localBin string) (Result, error) {
	exe, err := filepath.EvalSymlinks(selfExe)
	if err != nil {
		return Result{}, fmt.Errorf("resolve %s: %w", selfExe, err)
	}
	dir := filepath.Dir(exe)
	shimSrc := filepath.Join(dir, shimName)
	data, err := os.ReadFile(shimSrc)
	if errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("ccm-shim not found next to %s; run make build", dir)
	}
	if err != nil {
		return Result{}, fmt.Errorf("read %s: %w", shimSrc, err)
	}

	if err := ensureAccountsRoot(p.AccountsRoot); err != nil {
		return Result{}, err
	}
	res := Result{ShimPath: p.ShimPath, CCMLink: filepath.Join(localBin, linkName)}
	if res.ShimUpdated, err = writeShim(p.ShimPath, data); err != nil {
		return Result{}, err
	}
	if res.LinkCreated, err = ensureLink(res.CCMLink, exe); err != nil {
		return Result{}, err
	}
	return res, nil
}

// ensureAccountsRoot creates root private (0700). An existing directory is
// left as the user set it.
func ensureAccountsRoot(root string) error {
	if root == "" {
		return nil
	}
	if _, err := os.Stat(root); err == nil {
		return nil
	}
	if err := os.MkdirAll(root, privateMode); err != nil {
		return fmt.Errorf("create %s: %w", root, err)
	}
	if err := os.Chmod(root, privateMode); err != nil {
		return fmt.Errorf("chmod %s: %w", root, err)
	}
	return nil
}

func writeShim(dst string, data []byte) (bool, error) {
	if cur, err := os.ReadFile(dst); err == nil && bytes.Equal(cur, data) {
		if st, serr := os.Stat(dst); serr == nil && st.Mode().Perm() == execMode {
			return false, nil
		}
	}
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, execMode); err != nil {
		return false, fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".ccm-shim-*")
	if err != nil {
		return false, fmt.Errorf("temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return false, fmt.Errorf("write shim: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return false, fmt.Errorf("close shim: %w", err)
	}
	if err := os.Chmod(tmpName, execMode); err != nil {
		cleanup()
		return false, fmt.Errorf("chmod shim: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		cleanup()
		return false, fmt.Errorf("install shim: %w", err)
	}
	return true, nil
}

func ensureLink(link, target string) (bool, error) {
	st, err := os.Lstat(link)
	switch {
	case err == nil:
		if st.Mode()&os.ModeSymlink != 0 {
			if cur, rerr := os.Readlink(link); rerr == nil && cur == target {
				return false, nil
			}
		}
		return false, fmt.Errorf("%s exists; remove it or install manually", link)
	case !errors.Is(err, os.ErrNotExist):
		return false, fmt.Errorf("stat %s: %w", link, err)
	}
	if err := os.MkdirAll(filepath.Dir(link), execMode); err != nil {
		return false, fmt.Errorf("create %s: %w", filepath.Dir(link), err)
	}
	if err := os.Symlink(target, link); err != nil {
		return false, fmt.Errorf("link %s: %w", link, err)
	}
	return true, nil
}

// NextSteps returns the post-install instructions.
func NextSteps(accountsRoot string) string {
	return "Installed.\n" +
		"Add this line to ~/.zshrc, then open a new terminal:\n" +
		"  eval \"$(ccm shell-init zsh)\"\n" +
		"Next: ccm add <name> --email <you@example.com>\n"
}
