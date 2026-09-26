package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A report written by the active node round-trips into the same check state
// on the passive node, including suppressed, stale and changed checks.
func TestChecksFromSectionsRoundTrip(t *testing.T) {
	now := time.Now().Unix()
	conf := config{PrometheusOnlyIfNotExists: t.TempDir(), PrometheusOnlyIfNotExistsMaxS: 3600}
	active := state{
		checks: map[string]checkState{
			"Changed":           {Status: nagiosCritical, PrevStatus: nagiosOk, Epoch: now, Output: "boom"},
			"Warn":              {Status: nagiosWarning, PrevStatus: nagiosWarning, Epoch: now, Output: "meh"},
			"Stale":             {Status: nagiosUnknown, PrevStatus: nagiosUnknown, Epoch: now - 7200, Output: "old"},
			"Prometheus: Muted": {Status: nagiosCritical, PrevStatus: nagiosCritical, Epoch: now, Output: "muted"},
			"Fine":              {Status: nagiosOk, PrevStatus: nagiosOk, Epoch: now, Output: "ok", FederatedFrom: "fed"},
			"Recovered":         {Status: nagiosOk, PrevStatus: nagiosWarning, Epoch: now, Output: "back"},
		},
		staleEpoch: now - 3600,
	}

	encoded, err := json.Marshal(active.jsonReport("subject", conf, true))
	if err != nil {
		t.Fatal(err)
	}
	var report peerReport
	if err := json.Unmarshal(encoded, &report); err != nil {
		t.Fatal(err)
	}

	got := checksFromSections(report.Sections)
	if len(got) != len(active.checks) {
		t.Fatalf("mirrored %d checks, want %d: %v", len(got), len(active.checks), got)
	}
	for name, want := range active.checks {
		if got[name] != want {
			t.Errorf("%s = %+v, want %+v", name, got[name], want)
		}
	}
}

func TestChecksFromSectionsEmpty(t *testing.T) {
	if got := checksFromSections(jsonSections{}); got != nil {
		t.Fatalf("want nil for a report without checks, got %v", got)
	}
}

func TestParseNagiosCodeUnknownInput(t *testing.T) {
	for _, in := range []string{"", "ok", "BOGUS"} {
		if got := parseNagiosCode(in); got != nagiosUnknown {
			t.Errorf("parseNagiosCode(%q) = %v, want UNKNOWN", in, got)
		}
	}
}

func TestMirrorPeerState(t *testing.T) {
	local := state{checks: map[string]checkState{"Frozen": {Status: nagiosCritical}}}

	kept := mirrorPeerState(local, peerSnapshot{})
	if _, ok := kept.checks["Frozen"]; !ok {
		t.Fatal("an empty peer snapshot must keep the local state")
	}

	mirrored := mirrorPeerState(local, peerSnapshot{Checks: map[string]checkState{"Live": {Status: nagiosOk}}})
	if _, ok := mirrored.checks["Frozen"]; ok || len(mirrored.checks) != 1 {
		t.Fatalf("mirrored state should hold only the peer's checks, got %v", mirrored.checks)
	}
}

// fetchPeerSnapshot decodes the sections of a live report.
func TestFetchPeerSnapshotChecks(t *testing.T) {
	report := state{checks: map[string]checkState{"A": {Status: nagiosWarning, PrevStatus: nagiosWarning, Epoch: time.Now().Unix()}}}.
		jsonReport("s", config{}, true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(report)
	}))
	defer server.Close()

	snap, err := fetchPeerSnapshot(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.ChecksActive || snap.Checks["A"].Status != nagiosWarning {
		t.Fatalf("unexpected snapshot %+v", snap)
	}
}

// fetchPeerSnapshot rejects bodies that are not exactly one valid report, so
// a torn, spliced or garbled peer report is never mirrored.
func TestFetchPeerSnapshotRejectsBadBodies(t *testing.T) {
	valid, err := json.MarshalIndent(state{checks: map[string]checkState{"A": {Status: nagiosOk, Epoch: time.Now().Unix()}}}.
		jsonReport("subject", config{}, true), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	half := len(valid) / 2
	// A torn read as seen in the wild: one version's bytes up to the middle
	// of a string, then another version's bytes from a later line on, which
	// leaves a raw newline inside the string.
	cut := strings.Index(string(valid), `"subj`) + len(`"subj`)
	rest := strings.Index(string(valid), "\n  \"summary\"")
	spliced := append(append([]byte{}, valid[:cut]...), valid[rest:]...)
	tests := []struct {
		name string
		body []byte
	}{
		{"trailing data after the report", append(append([]byte{}, valid...), `{"x":1}`...)},
		{"spliced report", spliced},
		{"truncated report", valid[:half]},
		{"raw control character in a string", []byte("{\"lastUpdated\":\"2026-09-26T00:00:00Z\",\"subject\":\"a\x01b\"}")},
		{"raw newline in a string", []byte("{\"lastUpdated\":\"2026-09-26T00:00:00Z\",\"subject\":\"a\nb\"}")},
		{"empty body", nil},
		{"oversized body", append(append([]byte{}, valid[:len(valid)-1]...),
			append([]byte(`,"pad":"`+strings.Repeat("x", maxPeerReportBytes)), `"}`...)...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(tt.body)
			}))
			defer server.Close()
			if snap, err := fetchPeerSnapshot(context.Background(), server.URL); err == nil {
				t.Fatalf("want an error, got snapshot %+v", snap)
			}
		})
	}
}

// A report of the maximum size is still accepted.
func TestDecodePeerReportAtLimit(t *testing.T) {
	prefix := `{"lastUpdated":"2026-09-26T00:00:00Z","subject":"`
	suffix := `"}`
	body := prefix + strings.Repeat("x", maxPeerReportBytes-len(prefix)-len(suffix)) + suffix
	report, err := decodePeerReport(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if report.LastUpdated != "2026-09-26T00:00:00Z" {
		t.Fatalf("lastUpdated = %q", report.LastUpdated)
	}
	if _, err := decodePeerReport(strings.NewReader(body + " ")); err == nil {
		t.Fatal("want an error one byte over the limit")
	}
}
