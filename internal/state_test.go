package internal

import (
	"testing"
	"time"
)

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
