package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
