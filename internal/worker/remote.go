package worker

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/pratikbhadane24/multiclaude/internal/run"
)

// Remote runs commands on a worker and moves files to it. Remote paths
// are relative to the worker user's home directory.
type Remote interface {
	Run(ctx context.Context, script string) (run.Result, error)
	Interactive(ctx context.Context, cmd string) (int, error)
	Copy(ctx context.Context, local, remote string) error
	Rsync(ctx context.Context, local, remoteDir string, excludes []string) error
	Read(ctx context.Context, path string) ([]byte, bool, error)
	Write(ctx context.Context, path string, data []byte, mode os.FileMode) error
}

const (
	sshConnectTimeout = "ConnectTimeout=8"
	// missingFileCode is the exit code Read's script uses for "no such file".
	missingFileCode = 3
)

// SSHRemote is a Remote over the system ssh, scp and rsync. Host is any
// name ssh understands (an ~/.ssh/config alias, a Tailscale name).
type SSHRemote struct {
	Host string
	R    run.Runner
}

func sshOpts() []string { return []string{"-o", "BatchMode=yes", "-o", sshConnectTimeout} }

// shQuote single-quotes s for a POSIX shell.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// Run executes script with bash on the worker, from its home directory.
func (s SSHRemote) Run(ctx context.Context, script string) (run.Result, error) {
	args := append(sshOpts(), s.Host, "bash -c "+shQuote(script))
	res, err := s.R.Run(ctx, "ssh", args, nil, false)
	if err != nil {
		return res, fmt.Errorf("ssh %s: %w", s.Host, err)
	}
	if res.Code == 255 {
		return res, fmt.Errorf("ssh %s: cannot connect: %s", s.Host, strings.TrimSpace(res.Stderr))
	}
	return res, nil
}

// Interactive runs cmd on a terminal (ssh -t) wired to this one.
func (s SSHRemote) Interactive(ctx context.Context, cmd string) (int, error) {
	args := append([]string{"-t", "-o", sshConnectTimeout}, s.Host, cmd)
	res, err := s.R.Run(ctx, "ssh", args, nil, true)
	if err != nil {
		return 0, fmt.Errorf("ssh %s: %w", s.Host, err)
	}
	return res.Code, nil
}

// Copy copies one local file to the worker.
func (s SSHRemote) Copy(ctx context.Context, local, remote string) error {
	args := append(append([]string{"-q"}, sshOpts()...), local, s.Host+":"+remote)
	return s.check(ctx, "scp", args)
}

// Rsync copies local (a file or directory) into remoteDir, sending only
// changes and never deleting anything on the worker.
func (s SSHRemote) Rsync(ctx context.Context, local, remoteDir string, excludes []string) error {
	args := []string{"-a", "-e", "ssh " + strings.Join(sshOpts(), " ")}
	for _, e := range excludes {
		args = append(args, "--exclude", e)
	}
	args = append(args, local, s.Host+":"+strings.TrimSuffix(remoteDir, "/")+"/")
	return s.check(ctx, "rsync", args)
}

// Read returns a worker file's content; ok is false when it does not exist.
func (s SSHRemote) Read(ctx context.Context, p string) ([]byte, bool, error) {
	script := fmt.Sprintf(`f=%s; if [ -f "$f" ]; then base64 < "$f"; else exit %d; fi`, shQuote(p), missingFileCode)
	res, err := s.Run(ctx, script)
	if err != nil {
		return nil, false, err
	}
	if res.Code == missingFileCode {
		return nil, false, nil
	}
	if res.Code != 0 {
		return nil, false, fmt.Errorf("read %s on %s: %s", p, s.Host, strings.TrimSpace(res.Stderr))
	}
	data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(res.Stdout), ""))
	if err != nil {
		return nil, false, fmt.Errorf("read %s on %s: %w", p, s.Host, err)
	}
	return data, true, nil
}

// Write replaces a worker file atomically, backing up any existing one.
// The temp file is created private (umask 077, mktemp) before the final
// mode is applied, and a symlink at path is refused.
func (s SSHRemote) Write(ctx context.Context, p string, data []byte, mode os.FileMode) error {
	script := fmt.Sprintf(`set -e
umask 077
f=%s
if [ -L "$f" ]; then echo "refusing to write through symlink $f" >&2; exit 1; fi
mkdir -p "$(dirname "$f")"
if [ -e "$f" ]; then cp -p "$f" "$f.ccm-backup-$(date +%%s)"; fi
tmp=$(mktemp "$f.ccm-tmp.XXXXXX")
trap 'rm -f "$tmp"' EXIT
printf '%%s' %s | base64 -d > "$tmp"
chmod %o "$tmp"
mv "$tmp" "$f"
trap - EXIT`, shQuote(p), shQuote(base64.StdEncoding.EncodeToString(data)), mode.Perm())
	res, err := s.Run(ctx, script)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("write %s on %s: %s", path.Clean(p), s.Host, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func (s SSHRemote) check(ctx context.Context, name string, args []string) error {
	res, err := s.R.Run(ctx, name, args, nil, false)
	if err != nil {
		return fmt.Errorf("%s to %s: %w", name, s.Host, err)
	}
	if res.Code != 0 {
		return fmt.Errorf("%s to %s failed: %s", name, s.Host, strings.TrimSpace(res.Stderr))
	}
	return nil
}
