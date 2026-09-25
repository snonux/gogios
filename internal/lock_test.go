package internal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunLockExclusive(t *testing.T) {
	dir := t.TempDir()
	release, err := acquireRunLock(context.Background(), dir, false)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := acquireRunLock(context.Background(), dir, false); !errors.Is(err, errRunLocked) {
		t.Fatalf("second non-waiting acquire: err = %v, want errRunLocked", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := acquireRunLock(ctx, dir, true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting acquire past its deadline: err = %v, want DeadlineExceeded", err)
	}

	release()
	release2, err := acquireRunLock(context.Background(), dir, false)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	release2()
}

// A waiting run (-renotify, -force) gets the lock once the holder is done.
func TestRunLockWaitsForRelease(t *testing.T) {
	dir := t.TempDir()
	release, err := acquireRunLock(context.Background(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		release()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	release2, err := acquireRunLock(ctx, dir, true)
	if err != nil {
		t.Fatalf("waiting acquire: %v", err)
	}
	defer release2()
	if time.Since(start) < 100*time.Millisecond {
		t.Error("acquired the lock before the holder released it")
	}
}

func TestRunLockBadStateDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := acquireRunLock(context.Background(), file, false); err == nil || errors.Is(err, errRunLocked) {
		t.Fatalf("err = %v, want a directory error", err)
	}
}

// A second plain run skips (returns nil) while another run holds the lock,
// without touching the state.
func TestRunSkipsWhileLocked(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gogios.json")
	conf := `{"EmailTo":"x","EmailFrom":"y","SMTPDisable":true,"HTMLDisable":true,"StateDir":"` + dir + `",
		"CheckTimeoutS":5,"CheckConcurrency":1,"Checks":{"A":{"Plugin":"/bin/true"}}}`
	if err := os.WriteFile(cfg, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	release, err := acquireRunLock(context.Background(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), cfg, false, false); err != nil {
		t.Fatalf("locked plain run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); !os.IsNotExist(err) {
		t.Fatalf("a skipped run must not write state (stat err %v)", err)
	}
	release()

	if err := Run(context.Background(), cfg, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		t.Fatalf("unlocked run wrote no state: %v", err)
	}
}
