package keychain

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/pratikbhadane24/multiclaude/internal/registry"
)

const (
	credFileName = ".credentials.json"
	// privateBits are the group/other permission bits a credentials file
	// must not have.
	privateBits = 0o077
)

// credFilePath is where Claude Code keeps an account's login when the
// Keychain is unavailable (SSH sessions, Linux): <dir>/.credentials.json,
// or ~/.claude/.credentials.json for main.
func credFilePath(a registry.Account, home string) string {
	dir := a.Dir
	if dir == "" {
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, credFileName)
}

// readFile returns the credentials stored in path. A missing file, or one
// that is not a private regular file owned by this user, reads as logged
// out (nil, nil) and is never opened.
func readFile(path string) (*Creds, error) {
	if !isPrivateFile(path) {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("keychain: read credentials file: %w", err)
	}
	return parsePayload(data)
}

// isPrivateFile reports whether path is a regular file (not a symlink)
// owned by the current user with no group or other permissions.
func isPrivateFile(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&privateBits != 0 {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Getuid())
}

// fileCreds reads the account's credentials file; any home lookup failure
// reads as logged out.
func fileCreds(a registry.Account) (*Creds, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil
	}
	return readFile(credFilePath(a, home))
}
