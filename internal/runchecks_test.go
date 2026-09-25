package internal

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// runChecksFixture has many fresh checks whose interval is not yet reached
// (skipped, reusing their state) mixed with checks that run /bin/true, so
// results are written while skipped checks are looked up.
func runChecksFixture(n int) (state, config) {
	now := time.Now().Unix()
	conf := config{CheckConcurrency: 4, CheckTimeoutS: 5, Checks: map[string]check{}}
	s := state{checks: map[string]checkState{}}
	for i := 0; i < n; i++ {
		skipped := fmt.Sprintf("skipped %d", i)
		conf.Checks[skipped] = check{Plugin: "/bin/false", RunInterval: 3600}
		s.checks[skipped] = checkState{Status: nagiosOk, PrevStatus: nagiosOk, Epoch: now, Output: "cached"}
		conf.Checks[fmt.Sprintf("run %d", i)] = check{Plugin: "/bin/true"}
	}
	return s, conf
}

// Run with -race: skipped-check lookups must not race with result writes.
func TestRunChecksSkippedReuseState(t *testing.T) {
	s, conf := runChecksFixture(50)
	result := runChecks(context.Background(), s, conf)

	for name, cs := range result.checks {
		if cs.Status != nagiosOk {
			t.Errorf("%s = %v (%s), want OK", name, cs.Status, cs.Output)
		}
		if name[:3] == "ski" && cs.Output != "cached" {
			t.Errorf("%s was re-run instead of reusing its state: %q", name, cs.Output)
		}
	}
	if len(result.checks) != 100 {
		t.Fatalf("got %d results, want 100", len(result.checks))
	}
}

// A check depending on a skipped check must not wait for it: the skipped
// check's reused status releases (or fails) the dependency.
func TestRunChecksDependsOnSkipped(t *testing.T) {
	now := time.Now().Unix()
	conf := config{CheckConcurrency: 2, CheckTimeoutS: 5, Checks: map[string]check{
		"cached ok":   {Plugin: "/bin/false", RunInterval: 3600},
		"cached crit": {Plugin: "/bin/true", RunInterval: 3600},
		"after ok":    {Plugin: "/bin/true", DependsOn: []string{"cached ok"}},
		"after crit":  {Plugin: "/bin/true", DependsOn: []string{"cached crit"}},
	}}
	s := state{checks: map[string]checkState{
		"cached ok":   {Status: nagiosOk, Epoch: now},
		"cached crit": {Status: nagiosCritical, Epoch: now},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := runChecks(ctx, s, conf)

	if cs := result.checks["after ok"]; cs.Status != nagiosOk {
		t.Errorf("after ok = %v (%s), want OK", cs.Status, cs.Output)
	}
	if cs := result.checks["after crit"]; cs.Status == nagiosOk || ctx.Err() != nil {
		t.Errorf("after crit = %v (%s), ctx err %v; want skipped as not OK without waiting for the timeout", cs.Status, cs.Output, ctx.Err())
	}
}
