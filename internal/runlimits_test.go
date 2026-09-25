package internal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// holdLock takes the run lock in dir and back-dates it by age.
func holdLock(t *testing.T, dir string, age time.Duration) {
	t.Helper()
	release, err := acquireRunLock(context.Background(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	since := time.Now().Add(-age)
	if err := os.Chtimes(filepath.Join(dir, runLockFile), since, since); err != nil {
		t.Fatal(err)
	}
}

func TestRunLockHolderRecordsPID(t *testing.T) {
	dir := t.TempDir()
	holdLock(t, dir, 0)
	pid, since, err := runLockHolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pid != os.Getpid() || time.Since(since) > time.Minute {
		t.Fatalf("holder = PID %d since %v, want PID %d now", pid, since, os.Getpid())
	}
	if _, _, err := runLockHolder(t.TempDir()); err == nil {
		t.Fatal("no lock file: want an error")
	}
}

func TestHandleLockedRun(t *testing.T) {
	const staleAfter = 10 * time.Minute
	tests := []struct {
		name      string
		age       time.Duration
		noLock    bool
		wantErr   bool
		wantMails int
	}{
		{"busy run skips quietly", time.Minute, false, false, 0},
		{"stuck lock is loud", time.Hour, false, true, 1},
		{"lock file gone skips quietly", 0, true, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if !tt.noLock {
				holdLock(t, dir, tt.age)
			}
			mails := 0
			send := func(subject, body string) error {
				mails++
				if !strings.Contains(body, "PID") {
					t.Errorf("mail body %q names no PID", body)
				}
				return nil
			}
			err := handleLockedRun(config{StateDir: dir, hostname: "h"}, time.Now(), staleAfter, send)
			if (err != nil) != tt.wantErr || mails != tt.wantMails {
				t.Fatalf("err = %v, mails = %d; want err %v, mails %d", err, mails, tt.wantErr, tt.wantMails)
			}
		})
	}
}

// The stuck-lock mail goes out at most once per lockAlertInterval; a failed
// mail is retried by the next skipped run.
func TestHandleLockedRunRateLimitsMail(t *testing.T) {
	dir := t.TempDir()
	holdLock(t, dir, time.Hour)
	conf := config{StateDir: dir, hostname: "h"}
	now := time.Now()
	mails := 0
	failing := func(string, string) error { mails++; return errors.New("smtp down") }
	working := func(string, string) error { mails++; return nil }

	steps := []struct {
		at        time.Time
		send      func(string, string) error
		wantMails int
	}{
		{now, failing, 1},                                // tried, failed: no stamp
		{now.Add(5 * time.Minute), working, 2},           // retried, sent
		{now.Add(10 * time.Minute), working, 2},          // within the interval
		{now.Add(5*time.Minute + time.Hour), working, 3}, // interval over
	}
	for i, step := range steps {
		if err := handleLockedRun(conf, step.at, 10*time.Minute, step.send); err == nil {
			t.Fatalf("step %d: stuck lock must return an error", i)
		}
		if mails != step.wantMails {
			t.Fatalf("step %d: %d mails, want %d", i, mails, step.wantMails)
		}
	}
}

// A plain Run behind a stuck lock fails (stderr, exit 1) instead of skipping
// silently.
func TestRunFailsBehindStuckLock(t *testing.T) {
	dir := t.TempDir()
	cfg := writeRunConfig(t, dir, `"A":{"Plugin":"/bin/true"}`)
	holdLock(t, dir, time.Hour)
	err := Run(context.Background(), RunOptions{ConfigFile: cfg, LockWait: time.Second, Timeout: time.Minute})
	if err == nil || !strings.Contains(err.Error(), "not monitoring") {
		t.Fatalf("Run behind a stuck lock: err = %v", err)
	}
}

// The check budget starts once the lock is held: a -renotify that waited
// for a slow run still gets its full Timeout for the checks, while a lock
// held past LockWait fails the wait.
func TestRunSeparatesLockWaitFromTimeout(t *testing.T) {
	dir := t.TempDir()
	cfg := writeRunConfig(t, dir, `"A":{"Plugin":"/bin/sh","Args":["-c","sleep 0.5; echo fine"]}`)
	release, err := acquireRunLock(context.Background(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	opts := RunOptions{ConfigFile: cfg, Renotify: true, LockWait: 200 * time.Millisecond, Timeout: time.Second}
	if err := Run(context.Background(), opts); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock held past LockWait: err = %v, want DeadlineExceeded", err)
	}

	go func() {
		time.Sleep(800 * time.Millisecond)
		release()
	}()
	opts.LockWait = 5 * time.Second
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	s, err := newState(config{StateDir: dir, Checks: map[string]check{"A": {}}})
	if err != nil {
		t.Fatal(err)
	}
	if cs := s.checks["A"]; cs.Status != nagiosOk {
		t.Fatalf("check after waiting for the lock = %+v, want OK (not timed out)", cs)
	}
}

func TestStartWatchdog(t *testing.T) {
	fired := make(chan struct{})
	startWatchdog(10*time.Millisecond, func() { close(fired) })
	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not fire")
	}

	stop := startWatchdog(50*time.Millisecond, func() { t.Error("stopped watchdog fired") })
	stop()
	time.Sleep(100 * time.Millisecond)
}

func TestRemoveStaleTemps(t *testing.T) {
	dir := t.TempDir()
	html := filepath.Join(dir, "www", "index.html")
	if err := os.MkdirAll(filepath.Dir(html), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := []string{
		filepath.Join(dir, "state.json.123.tmp"),
		filepath.Join(dir, "report.txt.9.tmp"),
		filepath.Join(dir, notifyStateFile+".1.tmp"),
		html + ".77.tmp",
		filepath.Join(dir, "www", "index.json.5.tmp"),
	}
	kept := []string{filepath.Join(dir, "other.1.tmp"), filepath.Join(dir, "state.json")}
	for _, f := range append(stale, kept...) {
		if err := os.WriteFile(f, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	removeStaleTemps(config{StateDir: dir, HTMLStatusFile: html})
	for _, f := range stale {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("%s not removed", f)
		}
	}
	for _, f := range kept {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s removed: %v", f, err)
		}
	}
}

// writeRunConfig writes a mail- and HTML-less config with checks (JSON
// members of "Checks") into dir and returns its path.
func writeRunConfig(t *testing.T, dir, checks string) string {
	t.Helper()
	cfg := filepath.Join(dir, "gogios.json")
	conf := `{"EmailTo":"x","EmailFrom":"y","SMTPDisable":true,"HTMLDisable":true,"StateDir":"` + dir + `",
		"CheckTimeoutS":5,"CheckConcurrency":1,"Checks":{` + checks + `}}`
	if err := os.WriteFile(cfg, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}
