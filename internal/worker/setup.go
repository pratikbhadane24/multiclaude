package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/consts"
	"github.com/pratikbhadane24/multiclaude/internal/mcp"
	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/run"
)

// Remote layout on a worker (relative to its home).
const (
	remoteCCMDir    = ".local/share/ccm"
	remoteCCM       = "$HOME/.local/bin/ccm"
	remoteClaudeDir = ".claude"
	remoteSettings  = ".claude/settings.json"
	remoteMCPHub    = ".claude-accounts/" + mcp.HubFileName
	shimBinName     = "ccm-shim"
	privateMode     = 0o600
	shellFileMode   = 0o644
	claudeInstaller = "curl -fsSL https://claude.ai/install.sh | bash"
)

// pushExcludes are never copied to a worker, whatever the item.
var pushExcludes = []string{".credentials.json", "*.ccm-backup-*", ".DS_Store"}

// safeEmail accepts emails that need no shell quoting.
var safeEmail = regexp.MustCompile(`^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+$`)

// ErrDeclined means the user said no to a confirmation.
var ErrDeclined = errors.New("declined")

// Info is what Probe learned about a worker.
type Info struct {
	OS, Arch, Home                                     string
	HasGit, HasClang, HasClaude, HasNPX, HasUVX, HasGH bool
}

// Setup performs the `ccm worker add/sync` steps against one worker.
type Setup struct {
	P       paths.Paths
	R       Remote
	Out     io.Writer
	Yes     bool
	Confirm func(question string) bool
	// Local runs commands on the hub (reading its git config and gh version).
	Local run.Runner
	// Home is the worker's home dir, filled in by Probe.
	Home      string
	info      Info
	pollEvery time.Duration
}

// Step is one named stage of worker setup.
type Step struct {
	Name string
	Run  func(ctx context.Context) error
}

// RunSteps runs steps in order, printing ✓/✗ for each, and stops at the
// first failure.
func RunSteps(ctx context.Context, out io.Writer, steps []Step) error {
	for _, st := range steps {
		if err := st.Run(ctx); err != nil {
			fmt.Fprintf(out, "✗ %s: %v\n", st.Name, err)
			return fmt.Errorf("%s: %w", st.Name, err)
		}
		fmt.Fprintf(out, "✓ %s\n", st.Name)
	}
	return nil
}

func hubOS() string { return map[string]string{"darwin": "Darwin", "linux": "Linux"}[runtime.GOOS] }

func hubArch() string {
	return map[string]string{"arm64": "arm64", "amd64": "x86_64"}[runtime.GOARCH]
}

const probeScript = `echo "os=$(uname -s)"; echo "arch=$(uname -m)"; echo "home=$HOME"
export PATH="$HOME/.local/bin:$PATH"
for t in git clang claude npx uvx gh; do if command -v "$t" >/dev/null 2>&1; then echo "$t=1"; else echo "$t=0"; fi; done`

// Probe checks the worker's OS, architecture and tools. A worker whose
// OS or architecture differs from the hub's is refused.
func (s *Setup) Probe(ctx context.Context) (Info, error) {
	res, err := s.R.Run(ctx, probeScript)
	if err != nil {
		return Info{}, err
	}
	kv := parseKV(res.Stdout)
	info := Info{
		OS: kv["os"], Arch: kv["arch"], Home: kv["home"],
		HasGit: kv["git"] == "1", HasClang: kv["clang"] == "1", HasClaude: kv["claude"] == "1",
		HasNPX: kv["npx"] == "1", HasUVX: kv["uvx"] == "1", HasGH: kv["gh"] == "1",
	}
	if info.OS != hubOS() || info.Arch != hubArch() {
		return info, fmt.Errorf("worker is %s/%s but this ccm is %s/%s; only matching workers are supported",
			info.OS, info.Arch, hubOS(), hubArch())
	}
	if info.Home == "" {
		return info, errors.New("could not read the worker's home directory")
	}
	if !info.HasGit {
		return info, errors.New("git is missing on the worker; run `xcode-select --install` there")
	}
	s.Home, s.info = info.Home, info
	return info, nil
}

func parseKV(out string) map[string]string {
	kv := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			kv[k] = v
		}
	}
	return kv
}

// InstallClaude installs Claude Code with the official installer when the
// worker has none.
func (s *Setup) InstallClaude(ctx context.Context) error {
	if s.info.HasClaude {
		return nil
	}
	return s.runOK(ctx, claudeInstaller)
}

// InstallCCM copies this ccm and its shim to the worker and runs
// `ccm install` there.
func (s *Setup) InstallCCM(ctx context.Context, selfExe string) error {
	shim := filepath.Join(filepath.Dir(selfExe), shimBinName)
	if _, err := os.Stat(shim); err != nil {
		return fmt.Errorf("%s not found next to %s (build both with `make build`)", shimBinName, selfExe)
	}
	if err := s.runOK(ctx, "mkdir -p "+remoteCCMDir); err != nil {
		return err
	}
	// Copy beside the live binaries and rename over them: rewriting a
	// signed Mach-O in place gets it killed on its next run.
	var swap []string
	for _, f := range []string{selfExe, shim} {
		dest := remoteCCMDir + "/" + filepath.Base(f)
		if err := s.R.Copy(ctx, f, dest+".new"); err != nil {
			return err
		}
		swap = append(swap, fmt.Sprintf("chmod 755 %[1]s.new && mv -f %[1]s.new %[1]s", dest))
	}
	return s.runOK(ctx, strings.Join(swap, " && ")+" && "+remoteCCMDir+"/ccm install")
}

// ShellBlocks adds ccm's marked blocks to the worker's ~/.zshenv (PATH for
// non-interactive SSH) and ~/.zshrc (shell integration for Orca panes),
// after one confirmation. Unchanged files are not written.
func (s *Setup) ShellBlocks(ctx context.Context) error {
	type change struct {
		path string
		data []byte
	}
	var changes []change
	for _, f := range []struct{ path, body string }{{".zshenv", ZshenvBody()}, {".zshrc", ZshrcBody()}} {
		old, _, err := s.R.Read(ctx, f.path)
		if err != nil {
			return err
		}
		updated := UpsertBlock(string(old), f.body)
		if updated != string(old) {
			changes = append(changes, change{f.path, []byte(updated)})
		}
	}
	if len(changes) == 0 {
		return nil
	}
	if !s.Yes && (s.Confirm == nil || !s.Confirm("Add ccm's block to the worker's ~/.zshenv and ~/.zshrc?")) {
		return ErrDeclined
	}
	for _, c := range changes {
		if err := s.R.Write(ctx, c.path, c.data, shellFileMode); err != nil {
			return err
		}
	}
	return nil
}

// PushSetup copies the hub's shared setup (never history or logins),
// merges settings.json, writes the MCP source file and turns on auto-pick.
func (s *Setup) PushSetup(ctx context.Context) error {
	if err := s.pushItems(ctx); err != nil {
		return err
	}
	if err := s.pushSettings(ctx); err != nil {
		return err
	}
	if err := s.pushMCP(ctx); err != nil {
		return err
	}
	return s.runOK(ctx, remoteCCM+" config set default-account best")
}

// pushItems sends each shared item that exists on the hub: in one tar
// stream for items the worker mostly lacks (first push), else by rsync.
func (s *Setup) pushItems(ctx context.Context) error {
	var present []string
	local := map[string]int{}
	for _, item := range pushItemNames() {
		n, err := countFiles(filepath.Join(s.P.ClaudeDir, item))
		if err != nil {
			continue
		}
		present = append(present, item)
		local[item] = n
	}
	remote, err := s.remoteCounts(ctx, present)
	if err != nil {
		return err
	}
	var bulk []string
	for _, item := range present {
		if local[item] > 0 && remote[item]*2 < local[item] {
			bulk = append(bulk, item)
			continue
		}
		if err := s.R.Rsync(ctx, filepath.Join(s.P.ClaudeDir, item), remoteClaudeDir, pushExcludes); err != nil {
			return err
		}
	}
	if len(bulk) == 0 {
		return nil
	}
	return s.R.Tar(ctx, s.P.ClaudeDir, bulk, remoteClaudeDir, pushExcludes)
}

// countFiles counts regular files under path (1 for a file).
func countFiles(path string) (int, error) {
	n := 0
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			n++
		}
		return nil
	})
	return n, err
}

// remoteCounts returns the number of files under each item on the worker.
func (s *Setup) remoteCounts(ctx context.Context, items []string) (map[string]int, error) {
	out := map[string]int{}
	if len(items) == 0 {
		return out, nil
	}
	quoted := make([]string, len(items))
	for i, it := range items {
		quoted[i] = shQuote(it)
	}
	script := "cd " + remoteClaudeDir + " 2>/dev/null || exit 0\nfor i in " + strings.Join(quoted, " ") +
		`; do echo "$i=$(find "$i" -type f 2>/dev/null | wc -l | tr -d ' ')"; done`
	res, err := s.R.Run(ctx, script)
	if err != nil {
		return nil, err
	}
	for k, v := range parseKV(res.Stdout) {
		if n, err := strconv.Atoi(v); err == nil {
			out[k] = n
		}
	}
	return out, nil
}

// pushItemNames are the shared items minus history and settings.json.
func pushItemNames() []string {
	var out []string
	for _, it := range consts.SharedItems {
		if it == "settings.json" || slices.Contains(consts.HistoryItems, it) {
			continue
		}
		out = append(out, it)
	}
	return out
}

func (s *Setup) pushSettings(ctx context.Context) error {
	hub, err := os.ReadFile(filepath.Join(s.P.ClaudeDir, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read hub settings.json: %w", err)
	}
	cur, _, err := s.R.Read(ctx, remoteSettings)
	if err != nil {
		return err
	}
	merged, err := MergeSettings(hub, cur, s.Home+"/.local/bin/ccm")
	if err != nil {
		return err
	}
	return s.writeIfChanged(ctx, remoteSettings, cur, merged)
}

func (s *Setup) pushMCP(ctx context.Context) error {
	data, err := os.ReadFile(s.P.ClaudeJSON)
	if err != nil {
		return fmt.Errorf("read hub %s: %w", s.P.ClaudeJSON, err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return fmt.Errorf("parse hub %s: %w", s.P.ClaudeJSON, err)
	}
	servers := top["mcpServers"]
	if len(servers) == 0 {
		servers = json.RawMessage("{}")
	}
	out, err := json.MarshalIndent(map[string]json.RawMessage{"mcpServers": servers}, "", "  ")
	if err != nil {
		return err
	}
	cur, _, err := s.R.Read(ctx, remoteMCPHub)
	if err != nil {
		return err
	}
	return s.writeIfChanged(ctx, remoteMCPHub, cur, append(out, '\n'))
}

func (s *Setup) writeIfChanged(ctx context.Context, path string, cur, next []byte) error {
	if bytes.Equal(cur, next) {
		return nil
	}
	return s.R.Write(ctx, path, next, privateMode)
}

// ListEntry is one row of the worker's `ccm list --json`.
type ListEntry struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	LoginEmail string `json:"loginEmail"`
	LoggedIn   bool   `json:"loggedIn"`
}

// loggedInAs reports whether the entry is signed in as email. An entry
// without loginEmail (older ccm) is trusted when logged in.
func (e ListEntry) loggedInAs(email string) bool {
	return e.LoggedIn && (e.LoginEmail == "" || strings.EqualFold(e.LoginEmail, email))
}

// RemoteAccounts returns the worker's accounts and their login state.
func (s *Setup) RemoteAccounts(ctx context.Context) ([]ListEntry, error) {
	res, err := s.R.Run(ctx, remoteCCM+" list --json")
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, fmt.Errorf("ccm list on the worker failed: %s", strings.TrimSpace(res.Stderr))
	}
	var out []ListEntry
	if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
		return nil, fmt.Errorf("parse worker account list: %w", err)
	}
	return out, nil
}

// EnsureAccounts logs in every named account that the worker does not
// have yet, interactively (`ccm add` over ssh -t). main is refused: worker
// accounts always use the hub's names.
func (s *Setup) EnsureAccounts(ctx context.Context, names []string, emails map[string]string) error {
	if slices.Contains(names, consts.MainName) {
		return errors.New("main cannot be a worker account; name the hub account instead (e.g. inbox2)")
	}
	have, err := s.RemoteAccounts(ctx)
	if err != nil {
		return err
	}
	for _, name := range names {
		if i := slices.IndexFunc(have, func(e ListEntry) bool { return e.Name == name }); i >= 0 && have[i].loggedInAs(emails[name]) {
			continue
		}
		fmt.Fprintf(s.Out, "→ log in %s on the worker: open the link it prints in the browser signed in to %s.\n"+
			"  After you approve, the browser hands the login to the worker through an SSH tunnel.\n", name, emails[name])
		if !consts.AccountNameRE.MatchString(name) || !safeEmail.MatchString(emails[name]) {
			return fmt.Errorf("invalid account name or email for %q", name)
		}
		cmd := fmt.Sprintf("%s add %s --email %s --ignore-main", remoteCCM, name, emails[name])
		err := s.withCallbackForward(ctx, func(ctx context.Context) error {
			code, err := s.R.Interactive(ctx, cmd)
			if err == nil && code != 0 {
				err = fmt.Errorf("login of %s on the worker exited %d", name, code)
			}
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Doctor runs `ccm doctor` on the worker and shows its output.
func (s *Setup) Doctor(ctx context.Context) error {
	res, err := s.R.Run(ctx, remoteCCM+" doctor")
	if err != nil {
		return err
	}
	fmt.Fprint(s.Out, indent(res.Stdout+res.Stderr))
	if res.Code != 0 {
		return fmt.Errorf("ccm doctor on the worker reported problems (exit %d)", res.Code)
	}
	return nil
}

func indent(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	return "    " + strings.ReplaceAll(s, "\n", "\n    ") + "\n"
}

func (s *Setup) runOK(ctx context.Context, script string) error {
	res, err := s.R.Run(ctx, script)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return fmt.Errorf("`%s` exited %d: %s", script, res.Code, msg)
	}
	return nil
}
