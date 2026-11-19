package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"multiclaude/internal/fsutil"
	"multiclaude/internal/status"
	"multiclaude/internal/statusline"
	"multiclaude/internal/usage"
)

const (
	settingsFile     = "settings.json"
	settingsNewMode  = 0o600
	maxBackupTries   = 100
	backupSuffixText = ".ccm-backup-"
	statuslineUsage  = "statusline [--wrap \"<cmd>\"] | statusline install | statusline uninstall"
)

// startRefresh launches the detached cache refresher; tests replace it.
var startRefresh = statusline.StartDetached

// cmdStatusline renders the status line, or installs/uninstalls it.
func (a *App) cmdStatusline(ctx context.Context, args []string) int {
	if len(args) > 0 && (args[0] == "install" || args[0] == "uninstall") {
		if len(args) != 1 {
			return a.usageErr(statuslineUsage)
		}
		return a.statuslineEdit(ctx, args[0] == "install")
	}
	fs := a.newFlags("statusline")
	wrap := fs.String("wrap", "", "previous status line command to run first")
	if _, code, ok := a.parseArgs(fs, args, 0, statuslineUsage); !ok {
		return code
	}
	return statusline.Render(a.In, os.Getenv, a.statuslineDeps(*wrap), a.Out)
}

func (a *App) statuslineDeps(wrap string) statusline.Deps {
	d := statusline.Deps{
		Paths:     a.Paths,
		Now:       a.Now,
		ReadCache: func() (status.Cache, error) { return status.ReadCache(a.Paths) },
		Wrap:      wrap,
		RunWrap:   statusline.DefaultRunWrap,
	}
	if a.SelfExe != "" {
		d.SpawnRefresh = statusline.NewSpawner(a.Paths, a.Now, func() error { return startRefresh(a.SelfExe) })
	}
	return d
}

// finishStatusCache writes the usage cache after a status collection. With
// refreshOnly it is the whole command (quiet, exit code reported); otherwise a
// failed write is only a warning. done is true when the command is finished.
func (a *App) finishStatusCache(rows []status.Row, c usage.Combined, refreshOnly bool) (code int, done bool) {
	err := status.WriteCache(a.Paths, rows, c, a.Now())
	if refreshOnly {
		if err != nil {
			return a.errorf("%v", err), true
		}
		statusline.ReleaseLock(a.Paths)
		return ExitOK, true
	}
	if err != nil {
		a.warnf("usage cache: %v", err)
	}
	return ExitOK, false
}

func (a *App) statuslineEdit(ctx context.Context, install bool) int {
	if install && a.SelfExe == "" {
		return a.errorf("cannot determine the ccm binary path")
	}
	path := filepath.Join(a.Paths.ClaudeDir, settingsFile)
	data, mode, err := readSettings(path)
	if err != nil {
		return a.errorf("%v", err)
	}
	var edit statusline.Edit
	if install {
		edit, err = statusline.PlanInstall(data, a.SelfExe)
	} else {
		edit, err = statusline.PlanUninstall(data)
	}
	if err != nil {
		return a.errorf("%v", err)
	}
	if edit.NoOp {
		return a.noopMessage(install)
	}
	return a.confirmAndWrite(ctx, install, path, data, mode, edit)
}

func (a *App) noopMessage(install bool) int {
	if install {
		a.printf("already installed\n")
	} else {
		a.printf("not installed\n")
	}
	return ExitOK
}

func (a *App) confirmAndWrite(ctx context.Context, install bool, path string, data []byte, mode os.FileMode, e statusline.Edit) int {
	if e.Warn != "" {
		a.warnf("%s", e.Warn)
	}
	a.printf("%s statusLine:\n  before: %s\n  after:  %s\n", path, e.Before, e.After)
	verb := "Install ccm status line into"
	if !install {
		verb = "Remove ccm status line from"
	}
	answer, err := a.promptLine(ctx, fmt.Sprintf("%s ~/.claude/settings.json? [y/N] ", verb))
	if err != nil {
		if ctx.Err() != nil {
			return ExitInterrupted
		}
		return a.errorf("no answer: %v", err)
	}
	if ans := strings.ToLower(answer); ans != "y" && ans != "yes" {
		a.printf("no changes made\n")
		return ExitOK
	}
	return a.applySettings(path, data, mode, e.Content)
}

func (a *App) applySettings(path string, old []byte, mode os.FileMode, content []byte) int {
	if old != nil {
		backup, err := writeBackup(path, old, mode, a.Now().Unix())
		if err != nil {
			return a.errorf("backup: %v", err)
		}
		a.printf("backup: %s\n", backup)
	}
	if err := fsutil.WriteAtomic(resolveTarget(path), content, mode); err != nil {
		return a.errorf("%v", err)
	}
	a.printf("updated %s\n", path)
	return ExitOK
}

// resolveTarget follows a symlinked settings.json so the link survives the
// atomic rename (dotfile managers keep it as a link).
func resolveTarget(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// writeBackup creates path.ccm-backup-<unix> exclusively, appending -1, -2...
// when that name is taken, and returns the name used.
func writeBackup(path string, data []byte, mode os.FileMode, unix int64) (string, error) {
	base := fmt.Sprintf("%s%s%d", path, backupSuffixText, unix)
	for n := 0; n < maxBackupTries; n++ {
		name := base
		if n > 0 {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(data)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		return name, werr
	}
	return "", fmt.Errorf("too many backups named %s", base)
}

// readSettings returns the file bytes and mode; a missing file yields nil data.
func readSettings(path string) ([]byte, os.FileMode, error) {
	st, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, settingsNewMode, nil
	}
	if err != nil {
		return nil, 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	return data, st.Mode().Perm(), nil
}
