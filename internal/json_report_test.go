package internal

import (
	"encoding/json"
	"os"
	"path/filepath"
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
