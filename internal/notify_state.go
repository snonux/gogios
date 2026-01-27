package internal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// notifyState tracks the last notification timestamp and the check states at that time.
// This enables notification batching: Gogios can suppress emails until the configured
// interval has elapsed AND there's been an actual state change since the last notification.
type notifyState struct {
	stateFile       string         `json:"-"`
	LastNotifyEpoch int64          `json:"LastNotifyEpoch"`
	CheckStates     map[string]int `json:"CheckStates"` // check name -> status code at last notification
}

// newNotifyState loads the notification state from disk, or returns an empty state
// if no previous state exists (first run scenario).
func newNotifyState(stateDir string) (notifyState, error) {
	ns := notifyState{
		stateFile:   filepath.Join(stateDir, "notify_state.json"),
		CheckStates: make(map[string]int),
	}

	data, err := os.ReadFile(ns.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			// First run - no previous notification state
			return ns, nil
		}
		return ns, err
	}

	if err := json.Unmarshal(data, &ns); err != nil {
		return ns, err
	}

	return ns, nil
}

// intervalElapsed returns true if the minimum notification interval has passed
// since the last notification was sent.
func (ns notifyState) intervalElapsed(minIntervalS int) bool {
	if ns.LastNotifyEpoch == 0 {
		// No previous notification - interval is considered elapsed
		return true
	}
	elapsed := time.Now().Unix() - ns.LastNotifyEpoch
	return elapsed >= int64(minIntervalS)
}

// hasChanges compares the current check states to the snapshot taken at the last
// notification. Returns true if any check has changed status, if new checks were
// added, or if checks were removed.
func (ns notifyState) hasChanges(currentState state) bool {
	// Check for status changes or new checks
	for name, cs := range currentState.checks {
		prevStatus, exists := ns.CheckStates[name]
		if !exists {
			// New check appeared since last notification
			return true
		}
		if int(cs.Status) != prevStatus {
			// Status changed since last notification
			return true
		}
	}

	// Check for removed checks
	for name := range ns.CheckStates {
		if _, exists := currentState.checks[name]; !exists {
			return true
		}
	}

	return false
}

// recordNotification saves the current timestamp and a snapshot of all check states.
// Call this after successfully sending a notification email.
func (ns *notifyState) recordNotification(currentState state) error {
	ns.LastNotifyEpoch = time.Now().Unix()
	ns.CheckStates = make(map[string]int)

	for name, cs := range currentState.checks {
		ns.CheckStates[name] = int(cs.Status)
	}

	return ns.persist()
}

// persist writes the notification state to disk atomically using a temp file.
func (ns notifyState) persist() error {
	stateDir := filepath.Dir(ns.stateFile)
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			return err
		}
	}

	data, err := json.Marshal(ns)
	if err != nil {
		return err
	}

	tmpFile := ns.stateFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tmpFile, ns.stateFile)
}
