// Package auth runs Claude's login and status commands per account.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pratikbhadane24/multiclaude/internal/browser"
	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/run"
)

const configDirEnv = "CLAUDE_CONFIG_DIR"

// Status is the subset of `claude auth status --json` that ccm uses.
type Status struct {
	LoggedIn         bool   `json:"loggedIn"`
	Email            string `json:"email"`
	SubscriptionType string `json:"subscriptionType"`
	PlanTier         string `json:"planTier"`
	LoginExpiresAt   string `json:"loginExpiresAt"`
}

// MismatchError reports that the signed-in email differs from the wanted one.
type MismatchError struct{ Want, Got string }

func (e *MismatchError) Error() string {
	return fmt.Sprintf("signed in as %s, expected %s", e.Got, e.Want)
}

// Env returns a new env slice with CLAUDE_CONFIG_DIR set for the account
// (or removed for main). base is not modified.
func Env(a registry.Account, base []string) []string {
	prefix := configDirEnv + "="
	out := make([]string, 0, len(base)+1)
	for _, e := range base {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	if a.IsMain() {
		return out
	}
	return append(out, prefix+a.Dir)
}

// Binary returns the shim when installed, else the real claude.
func Binary(p paths.Paths) string {
	if _, err := os.Stat(p.ShimPath); err == nil {
		return p.ShimPath
	}
	return p.RealClaude
}

// GetStatus runs `auth status --json` with the account's env.
func GetStatus(ctx context.Context, r run.Runner, p paths.Paths, a registry.Account) (Status, error) {
	res, err := r.Run(ctx, Binary(p), []string{"auth", "status", "--json"}, Env(a, os.Environ()), false)
	if err != nil {
		return Status{}, fmt.Errorf("auth status for %s: %w", a.Name, err)
	}
	var st Status
	if err := json.Unmarshal([]byte(res.Stdout), &st); err != nil {
		return Status{}, fmt.Errorf("parse auth status for %s: %w", a.Name, err)
	}
	return st, nil
}

// Refresh runs GetStatus to let claude refresh tokens, discarding the result.
func Refresh(ctx context.Context, r run.Runner, p paths.Paths, a registry.Account) error {
	_, err := GetStatus(ctx, r, p, a)
	return err
}

// LoginOptions controls how the login URL is presented.
type LoginOptions struct {
	SelfExe string // ccm binary used as $BROWSER; empty leaves the browser env alone
	Mode    browser.Mode
}

// loginEnv returns the login env: Env plus the BROWSER contract when SelfExe is set.
func loginEnv(a registry.Account, email string, opts LoginOptions) []string {
	env := Env(a, os.Environ())
	if opts.SelfExe == "" {
		return env
	}
	return append(env,
		"BROWSER="+opts.SelfExe,
		browser.EnvAccount+"="+a.Name,
		browser.EnvEmail+"="+email,
		browser.EnvMode+"="+opts.Mode.String(),
	)
}

// LoginWith runs an interactive login and verifies the resulting email.
func LoginWith(ctx context.Context, r run.Runner, p paths.Paths, a registry.Account, email string, opts LoginOptions) (Status, error) {
	args := []string{"auth", "login", "--claudeai"}
	if email != "" {
		args = append(args, "--email", email)
	}
	res, err := r.Run(ctx, Binary(p), args, loginEnv(a, email, opts), true)
	if err != nil {
		return Status{}, fmt.Errorf("login %s: %w", a.Name, err)
	}
	if res.Code != 0 {
		return Status{}, fmt.Errorf("login %s: claude exited with code %d", a.Name, res.Code)
	}
	st, err := GetStatus(ctx, r, p, a)
	if err != nil {
		return Status{}, err
	}
	if email != "" && !strings.EqualFold(st.Email, email) {
		return st, &MismatchError{Want: email, Got: st.Email}
	}
	return st, nil
}
