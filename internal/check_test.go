package internal

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A plugin whose forked child keeps stdout open must not keep the check (and
// with it the run and its lock) waiting after the kill: WaitDelay ends it.
func TestRunCommandForkedChildHoldsStdout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := runCommand(ctx, 200*time.Millisecond, "/bin/sh", "-c", "(sleep 5) & sleep 5")
	if err == nil {
		t.Fatal("killed plugin: want an error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("runCommand waited %v for the forked child", elapsed)
	}
}

func TestCheckRun(t *testing.T) {
	tests := []struct {
		name       string
		chk        check
		timeout    time.Duration
		wantStatus nagiosCode
		wantOutput string
	}{
		{"ok strips perf data", check{Plugin: "/bin/sh", Args: []string{"-c", "echo 'OK - fine | load=1'"}}, 5 * time.Second, nagiosOk, "OK - fine"},
		{"critical", check{Plugin: "/bin/sh", Args: []string{"-c", "echo bad; exit 2"}}, 5 * time.Second, nagiosCritical, "bad"},
		{"exit code out of range", check{Plugin: "/bin/sh", Args: []string{"-c", "exit 7"}}, 5 * time.Second, nagiosUnknown, ""},
		{"missing plugin", check{Plugin: "/nonexistent/plugin"}, 5 * time.Second, nagiosUnknown, ""},
		{"timeout", check{Plugin: "/bin/sh", Args: []string{"-c", "sleep 5"}}, 100 * time.Millisecond, nagiosCritical, "Check command timed out"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()
			got := tt.chk.run(ctx, "X")
			if got.status != tt.wantStatus || got.output != tt.wantOutput {
				t.Fatalf("run = %v %q, want %v %q", got.status, got.output, tt.wantStatus, tt.wantOutput)
			}
		})
	}
}

// Retries and the random spread end with the run's context instead of
// sleeping past the deadline.
func TestRunCheckHonoursRunDeadline(t *testing.T) {
	conf := config{CheckTimeoutS: 5, CheckConcurrency: 1}
	tests := []struct {
		name       string
		chk        check
		wantStatus nagiosCode
		wantOutput string
	}{
		{"retry interval", check{Plugin: "/bin/sh", Args: []string{"-c", "echo down; exit 2"}, Retries: 3, RetryInterval: 60}, nagiosCritical, "down"},
		{"random spread", check{Plugin: "/bin/true", RandomSpread: 100_000_000}, nagiosUnknown, "Run deadline reached"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			deps := newDependency(config{Checks: map[string]check{"X": tt.chk}})
			start := time.Now()
			got := runCheck(ctx, make(chan struct{}, 1), deps, namedCheck{tt.chk, "X"}, conf, tt.chk.Retries)
			if elapsed := time.Since(start); elapsed > 3*time.Second {
				t.Fatalf("runCheck took %v past the run deadline", elapsed)
			}
			if got.status != tt.wantStatus || !strings.HasPrefix(got.output, tt.wantOutput) {
				t.Fatalf("runCheck = %v %q, want %v %q", got.status, got.output, tt.wantStatus, tt.wantOutput)
			}
		})
	}
}

// A retry that succeeds replaces the failed result.
func TestRunCheckRetrySucceeds(t *testing.T) {
	marker := t.TempDir() + "/tried"
	chk := check{Plugin: "/bin/sh", Args: []string{"-c", "test -e " + marker + " && exit 0; touch " + marker + "; exit 2"}, Retries: 1}
	deps := newDependency(config{Checks: map[string]check{"X": chk}})
	got := runCheck(context.Background(), make(chan struct{}, 1), deps, namedCheck{chk, "X"}, config{CheckTimeoutS: 5}, chk.Retries)
	if got.status != nagiosOk {
		t.Fatalf("retried check = %v %q, want OK", got.status, got.output)
	}
}
