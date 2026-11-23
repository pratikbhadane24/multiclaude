package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// gitIdentityKeys selects the hub git settings copied to workers: who you
// are and URL rewrites. Credential settings are never copied.
const gitIdentityKeys = `^(user\.(name|email)|url\..*\.insteadof)$`

const ghLogin = "gh auth login --web --git-protocol https --hostname github.com && gh auth setup-git"

// GitIdentity copies the hub's git user name, email and URL rewrites to
// the worker's global git config.
func (s *Setup) GitIdentity(ctx context.Context) error {
	if s.Local == nil {
		return errors.New("no local runner")
	}
	res, err := s.Local.Run(ctx, "git", []string{"config", "--global", "--get-regexp", gitIdentityKeys}, nil, false)
	if err != nil {
		return fmt.Errorf("read hub git config: %w", err)
	}
	var cmds []string
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		if hasControl(key) || hasControl(value) {
			return fmt.Errorf("hub git setting %q has control characters", key)
		}
		cmds = append(cmds, fmt.Sprintf("git config --global %s %s", shQuote(key), shQuote(value)))
	}
	if len(cmds) == 0 {
		return nil
	}
	return s.runOK(ctx, strings.Join(cmds, " && "))
}

func hasControl(s string) bool {
	return strings.ContainsFunc(s, unicode.IsControl)
}

// ghAssetArch maps `uname -m` to the gh release asset architecture.
var ghAssetArch = map[string]string{"arm64": "arm64", "x86_64": "amd64"}

// InstallGH installs the hub's gh version into the worker's ~/.local/bin
// from GitHub's releases (no Homebrew, no sudo) when the worker has none.
func (s *Setup) InstallGH(ctx context.Context) error {
	if s.info.HasGH {
		return nil
	}
	if s.Local == nil {
		return errors.New("no local runner")
	}
	res, err := s.Local.Run(ctx, "gh", []string{"--version"}, nil, false)
	if err != nil || res.Code != 0 {
		return errors.New("gh is not installed on the hub; install it (brew install gh) to set it up on workers")
	}
	fields := strings.Fields(res.Stdout)
	arch, ok := ghAssetArch[s.info.Arch]
	if len(fields) < 3 || !ok {
		return fmt.Errorf("cannot pick a gh release for %q", s.info.Arch)
	}
	version := fields[2]
	asset := fmt.Sprintf("gh_%s_macOS_%s", version, arch)
	url := fmt.Sprintf("https://github.com/cli/cli/releases/download/v%s/%s.zip", version, asset)
	script := fmt.Sprintf(`set -e
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL %s -o "$tmp/gh.zip"
unzip -q "$tmp/gh.zip" -d "$tmp"
mkdir -p .local/bin
mv -f "$tmp/%s/bin/gh" .local/bin/gh
chmod 755 .local/bin/gh`, shQuote(url), asset)
	return s.runOK(ctx, script)
}

// GitHubLogin signs the worker's gh in to GitHub (device code: open the
// printed link in any browser) and makes git use it, unless it already is.
func (s *Setup) GitHubLogin(ctx context.Context) error {
	res, err := s.R.Run(ctx, `export PATH="$HOME/.local/bin:$PATH"; gh auth status --hostname github.com >/dev/null 2>&1`)
	if err != nil {
		return err
	}
	if res.Code == 0 {
		return nil
	}
	fmt.Fprintln(s.Out, "→ log in to GitHub on the worker: open the link it prints in any browser and enter the code.")
	code, err := s.R.Interactive(ctx, `PATH="$HOME/.local/bin:$PATH" && `+ghLogin)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("gh login on the worker exited %d", code)
	}
	return nil
}
