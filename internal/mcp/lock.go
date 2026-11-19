package mcp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	lockFileName     = ".mcp-sync.lock"
	defaultLockWait  = 2 * time.Second
	lockPollInterval = 10 * time.Millisecond
	dirMode          = 0o700
)

// ErrLockTimeout means another ccm process held the sync lock for too long;
// the sync is skipped rather than delaying Claude's start.
var ErrLockTimeout = errors.New("mcp sync lock busy")

// lockWait is a var so tests can shorten it.
var lockWait = defaultLockWait

// acquireLock takes an exclusive advisory flock on path, waiting at most
// lockWait. The returned func releases it.
func acquireLock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	deadline := time.Now().Add(lockWait)
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
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("%w after %v (%s)", ErrLockTimeout, lockWait, path)
		}
		time.Sleep(lockPollInterval)
	}
}
