package internal

import (
	"testing"
	"time"
)

func TestHasCriticalChange_OkToWarning(t *testing.T) {
	// OK -> WARNING must not trigger an immediate notification.
	s := state{checks: map[string]checkState{
		"Check A": {Status: nagiosWarning, PrevStatus: nagiosOk},
	}}
	if s.hasCriticalChange(config{}) {
		t.Error("expected hasCriticalChange=false for OK->WARNING")
	}
}

func TestHasCriticalChange_WarningToOk(t *testing.T) {
	// WARNING -> OK must not trigger an immediate notification either.
	s := state{checks: map[string]checkState{
		"Check A": {Status: nagiosOk, PrevStatus: nagiosWarning},
	}}
	if s.hasCriticalChange(config{}) {
		t.Error("expected hasCriticalChange=false for WARNING->OK")
	}
}

func TestHasCriticalChange_ToCritical(t *testing.T) {
	s := state{checks: map[string]checkState{
		"Check A": {Status: nagiosCritical, PrevStatus: nagiosWarning},
	}}
	if !s.hasCriticalChange(config{}) {
		t.Error("expected hasCriticalChange=true when a check becomes CRITICAL")
	}
}

func TestHasCriticalChange_RecoveredFromCritical(t *testing.T) {
	s := state{checks: map[string]checkState{
		"Check A": {Status: nagiosOk, PrevStatus: nagiosCritical},
	}}
	if !s.hasCriticalChange(config{}) {
		t.Error("expected hasCriticalChange=true when a check recovers from CRITICAL")
	}
}

func TestHasCriticalChange_NoChange(t *testing.T) {
	s := state{checks: map[string]checkState{
		"Check A": {Status: nagiosCritical, PrevStatus: nagiosCritical},
	}}
	if s.hasCriticalChange(config{}) {
		t.Error("expected hasCriticalChange=false when status did not change")
	}
}

func TestAge(t *testing.T) {
	state := state{checks: make(map[string]checkState)}

	state.checks["Check Foo"] = checkState{Epoch: 0}
	minAge := time.Duration(time.Now().Unix())

	if reportedAge := state.age("Check Foo"); reportedAge < minAge {
		t.Errorf("expected age >= %v, got %v", minAge, reportedAge)
	}

	maxAge := time.Duration(time.Now().Unix())
	state.checks["Check Bar"] = checkState{Epoch: time.Now().Unix()}

	if reportedAge := state.age("Check Bar"); reportedAge >= minAge {
		t.Errorf("expected age < %v, got %v", maxAge, reportedAge)
	}
}
