package status

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
)

const (
	cacheLockName     = "usage.lock"
	cacheDirMode      = 0o700
	cacheLockWait     = 2 * time.Second
	cacheLockPollTime = 10 * time.Millisecond
)

// errCacheLockBusy means another ccm process held the cache lock too long.
var errCacheLockBusy = errors.New("usage cache lock busy")

// lockCache takes an exclusive advisory flock on <AccountsRoot>/cache/usage.lock,
// waiting at most cacheLockWait. The kernel drops the lock if the holder
// dies, so a stale lock cannot outlive its process. The func releases it.
func lockCache(p paths.Paths) (func(), error) {
	path := filepath.Join(p.AccountsRoot, cacheDirName, cacheLockName)
	if err := os.MkdirAll(filepath.Dir(path), cacheDirMode); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, cacheFileMode)
	if err != nil {
		return nil, fmt.Errorf("open cache lock: %w", err)
	}
	deadline := time.Now().Add(cacheLockWait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()
			return nil, fmt.Errorf("lock usage cache: %w", err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, errCacheLockBusy
		}
		time.Sleep(cacheLockPollTime)
	}
}
