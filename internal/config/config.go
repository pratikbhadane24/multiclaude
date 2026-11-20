// Package config stores ccm's launch-argument settings.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"multiclaude/internal/paths"
)

const (
	fileName = "config.json"
	fileMode = 0o600
	dirMode  = 0o700
)

// AccountConfig holds per-account launch arguments.
type AccountConfig struct {
	Args []string `json:"args,omitempty"`
}

// Config is the on-disk ccm configuration.
type Config struct {
	DefaultArgs      []string                 `json:"defaultArgs,omitempty"`
	Accounts         map[string]AccountConfig `json:"accounts,omitempty"`
	AutoSwitch       bool                     `json:"autoSwitch,omitempty"`
	AutoSwitchPrompt string                   `json:"autoSwitchPrompt,omitempty"`
}

func filePath(p paths.Paths) string { return filepath.Join(p.AccountsRoot, fileName) }

// Load reads the config; a missing file yields an empty Config.
func Load(p paths.Paths) (Config, error) {
	data, err := os.ReadFile(filePath(p))
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", filePath(p), err)
	}
	return c, nil
}

// Save writes the config atomically with mode 0600.
func Save(p paths.Paths, c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	dest := filePath(p)
	if err := os.MkdirAll(filepath.Dir(dest), dirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(dest), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".config.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(fileMode); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod config: %w", err)
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return fmt.Errorf("rename config: %w", err)
	}
	return nil
}

// MergedArgs returns defaults, then the account's args, then typed, in a new slice.
func MergedArgs(c Config, account string, typed []string) []string {
	acct := c.Accounts[account].Args
	out := make([]string, 0, len(c.DefaultArgs)+len(acct)+len(typed))
	out = append(out, c.DefaultArgs...)
	out = append(out, acct...)
	return append(out, typed...)
}

var safeArg = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

func quote(s string) string {
	if safeArg.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ShellJoin renders args as one POSIX shell string, single-quoting as needed.
func ShellJoin(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = quote(a)
	}
	return strings.Join(parts, " ")
}
