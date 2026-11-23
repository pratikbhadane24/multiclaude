// Package keychain reads per-account Claude Code credentials from the macOS
// Keychain, or from Claude Code's .credentials.json fallback file.
package keychain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/text/unicode/norm"

	"github.com/pratikbhadane24/multiclaude/internal/consts"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/run"
)

const (
	securityBin = "/usr/bin/security"
	hashHexLen  = 8
)

// ErrCredsParse indicates the keychain payload was not valid credentials JSON.
var ErrCredsParse = errors.New("keychain: cannot parse credentials")

// Creds holds the OAuth fields ccm needs. Never print the token.
type Creds struct {
	AccessToken      string
	ExpiresAt        int64
	SubscriptionType string
	RateLimitTier    string
	// AccountUUID identifies the login (not the token); never printed.
	AccountUUID string
}

// String redacts all fields so credentials cannot leak via formatting.
func (c Creds) String() string { return "Creds{redacted}" }

// Service returns the keychain service name for an account dir ("" = main).
// The dir is NFC-normalized and cleaned before hashing.
func Service(dir string) string {
	if dir == "" {
		return consts.KeychainBase
	}
	clean := filepath.Clean(norm.NFC.String(dir))
	sum := sha256.Sum256([]byte(clean))
	return consts.KeychainBase + "-" + hex.EncodeToString(sum[:])[:hashHexLen]
}

type payload struct {
	ClaudeAiOauth struct {
		AccessToken      string `json:"accessToken"`
		ExpiresAt        int64  `json:"expiresAt"`
		SubscriptionType string `json:"subscriptionType"`
		RateLimitTier    string `json:"rateLimitTier"`
		AccountUUID      string `json:"accountUuid"`
	} `json:"claudeAiOauth"`
}

const notFoundCode = 44

// Present reports whether the account's keychain item exists without
// reading the secret (no -w), falling back to its credentials file.
func Present(ctx context.Context, r run.Runner, a registry.Account) (bool, error) {
	res, err := r.Run(ctx, securityBin, []string{"find-generic-password", "-s", Service(a.Dir)}, nil, false)
	if err != nil {
		return false, fmt.Errorf("keychain: run security: %w", err)
	}
	switch res.Code {
	case 0:
		return true, nil
	case notFoundCode:
		c, err := fileCreds(a)
		return c != nil, err
	}
	return false, fmt.Errorf("keychain: security exited %d", res.Code)
}

// Read fetches an account's credentials from the Keychain, or, when the
// Keychain has none (absent or locked, e.g. over SSH), from the account's
// private .credentials.json. It returns nil, nil when neither has a login.
func Read(ctx context.Context, r run.Runner, a registry.Account) (*Creds, error) {
	args := []string{"find-generic-password", "-s", Service(a.Dir), "-w"}
	res, err := r.Run(ctx, securityBin, args, nil, false)
	if err != nil {
		return nil, fmt.Errorf("keychain: run security: %w", err)
	}
	if res.Code != 0 {
		return fileCreds(a)
	}
	return parsePayload([]byte(res.Stdout))
}

// parsePayload decodes Claude Code's credentials JSON (Keychain secret or
// .credentials.json). A payload without an access token reads as logged out.
func parsePayload(data []byte) (*Creds, error) {
	var p payload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, ErrCredsParse
	}
	o := p.ClaudeAiOauth
	if o.AccessToken == "" {
		return nil, nil
	}
	return &Creds{
		AccessToken:      o.AccessToken,
		ExpiresAt:        o.ExpiresAt,
		SubscriptionType: o.SubscriptionType,
		RateLimitTier:    o.RateLimitTier,
		AccountUUID:      o.AccountUUID,
	}, nil
}
