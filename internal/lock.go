package internal

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
// The holder writes its PID into the file on acquiring it, which also sets
// the file's mtime to the acquisition time (see runLockHolder).
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
			recordLockHolder(f)
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

// recordLockHolder replaces the lock file's content by this process's PID.
// Failing to do so is harmless for the lock itself, so it is only logged.
func recordLockHolder(f *os.File) {
	if err := f.Truncate(0); err != nil {
		log.Println("warning: truncate run lock:", err)
		return
	}
	if _, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0); err != nil {
		log.Println("warning: record run lock holder:", err)
	}
}

// runLockHolder returns the PID recorded in StateDir's lock file (0 when
// unreadable) and since when it has been held: the file's mtime, which the
// holder bumped when it took the lock.
func runLockHolder(stateDir string) (int, time.Time, error) {
	path := filepath.Join(stateDir, runLockFile)
	info, err := os.Stat(path)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("stat run lock: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, info.ModTime(), fmt.Errorf("read run lock: %w", err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid, info.ModTime(), nil
}
