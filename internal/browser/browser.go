// Package browser handles the login URL Claude tries to open, so it never
// lands in the user's default (possibly wrong-account) browser.
package browser

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strings"

	"multiclaude/internal/run"
)

// Env contract between ccm login and the BROWSER shim invocation.
const (
	EnvAccount = "CCM_LOGIN_ACCOUNT"
	EnvEmail   = "CCM_LOGIN_EMAIL"
	EnvMode    = "CCM_LOGIN_OPEN"
)

// Mode kinds.
const (
	KindCopy          = "copy"
	KindIncognito     = "incognito"
	KindChromeProfile = "chrome-profile"
)

const (
	chromeProfilePrefix = "chrome:"
	openBin             = "/usr/bin/open"
	pbcopyBin           = "/usr/bin/pbcopy"
	chromeApp           = "Google Chrome"
	httpsPrefix         = "https://"
	exitRefused         = 1
)

// ValidModes describes the accepted mode strings.
const ValidModes = `"copy", "incognito" or "chrome:<Profile dir>"`

// Mode says how a login link is presented.
type Mode struct {
	Kind    string
	Profile string
}

// ParseMode parses "", "copy", "incognito" or "chrome:<Profile dir>".
func ParseMode(s string) (Mode, error) {
	switch {
	case s == "" || s == KindCopy:
		return Mode{Kind: KindCopy}, nil
	case s == KindIncognito:
		return Mode{Kind: KindIncognito}, nil
	case strings.HasPrefix(s, chromeProfilePrefix) && len(s) > len(chromeProfilePrefix):
		return Mode{Kind: KindChromeProfile, Profile: strings.TrimPrefix(s, chromeProfilePrefix)}, nil
	}
	return Mode{}, fmt.Errorf("invalid login browser %q: use %s", s, ValidModes)
}

// String is the inverse of ParseMode.
func (m Mode) String() string {
	if m.Kind == KindChromeProfile {
		return chromeProfilePrefix + m.Profile
	}
	if m.Kind == "" {
		return KindCopy
	}
	return m.Kind
}

// Clipboard copies text to the system clipboard; replaceable in tests.
var Clipboard = pbcopy

func pbcopy(text string) error {
	cmd := exec.Command(pbcopyBin)
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pbcopy: %w", err)
	}
	return nil
}

// IsOpenURLInvocation reports whether the shim was called by claude as $BROWSER.
func IsOpenURLInvocation(args []string, getenv func(string) string) bool {
	return len(args) == 1 && strings.HasPrefix(args[0], httpsPrefix) && getenv(EnvAccount) != ""
}

func hostAllowed(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	for _, domain := range []string{"claude.ai", "claude.com", "anthropic.com"} {
		if h == domain || strings.HasSuffix(h, "."+domain) {
			return true
		}
	}
	return false
}

// HandleOpenURL presents the login URL per the configured mode and returns an exit code.
func HandleOpenURL(rawURL string, getenv func(string) string, r run.Runner, out io.Writer) int {
	if !hostAllowed(rawURL) {
		fmt.Fprintln(out, "ccm: refusing to open a URL outside Claude/Anthropic domains.")
		return exitRefused
	}
	mode, err := ParseMode(getenv(EnvMode))
	if err != nil {
		mode = Mode{Kind: KindCopy}
	}
	if args := openArgs(mode, rawURL); args != nil {
		res, err := r.Run(context.Background(), openBin, args, nil, false)
		if err == nil && res.Code == 0 {
			return 0
		}
		fmt.Fprintf(out, "ccm: warning: could not open Google Chrome (%s); copying the link instead.\n", mode)
	}
	return copyLink(rawURL, getenv, out)
}

func openArgs(m Mode, rawURL string) []string {
	base := []string{"-na", chromeApp, "--args"}
	switch m.Kind {
	case KindIncognito:
		return append(base, "--incognito", rawURL)
	case KindChromeProfile:
		return append(base, "--profile-directory="+m.Profile, rawURL)
	}
	return nil
}

func copyLink(rawURL string, getenv func(string) string, out io.Writer) int {
	who := fmt.Sprintf("%q", getenv(EnvAccount))
	if email := getenv(EnvEmail); email != "" {
		who += " (" + email + ")"
	}
	if err := Clipboard(rawURL); err != nil {
		fmt.Fprintf(out, "ccm: warning: could not copy to clipboard: %v\n", err)
		fmt.Fprintf(out, "Login link for %s:\n", who)
	} else {
		fmt.Fprintf(out, "Login link for %s copied to clipboard.\n", who)
	}
	fmt.Fprintln(out, "Paste it into the browser or profile signed in to that account, or a private window.")
	fmt.Fprintln(out, rawURL)
	return 0
}
