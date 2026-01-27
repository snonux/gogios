package internal

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewNotifyState_FirstRun(t *testing.T) {
	// First run with no existing state file should return empty state
	tmpDir := t.TempDir()
	ns, err := newNotifyState(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ns.LastNotifyEpoch != 0 {
		t.Errorf("expected LastNotifyEpoch=0, got %d", ns.LastNotifyEpoch)
	}
	if len(ns.CheckStates) != 0 {
		t.Errorf("expected empty CheckStates, got %d entries", len(ns.CheckStates))
	}
}

func TestNotifyState_Persistence(t *testing.T) {
	// Test round-trip: save and load notification state
	tmpDir := t.TempDir()
	ns, err := newNotifyState(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create a mock state and record notification
	mockState := state{checks: map[string]checkState{
		"Check A": {Status: nagiosOk},
		"Check B": {Status: nagiosCritical},
	}}

	if err := ns.recordNotification(mockState); err != nil {
		t.Fatalf("failed to record notification: %v", err)
	}

	// Load the state back
	ns2, err := newNotifyState(tmpDir)
	if err != nil {
		t.Fatalf("failed to reload state: %v", err)
	}

	if ns2.LastNotifyEpoch == 0 {
		t.Error("expected non-zero LastNotifyEpoch after reload")
	}
	if len(ns2.CheckStates) != 2 {
		t.Errorf("expected 2 check states, got %d", len(ns2.CheckStates))
	}
	if ns2.CheckStates["Check A"] != int(nagiosOk) {
		t.Errorf("expected Check A status=%d, got %d", nagiosOk, ns2.CheckStates["Check A"])
	}
	if ns2.CheckStates["Check B"] != int(nagiosCritical) {
		t.Errorf("expected Check B status=%d, got %d", nagiosCritical, ns2.CheckStates["Check B"])
	}
}

func TestIntervalElapsed_FirstRun(t *testing.T) {
	// First run (LastNotifyEpoch=0) should always return true
	ns := notifyState{LastNotifyEpoch: 0}
	if !ns.intervalElapsed(3600) {
		t.Error("expected intervalElapsed=true on first run")
	}
}

func TestIntervalElapsed_NotYet(t *testing.T) {
	// Notification sent 30 seconds ago, interval is 60 seconds
	ns := notifyState{LastNotifyEpoch: time.Now().Unix() - 30}
	if ns.intervalElapsed(60) {
		t.Error("expected intervalElapsed=false when only 30s of 60s elapsed")
	}
}

func TestIntervalElapsed_Elapsed(t *testing.T) {
	// Notification sent 120 seconds ago, interval is 60 seconds
	ns := notifyState{LastNotifyEpoch: time.Now().Unix() - 120}
	if !ns.intervalElapsed(60) {
		t.Error("expected intervalElapsed=true when 120s of 60s elapsed")
	}
}

func TestIntervalElapsed_ZeroInterval(t *testing.T) {
	// Interval of 0 should always return true (immediate notification mode)
	ns := notifyState{LastNotifyEpoch: time.Now().Unix()}
	if !ns.intervalElapsed(0) {
		t.Error("expected intervalElapsed=true when interval is 0")
	}
}

func TestHasChanges_NoChanges(t *testing.T) {
	ns := notifyState{
		CheckStates: map[string]int{
			"Check A": int(nagiosOk),
			"Check B": int(nagiosCritical),
		},
	}

	currentState := state{checks: map[string]checkState{
		"Check A": {Status: nagiosOk},
		"Check B": {Status: nagiosCritical},
	}}

	if ns.hasChanges(currentState) {
		t.Error("expected hasChanges=false when states are identical")
	}
}

func TestHasChanges_StatusChanged(t *testing.T) {
	ns := notifyState{
		CheckStates: map[string]int{
			"Check A": int(nagiosOk),
			"Check B": int(nagiosOk),
		},
	}

	currentState := state{checks: map[string]checkState{
		"Check A": {Status: nagiosOk},
		"Check B": {Status: nagiosCritical}, // Changed from OK to CRITICAL
	}}

	if !ns.hasChanges(currentState) {
		t.Error("expected hasChanges=true when check status changed")
	}
}

func TestHasChanges_NewCheck(t *testing.T) {
	ns := notifyState{
		CheckStates: map[string]int{
			"Check A": int(nagiosOk),
		},
	}

	currentState := state{checks: map[string]checkState{
		"Check A": {Status: nagiosOk},
		"Check B": {Status: nagiosCritical}, // New check
	}}

	if !ns.hasChanges(currentState) {
		t.Error("expected hasChanges=true when new check added")
	}
}

func TestHasChanges_RemovedCheck(t *testing.T) {
	ns := notifyState{
		CheckStates: map[string]int{
			"Check A": int(nagiosOk),
			"Check B": int(nagiosCritical),
		},
	}

	currentState := state{checks: map[string]checkState{
		"Check A": {Status: nagiosOk},
		// Check B removed
	}}

	if !ns.hasChanges(currentState) {
		t.Error("expected hasChanges=true when check removed")
	}
}

func TestHasChanges_EmptyPrevious(t *testing.T) {
	// First notification - no previous state
	ns := notifyState{
		CheckStates: map[string]int{},
	}

	currentState := state{checks: map[string]checkState{
		"Check A": {Status: nagiosOk},
	}}

	if !ns.hasChanges(currentState) {
		t.Error("expected hasChanges=true on first run with checks")
	}
}

func TestHasChanges_BothEmpty(t *testing.T) {
	ns := notifyState{
		CheckStates: map[string]int{},
	}

	currentState := state{checks: map[string]checkState{}}

	if ns.hasChanges(currentState) {
		t.Error("expected hasChanges=false when both states are empty")
	}
}

func TestRecordNotification(t *testing.T) {
	tmpDir := t.TempDir()
	ns, _ := newNotifyState(tmpDir)

	mockState := state{checks: map[string]checkState{
		"Check A": {Status: nagiosOk},
		"Check B": {Status: nagiosWarning},
		"Check C": {Status: nagiosCritical},
	}}

	beforeRecord := time.Now().Unix()
	if err := ns.recordNotification(mockState); err != nil {
		t.Fatalf("failed to record notification: %v", err)
	}
	afterRecord := time.Now().Unix()

	// Verify timestamp is within expected range
	if ns.LastNotifyEpoch < beforeRecord || ns.LastNotifyEpoch > afterRecord {
		t.Errorf("LastNotifyEpoch=%d not in range [%d, %d]", ns.LastNotifyEpoch, beforeRecord, afterRecord)
	}

	// Verify all check states were captured
	if len(ns.CheckStates) != 3 {
		t.Errorf("expected 3 check states, got %d", len(ns.CheckStates))
	}

	// Verify state file was created
	stateFile := filepath.Join(tmpDir, "notify_state.json")
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		t.Error("expected state file to be created")
	}
}
