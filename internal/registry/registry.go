// Package registry persists the list of ccm accounts.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"multiclaude/internal/consts"
	"multiclaude/internal/paths"
)

const (
	registryFile = "accounts.json"
	rootMode     = 0o700
	fileMode     = 0o600
)

// Account is one Claude Code account; an empty Dir means the main account.
type Account struct {
	Name  string `json:"name"`
	Dir   string `json:"dir"`
	Email string `json:"email,omitempty"`
}

// IsMain reports whether the account is the main (~/.claude) account.
func (a Account) IsMain() bool { return a.Name == consts.MainName }

// NormalizeDir returns an absolute, NFC-normalized path with no trailing slash.
func NormalizeDir(p, home string) string { return paths.Normalize(p, home) }

// ValidateName checks an account name against the format and reserved words.
func ValidateName(name string) error {
	if !consts.AccountNameRE.MatchString(name) {
		return fmt.Errorf("invalid account name %q: must match %s", name, consts.AccountNameRE)
	}
	if _, ok := consts.ReservedNames[name]; ok {
		return fmt.Errorf("account name %q is reserved", name)
	}
	return nil
}

func registryPath(p paths.Paths) string { return filepath.Join(p.AccountsRoot, registryFile) }

// Load reads the registry; main is always first and never read from disk.
func Load(p paths.Paths) ([]Account, error) {
	main := Account{Name: consts.MainName}
	file := registryPath(p)
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return []Account{main}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	var stored []Account
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	out := []Account{main}
	for _, a := range stored {
		if !a.IsMain() {
			out = append(out, a)
		}
	}
	return out, nil
}

// Save atomically writes all non-main accounts.
func Save(p paths.Paths, accts []Account) error {
	stored := make([]Account, 0, len(accts))
	for _, a := range accts {
		if !a.IsMain() {
			stored = append(stored, a)
		}
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("encode registry: %w", err)
	}
	return atomicWrite(p.AccountsRoot, registryPath(p), data)
}

func atomicWrite(dir, dest string, data []byte) error {
	if err := os.MkdirAll(dir, rootMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".accounts-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(fileMode); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmp.Name(), err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return fmt.Errorf("rename to %s: %w", dest, err)
	}
	return nil
}

// With returns a new list with a replacing the same-named account, or appended.
func With(list []Account, a Account) []Account {
	out := make([]Account, len(list), len(list)+1)
	copy(out, list)
	for i := range out {
		if out[i].Name == a.Name {
			out[i] = a
			return out
		}
	}
	return append(out, a)
}

// Without returns a new list excluding the named account.
func Without(list []Account, name string) []Account {
	out := make([]Account, 0, len(list))
	for _, a := range list {
		if a.Name != name {
			out = append(out, a)
		}
	}
	return out
}

// Find looks up an account by name.
func Find(list []Account, name string) (Account, bool) {
	for _, a := range list {
		if a.Name == name {
			return a, true
		}
	}
	return Account{}, false
}
