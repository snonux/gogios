package internal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// runLockFile is the lock file in StateDir that serialises Gogios runs.
const runLockFile = "gogios.lock"

// runLockPoll is how often a waiting run retries the lock.
const runLockPoll = time.Second

// errRunLocked is returned by acquireRunLock when another run holds the lock
// and the caller asked not to wait.
var errRunLocked = errors.New("another gogios run holds the lock")

// acquireRunLock takes an exclusive flock on StateDir/gogios.lock, so runs
// started by overlapping cron jobs (the */5 checks and -renotify or -force)
// never read-modify-write the state and report files concurrently. With wait
// false it fails with errRunLocked when the lock is taken; with wait true it
// retries until ctx ends. The lock is tied to the open descriptor, so the
// kernel releases it even if the process dies without calling release.
func acquireRunLock(ctx context.Context, stateDir string, wait bool) (func(), error) {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("create state directory %s: %w", stateDir, err)
	}
	path := filepath.Join(stateDir, runLockFile)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open run lock %s: %w", path, err)
	}
	release := func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}

	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return release, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if !wait {
			_ = f.Close()
			return nil, errRunLocked
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("wait for run lock %s: %w", path, ctx.Err())
		case <-time.After(runLockPoll):
		}
	}
}
