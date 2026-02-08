package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type jsonReport struct {
	LastUpdated string       `json:"lastUpdated"`
	Subject     string       `json:"subject"`
	Summary     jsonSummary  `json:"summary"`
	Sections    jsonSections `json:"sections"`
}

type jsonSummary struct {
	Critical   int `json:"critical"`
	Warning    int `json:"warning"`
	Unknown    int `json:"unknown"`
	Stale      int `json:"stale"`
	Suppressed int `json:"suppressed"`
	Ok         int `json:"ok"`
}

type jsonSections struct {
	StatusChanged []jsonCheck `json:"statusChanged"`
	Unhandled     []jsonCheck `json:"unhandled"`
	Stale         []jsonCheck `json:"stale"`
	Suppressed    []jsonCheck `json:"suppressed"`
	Ok            []jsonCheck `json:"ok"`
}

type jsonCheck struct {
	Name                  string `json:"name"`
	Status                string `json:"status"`
	PrevStatus            string `json:"prevStatus,omitempty"`
	Output                string `json:"output"`
	FederatedFrom         string `json:"federatedFrom,omitempty"`
	Epoch                 int64  `json:"epoch"`
	LastCheckedAgeSeconds int64  `json:"lastCheckedAgeSeconds,omitempty"`
}

func persistJSONReport(state state, subject string, conf config) error {
	htmlFile := conf.HTMLStatusFile
	if htmlFile == "" {
		return nil
	}

	jsonFile := jsonReportPath(htmlFile)
	jsonDir := filepath.Dir(jsonFile)
	if err := os.MkdirAll(jsonDir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", jsonDir, err)
	}

	tmpFile := jsonFile + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer f.Close()

	report := state.jsonReport(subject, conf)
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("failed to write JSON: %w", err)
	}

	return os.Rename(tmpFile, jsonFile)
}

func jsonReportPath(htmlPath string) string {
	ext := filepath.Ext(htmlPath)
	if ext == "" {
		return htmlPath + ".json"
	}
	return strings.TrimSuffix(htmlPath, ext) + ".json"
}

func (s state) jsonReport(subject string, conf config) jsonReport {
	now := time.Now()

	summary := jsonSummary{
		Critical:   s.countBy(conf, func(cs checkState) bool { return cs.Status == nagiosCritical }),
		Warning:    s.countBy(conf, func(cs checkState) bool { return cs.Status == nagiosWarning }),
		Unknown:    s.countBy(conf, func(cs checkState) bool { return cs.Status == nagiosUnknown }),
		Stale:      s.countStale(conf),
		Suppressed: s.countSuppressed(conf),
		Ok:         s.countBy(conf, func(cs checkState) bool { return cs.Status == nagiosOk }),
	}

	sections := jsonSections{
		StatusChanged: s.jsonReportChanged(now, conf),
		Unhandled:     s.jsonReportUnhandled(now, conf),
		Stale:         s.jsonReportStale(now, conf),
		Suppressed:    s.jsonReportSuppressed(conf),
		Ok: s.jsonReportBy(now, false, false, conf, func(cs checkState) bool {
			return cs.Status == nagiosOk
		}),
	}

	return jsonReport{
		LastUpdated: now.Format(time.RFC3339),
		Subject:     subject,
		Summary:     summary,
		Sections:    sections,
	}
}

func (s state) jsonReportChanged(now time.Time, conf config) []jsonCheck {
	var checks []jsonCheck
	checks = append(checks, s.jsonReportBy(now, true, false, conf, func(cs checkState) bool {
		return cs.Status == nagiosCritical && cs.changed()
	})...)
	checks = append(checks, s.jsonReportBy(now, true, false, conf, func(cs checkState) bool {
		return cs.Status == nagiosWarning && cs.changed()
	})...)
	checks = append(checks, s.jsonReportBy(now, true, false, conf, func(cs checkState) bool {
		return cs.Status == nagiosUnknown && cs.changed()
	})...)
	checks = append(checks, s.jsonReportBy(now, true, false, conf, func(cs checkState) bool {
		return cs.Status == nagiosOk && cs.changed()
	})...)
	return checks
}

func (s state) jsonReportUnhandled(now time.Time, conf config) []jsonCheck {
	var checks []jsonCheck
	checks = append(checks, s.jsonReportBy(now, false, false, conf, func(cs checkState) bool {
		return cs.Status == nagiosCritical
	})...)
	checks = append(checks, s.jsonReportBy(now, false, false, conf, func(cs checkState) bool {
		return cs.Status == nagiosWarning
	})...)
	checks = append(checks, s.jsonReportBy(now, false, false, conf, func(cs checkState) bool {
		return cs.Status == nagiosUnknown
	})...)
	return checks
}

func (s state) jsonReportStale(now time.Time, conf config) []jsonCheck {
	return s.jsonReportBy(now, false, true, conf, func(cs checkState) bool {
		return cs.Epoch < s.staleEpoch && cs.Status != nagiosOk
	})
}

func (s state) jsonReportSuppressed(conf config) []jsonCheck {
	var checks []jsonCheck
	for name, cs := range s.checks {
		if cs.Status == nagiosOk || !isCheckSuppressed(name, conf) {
			continue
		}
		checks = append(checks, jsonCheck{
			Name:          name,
			Status:        nagiosCode(cs.Status).Str(),
			Output:        cs.Output,
			FederatedFrom: cs.FederatedFrom,
			Epoch:         cs.Epoch,
		})
	}
	return checks
}

func (s state) jsonReportBy(now time.Time, showStatusChange, isStaleReport bool, conf config,
	filter func(cs checkState) bool,
) []jsonCheck {
	var checks []jsonCheck
	for name, cs := range s.checks {
		if !filter(cs) {
			continue
		}
		if !isStaleReport && cs.Epoch < s.staleEpoch {
			continue
		}
		if cs.Status != nagiosOk && isCheckSuppressed(name, conf) {
			continue
		}

		entry := jsonCheck{
			Name:          name,
			Status:        nagiosCode(cs.Status).Str(),
			Output:        cs.Output,
			FederatedFrom: cs.FederatedFrom,
			Epoch:         cs.Epoch,
		}
		if showStatusChange && cs.changed() {
			entry.PrevStatus = nagiosCode(cs.PrevStatus).Str()
		}
		if isStaleReport {
			entry.LastCheckedAgeSeconds = int64(now.Sub(time.Unix(cs.Epoch, 0)).Seconds())
		}
		checks = append(checks, entry)
	}
	return checks
}
