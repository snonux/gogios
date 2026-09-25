package internal

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// muteConf returns a config whose Prometheus checks and "Check HTTPS f3s"
// are muted while the returned marker file exists.
func muteConf(t *testing.T) (config, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "f3s_taken_down")
	return config{
		PrometheusOnlyIfNotExists:     marker,
		PrometheusOnlyIfNotExistsMaxS: 86400,
		Checks: map[string]check{
			"Check HTTPS f3s": {OnlyIfNotExists: marker},
			"Check Plain":     {},
		},
	}, marker
}

func setMarker(t *testing.T, marker string, present bool) {
	t.Helper()
	if !present {
		if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// run applies one run's results the way Run does: update, then markHidden.
func runOnce(s state, conf config, results map[string]nagiosCode) {
	for name, status := range results {
		s.update(checkResult{name: name, status: status, epoch: time.Now().Unix()})
	}
	s.markHidden(conf)
}

func TestMarkHidden(t *testing.T) {
	conf, marker := muteConf(t)
	setMarker(t, marker, true)
	s := state{checks: map[string]checkState{
		"Prometheus: Watchdog": {Status: nagiosCritical, PrevStatus: nagiosOk},
		"Prometheus: Fine":     {Status: nagiosOk},
		"Check Plain":          {Status: nagiosCritical},
		"Check HTTPS f3s":      {Status: nagiosWarning, PrevStatus: nagiosOk},
		"Prometheus: Old":      {Status: nagiosCritical, PrevStatus: nagiosCritical},
	}}
	s.markHidden(conf)

	want := map[string]bool{
		"Prometheus: Watchdog": true,
		"Prometheus: Fine":     false, // OK is never hidden
		"Check Plain":          false, // not muted
		"Check HTTPS f3s":      true,
		"Prometheus: Old":      false, // CRITICAL before the mute began
	}
	for name, hidden := range want {
		if s.checks[name].Hidden != hidden {
			t.Errorf("%s Hidden = %v, want %v", name, s.checks[name].Hidden, hidden)
		}
	}
}

// The Watchdog is CRITICAL all night while the cluster is off (muted), stays
// CRITICAL for a run after the unmute while Prometheus boots, then recovers:
// no mail for any of it. A real CRITICAL after that mails again.
func TestWatchdogOvernightMuteNoRecoveryMail(t *testing.T) {
	conf, marker := muteConf(t)
	s := state{checks: map[string]checkState{}}
	const wd = "Prometheus: Watchdog"

	runOnce(s, conf, map[string]nagiosCode{wd: nagiosOk})

	steps := []struct {
		desc   string
		marker bool
		status nagiosCode
		mail   bool
	}{
		{"cluster shut down (muted)", true, nagiosCritical, false},
		{"still down overnight", true, nagiosCritical, false},
		{"unmuted, Prometheus still booting", false, nagiosCritical, false},
		{"Prometheus back", false, nagiosOk, false},
		{"real outage later", false, nagiosCritical, true},
		{"real recovery", false, nagiosOk, true},
	}
	for _, step := range steps {
		setMarker(t, marker, step.marker)
		runOnce(s, conf, map[string]nagiosCode{wd: step.status})
		if got := s.hasCriticalChange(conf); got != step.mail {
			t.Errorf("%s: hasCriticalChange = %v, want %v (%+v)", step.desc, got, step.mail, s.checks[wd])
		}
	}
}

// A CRITICAL that was mailed before the mute began is not hidden by it: its
// recovery mails, whether it comes after the unmute or during the mute.
func TestPreMuteCriticalRecoveryMails(t *testing.T) {
	const wd = "Prometheus: Watchdog"
	for _, recoverMuted := range []bool{false, true} {
		conf, marker := muteConf(t)
		s := state{checks: map[string]checkState{}}
		runOnce(s, conf, map[string]nagiosCode{wd: nagiosOk})
		runOnce(s, conf, map[string]nagiosCode{wd: nagiosCritical})
		if !s.hasCriticalChange(conf) {
			t.Fatal("OK -> CRITICAL before the mute must mail")
		}
		setMarker(t, marker, true)
		runOnce(s, conf, map[string]nagiosCode{wd: nagiosCritical})
		if s.checks[wd].Hidden {
			t.Fatalf("a CRITICAL older than the mute must not become hidden: %+v", s.checks[wd])
		}
		if s.hasCriticalChange(conf) {
			t.Fatal("an unchanged CRITICAL must not mail")
		}
		setMarker(t, marker, recoverMuted)
		runOnce(s, conf, map[string]nagiosCode{wd: nagiosOk})
		if !s.hasCriticalChange(conf) {
			t.Errorf("recovery (muted=%v) of a mailed CRITICAL must mail: %+v", recoverMuted, s.checks[wd])
		}
	}
}

// Entering CRITICAL from a hidden non-critical status is news and mails.
func TestHiddenWarningToCriticalMails(t *testing.T) {
	conf, marker := muteConf(t)
	s := state{checks: map[string]checkState{}}
	setMarker(t, marker, true)
	runOnce(s, conf, map[string]nagiosCode{"Check HTTPS f3s": nagiosWarning})
	setMarker(t, marker, false)
	runOnce(s, conf, map[string]nagiosCode{"Check HTTPS f3s": nagiosCritical})
	if !s.hasCriticalChange(conf) {
		t.Fatalf("hidden WARNING -> CRITICAL must mail: %+v", s.checks["Check HTTPS f3s"])
	}
}

// A check that was not hidden still mails its recovery.
func TestUnhiddenRecoveryMails(t *testing.T) {
	conf, _ := muteConf(t)
	s := state{checks: map[string]checkState{}}
	runOnce(s, conf, map[string]nagiosCode{"Check Plain": nagiosCritical})
	runOnce(s, conf, map[string]nagiosCode{"Check Plain": nagiosOk})
	if !s.hasCriticalChange(conf) {
		t.Fatal("CRITICAL -> OK of a visible check must mail")
	}
}

// Hidden is cleared once the status changes after the unmute.
func TestHiddenClearedOnChange(t *testing.T) {
	conf, marker := muteConf(t)
	s := state{checks: map[string]checkState{}}
	setMarker(t, marker, true)
	runOnce(s, conf, map[string]nagiosCode{"Check HTTPS f3s": nagiosCritical})
	setMarker(t, marker, false)
	runOnce(s, conf, map[string]nagiosCode{"Check HTTPS f3s": nagiosWarning})
	if cs := s.checks["Check HTTPS f3s"]; cs.Hidden || !cs.PrevHidden {
		t.Fatalf("after leaving the hidden status: %+v, want Hidden false, PrevHidden true", cs)
	}
}

func TestNotifyTrigger(t *testing.T) {
	now := time.Now().Unix()
	unhandled := state{staleEpoch: now - 3600, checks: map[string]checkState{"A": {Status: nagiosWarning, PrevStatus: nagiosWarning, Epoch: now}}}
	staleOnly := state{staleEpoch: now - 3600, checks: map[string]checkState{"A": {Status: nagiosWarning, PrevStatus: nagiosWarning, Epoch: now - 7200}}}
	allOk := state{checks: map[string]checkState{"A": {Status: nagiosOk, PrevStatus: nagiosOk, Epoch: now}}}
	tests := []struct {
		name            string
		s               state
		renotify, force bool
		want            bool
	}{
		{"renotify with unhandled", unhandled, true, false, true},
		{"unhandled without renotify", unhandled, false, false, false},
		{"renotify with stale only", staleOnly, true, false, false},
		{"renotify all OK", allOk, true, false, false},
		{"force all OK", allOk, false, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.notifyTrigger(tt.renotify, tt.force, config{}); got != tt.want {
				t.Errorf("notifyTrigger = %v, want %v", got, tt.want)
			}
		})
	}
}

// state.json round-trips the Host and Hidden fields.
func TestStatePersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	conf := config{StateDir: dir, StaleThreshold: 3600, Checks: map[string]check{"A": {Local: true}}}
	s := state{stateFile: filepath.Join(dir, "state.json"), checks: map[string]checkState{
		"A": {Status: nagiosCritical, PrevStatus: nagiosOk, Host: "h", Hidden: true, PrevHidden: true},
	}}
	if err := s.persist(); err != nil {
		t.Fatal(err)
	}
	loaded, err := newState(conf)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.checks["A"] != s.checks["A"] {
		t.Fatalf("loaded %+v, want %+v", loaded.checks["A"], s.checks["A"])
	}
}
