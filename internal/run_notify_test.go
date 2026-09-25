package internal

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// notifyAndPublish mails (here: records the notification, SMTP disabled)
// only for checks the node owns, and publishes the full state either way.
func TestNotifyAndPublishOwnership(t *testing.T) {
	now := time.Now().Unix()
	peerChange := checkState{Status: nagiosCritical, PrevStatus: nagiosOk, Epoch: now}
	tests := []struct {
		name     string
		passive  bool
		checks   map[string]checkState
		wantMail bool
	}{
		{"passive: own local CRITICAL mails", true, map[string]checkState{
			"Check Disk passive": {Status: nagiosCritical, PrevStatus: nagiosOk, Epoch: now, Host: passiveHost},
		}, true},
		{"passive: mirrored CRITICAL does not mail", true, map[string]checkState{
			"Shared": peerChange,
		}, false},
		{"active: peer's local CRITICAL does not mail", false, map[string]checkState{
			"Check Disk passive": {Status: nagiosCritical, PrevStatus: nagiosOk, Epoch: now, Host: passiveHost},
		}, false},
		{"active: own local CRITICAL mails", false, map[string]checkState{
			"Check Disk active": {Status: nagiosCritical, PrevStatus: nagiosOk, Epoch: now, Host: activeHost},
		}, true},
		{"active: shared CRITICAL mails", false, map[string]checkState{
			"Shared": peerChange,
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			hostname := activeHost
			if tt.passive {
				hostname = passiveHost
			}
			conf := config{
				hostname:       hostname,
				SMTPDisable:    true,
				StateDir:       dir,
				HTMLStatusFile: filepath.Join(dir, "index.html"),
			}
			s := state{stateFile: filepath.Join(dir, "state.json"), staleEpoch: now - 3600, checks: tt.checks}
			ns, err := newNotifyState(dir)
			if err != nil {
				t.Fatal(err)
			}

			notifyAndPublish(s, conf, peerDecision{Active: !tt.passive}, ns, false, false)

			_, err = os.Stat(filepath.Join(dir, "notify_state.json"))
			if mailed := err == nil; mailed != tt.wantMail {
				t.Errorf("mailed = %v, want %v", mailed, tt.wantMail)
			}
			for _, report := range []string{"report.txt", "index.html", "index.json"} {
				if _, err := os.Stat(filepath.Join(dir, report)); err != nil {
					t.Errorf("%s not published: %v", report, err)
				}
			}
		})
	}
}
