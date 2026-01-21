package internal

import (
	"os"
	"testing"
	"time"
)

func TestIsSuppressed_EmptyPath(t *testing.T) {
	// Empty file path should not suppress
	if isSuppressed("", 86400) {
		t.Error("Expected empty path to not suppress")
	}
}

func TestIsSuppressed_NonExistentFile(t *testing.T) {
	// Non-existent file should not suppress
	if isSuppressed("/nonexistent/path/to/file", 86400) {
		t.Error("Expected non-existent file to not suppress")
	}
}

func TestIsSuppressed_RecentFile(t *testing.T) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "suppress_test")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Recent file should suppress
	if !isSuppressed(tmpFile.Name(), 86400) {
		t.Error("Expected recent file to suppress")
	}
}

func TestIsSuppressed_OldFile(t *testing.T) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "suppress_test")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Set the file's modification time to 2 hours ago
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(tmpFile.Name(), oldTime, oldTime); err != nil {
		t.Fatalf("Failed to change file time: %v", err)
	}

	// File older than maxAgeS (1 hour = 3600s) should not suppress
	if isSuppressed(tmpFile.Name(), 3600) {
		t.Error("Expected old file to not suppress")
	}

	// File within maxAgeS (3 hours = 10800s) should suppress
	if !isSuppressed(tmpFile.Name(), 10800) {
		t.Error("Expected file within max age to suppress")
	}
}

func TestIsCheckSuppressed_PrometheusCheck(t *testing.T) {
	// Create a temporary file for Prometheus suppression
	tmpFile, err := os.CreateTemp("", "prometheus_suppress_test")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	conf := config{
		PrometheusOnlyIfNotExists:     tmpFile.Name(),
		PrometheusOnlyIfNotExistsMaxS: 86400,
		Checks:                        make(map[string]check),
	}

	// Prometheus check should be suppressed when file exists
	if !isCheckSuppressed("Prometheus: TestAlert", conf) {
		t.Error("Expected Prometheus check to be suppressed")
	}

	// Non-Prometheus check should not be affected by Prometheus suppression
	conf.Checks["Regular Check"] = check{}
	if isCheckSuppressed("Regular Check", conf) {
		t.Error("Expected regular check to not be suppressed by Prometheus config")
	}
}

func TestIsCheckSuppressed_RegularCheck(t *testing.T) {
	// Create a temporary file for check suppression
	tmpFile, err := os.CreateTemp("", "check_suppress_test")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	conf := config{
		PrometheusOnlyIfNotExistsMaxS: 86400,
		Checks: map[string]check{
			"Suppressed Check": {
				OnlyIfNotExists:     tmpFile.Name(),
				OnlyIfNotExistsMaxS: 86400,
			},
			"Normal Check": {},
		},
	}

	// Check with suppression file should be suppressed
	if !isCheckSuppressed("Suppressed Check", conf) {
		t.Error("Expected check with suppression file to be suppressed")
	}

	// Check without suppression file should not be suppressed
	if isCheckSuppressed("Normal Check", conf) {
		t.Error("Expected check without suppression file to not be suppressed")
	}

	// Unknown check (not in config) should not be suppressed
	if isCheckSuppressed("Unknown Check", conf) {
		t.Error("Expected unknown check to not be suppressed")
	}
}

func TestIsCheckSuppressed_UsesGlobalDefaultMaxAge(t *testing.T) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "suppress_test")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Set the file's modification time to 2 hours ago
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(tmpFile.Name(), oldTime, oldTime); err != nil {
		t.Fatalf("Failed to change file time: %v", err)
	}

	// Config with short global max age (1 hour)
	conf := config{
		PrometheusOnlyIfNotExistsMaxS: 3600,
		Checks: map[string]check{
			"Test Check": {
				OnlyIfNotExists:     tmpFile.Name(),
				OnlyIfNotExistsMaxS: 0, // Use global default
			},
		},
	}

	// Should NOT be suppressed because file is older than global default (1 hour)
	if isCheckSuppressed("Test Check", conf) {
		t.Error("Expected check to not be suppressed when file is older than global max age")
	}

	// Config with longer global max age (3 hours)
	conf.PrometheusOnlyIfNotExistsMaxS = 10800
	if !isCheckSuppressed("Test Check", conf) {
		t.Error("Expected check to be suppressed when file is within global max age")
	}
}
