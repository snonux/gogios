package internal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPersistJSONReport(t *testing.T) {
	tmpDir := t.TempDir()
	htmlFile := filepath.Join(tmpDir, "status.html")
	conf := config{HTMLStatusFile: htmlFile}

	now := time.Now().Unix()
	s := state{
		checks: map[string]checkState{
			"CriticalCheck": {
				Status:     nagiosCritical,
				PrevStatus: nagiosOk,
				Epoch:      now,
				Output:     "boom",
			},
			"StaleCheck": {
				Status:     nagiosWarning,
				PrevStatus: nagiosWarning,
				Epoch:      now - 1000,
				Output:     "stale warning",
			},
			"OkCheck": {
				Status:     nagiosOk,
				PrevStatus: nagiosOk,
				Epoch:      now,
				Output:     "all good",
			},
		},
		staleEpoch: now - 100,
	}

	subject := "GOGIOS Report [C:1 W:1 U:0 S:1 SU:0 OK:1]"
	if err := persistJSONReport(s, subject, conf); err != nil {
		t.Fatalf("persistJSONReport() error = %v", err)
	}

	jsonFile := filepath.Join(tmpDir, "status.json")
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		t.Fatalf("failed to read JSON file: %v", err)
	}

	var report jsonReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("failed to unmarshal JSON report: %v", err)
	}

	if report.LastUpdated == "" {
		t.Fatal("lastUpdated is empty")
	}
	if _, err := time.Parse(time.RFC3339, report.LastUpdated); err != nil {
		t.Fatalf("lastUpdated is not RFC3339: %v", err)
	}
	if report.Subject != subject {
		t.Fatalf("subject = %q, want %q", report.Subject, subject)
	}

	if report.Summary.Critical != 1 || report.Summary.Warning != 1 || report.Summary.Stale != 1 || report.Summary.Ok != 1 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}

	if len(report.Sections.StatusChanged) != 1 || report.Sections.StatusChanged[0].Name != "CriticalCheck" {
		t.Fatalf("unexpected statusChanged: %+v", report.Sections.StatusChanged)
	}
	if len(report.Sections.Unhandled) != 1 || report.Sections.Unhandled[0].Name != "CriticalCheck" {
		t.Fatalf("unexpected unhandled: %+v", report.Sections.Unhandled)
	}
	if len(report.Sections.Stale) != 1 || report.Sections.Stale[0].Name != "StaleCheck" {
		t.Fatalf("unexpected stale: %+v", report.Sections.Stale)
	}
	if len(report.Sections.Ok) != 1 || report.Sections.Ok[0].Name != "OkCheck" {
		t.Fatalf("unexpected ok: %+v", report.Sections.Ok)
	}
	if len(report.Sections.Suppressed) != 0 {
		t.Fatalf("unexpected suppressed: %+v", report.Sections.Suppressed)
	}
}

// TestJSONReportKeepsStaleOkChecks guards the parity bug where jsonReportBy
// dropped stale OK checks from sections.ok while countBy still counted them in
// summary.ok, leaving the blob internally inconsistent. htmlReportBy keeps them
// (see html.go), so the JSON must too.
func TestJSONReportKeepsStaleOkChecks(t *testing.T) {
	now := time.Now().Unix()
	s := state{
		checks: map[string]checkState{
			"FreshOkCheck": {Status: nagiosOk, PrevStatus: nagiosOk, Epoch: now, Output: "all good"},
			"StaleOkCheck": {Status: nagiosOk, PrevStatus: nagiosOk, Epoch: now - 1000, Output: "old but fine"},
		},
		staleEpoch: now - 100,
	}

	report := s.jsonReport("subject", config{})

	if report.Summary.Ok != 2 {
		t.Fatalf("summary.ok = %d, want 2", report.Summary.Ok)
	}
	if len(report.Sections.Ok) != report.Summary.Ok {
		t.Fatalf("sections.ok has %d entries but summary.ok says %d: %+v",
			len(report.Sections.Ok), report.Summary.Ok, report.Sections.Ok)
	}
	// A stale OK check is not a stale alert; only non-OK checks count as stale.
	if report.Summary.Stale != 0 {
		t.Fatalf("summary.stale = %d, want 0", report.Summary.Stale)
	}
}

// TestJSONReportEmptySectionsMarshalAsArrays guards against empty sections
// serializing as null, which would force a null guard on every browser client.
func TestJSONReportEmptySectionsMarshalAsArrays(t *testing.T) {
	s := state{checks: map[string]checkState{}, staleEpoch: time.Now().Unix()}

	data, err := json.Marshal(s.jsonReport("subject", config{}))
	if err != nil {
		t.Fatalf("failed to marshal report: %v", err)
	}

	for _, section := range []string{"statusChanged", "unhandled", "stale", "suppressed", "ok"} {
		want := `"` + section + `":[]`
		if !strings.Contains(string(data), want) {
			t.Fatalf("section %q did not marshal as an empty array; got %s", section, data)
		}
	}
}
