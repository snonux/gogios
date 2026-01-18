package internal

import (
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// persistHTMLReport generates and persists the HTML status page.
// Mirrors persistReport() pattern from run.go with atomic write.
func persistHTMLReport(state state, subject string, conf config) error {
	htmlFile := conf.HTMLStatusFile
	if htmlFile == "" {
		log.Println("debug: HTMLStatusFile is empty, skipping HTML report generation")
		return nil
	}
	
	log.Println("debug: HTMLStatusFile set to", htmlFile)
	htmlDir := filepath.Dir(htmlFile)

	// Auto-create directory if it doesn't exist
	// CLAUDE: Only create it when it doesnt exist yet
	if err := os.MkdirAll(htmlDir, 0o755); err != nil {
		log.Println("debug: error creating directory:", err)
		return fmt.Errorf("failed to create directory %s: %w", htmlDir, err)
	}
	log.Println("debug: directory ensured at", htmlDir)

	tmpFile := htmlFile + ".tmp"
	log.Println("debug: writing to temp file", tmpFile)

	f, err := os.Create(tmpFile)
	if err != nil {
		log.Println("debug: error creating temp file:", err)
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer f.Close()

	htmlContent := state.htmlReport(subject)
	if _, err = f.WriteString(htmlContent); err != nil {
		log.Println("debug: error writing HTML:", err)
		return fmt.Errorf("failed to write HTML: %w", err)
	}
	log.Println("debug: successfully wrote HTML to temp file")

	err = os.Rename(tmpFile, htmlFile)
	if err != nil {
		log.Println("debug: error renaming temp file to final location:", err)
		return err
	}
	log.Println("debug: successfully renamed and persisted HTML report to", htmlFile)
	return nil
}

// htmlReport generates the complete HTML status page.
// Mirrors state.report() pattern from state.go:133-163.
func (s state) htmlReport(subject string) string {
	var sb strings.Builder

	// Calculate counts for header summary (without generating HTML yet)
	numCriticals := s.countBy(func(cs checkState) bool {
		return cs.Status == nagiosCritical
	})
	numWarnings := s.countBy(func(cs checkState) bool {
		return cs.Status == nagiosWarning
	})
	numUnknown := s.countBy(func(cs checkState) bool {
		return cs.Status == nagiosUnknown
	})
	numOK := s.countBy(func(cs checkState) bool {
		return cs.Status == nagiosOk
	})
	numStale := s.countStale()

	// Write HTML header with summary
	sb.WriteString(htmlHeader(subject, numCriticals, numWarnings, numUnknown, numStale, numOK))

	// Alerts with status changed section
	sb.WriteString(`<div class="section">` + "\n")
	sb.WriteString(`<h2>Alerts with status changed</h2>` + "\n")
	changed := s.htmlReportChanged(&sb)
	if !changed {
		sb.WriteString(`<p>There were no status changes...</p>` + "\n")
	}
	sb.WriteString(`</div>` + "\n\n")

	// Unhandled alerts section
	sb.WriteString(`<div class="section">` + "\n")
	sb.WriteString(`<h2>Unhandled alerts</h2>` + "\n")
	hasUnhandled := (numCriticals + numWarnings + numUnknown) > 0
	if hasUnhandled {
		s.htmlReportUnhandledContent(&sb)
	} else {
		sb.WriteString(`<p>There are no unhandled alerts...</p>` + "\n")
	}
	sb.WriteString(`</div>` + "\n\n")

	// Stale alerts section
	sb.WriteString(`<div class="section">` + "\n")
	sb.WriteString(`<h2>Stale alerts</h2>` + "\n")
	if numStale == 0 {
		sb.WriteString(`<p>There are no stale alerts...</p>` + "\n")
	} else {
		s.htmlReportStaleAlerts(&sb)
	}
	sb.WriteString(`</div>` + "\n\n")

	// OK checks section
	sb.WriteString(`<div class="section">` + "\n")
	sb.WriteString(`<h2>OK checks</h2>` + "\n")
	if numOK == 0 {
		sb.WriteString(`<p>There are no OK checks...</p>` + "\n")
	} else {
		s.htmlReportBy(&sb, false, false, func(cs checkState) bool {
			return cs.Status == nagiosOk
		})
	}
	sb.WriteString(`</div>` + "\n\n")

	sb.WriteString(htmlFooter())

	return sb.String()
}

// htmlReportChanged generates HTML for checks with status changes.
// Mirrors state.reportChanged() from state.go:166-192.
func (s state) htmlReportChanged(sb *strings.Builder) (changed bool) {
	if 0 < s.htmlReportBy(sb, true, false, func(cs checkState) bool {
		return cs.Status == nagiosCritical && cs.changed()
	}) {
		changed = true
	}

	if 0 < s.htmlReportBy(sb, true, false, func(cs checkState) bool {
		return cs.Status == nagiosWarning && cs.changed()
	}) {
		changed = true
	}

	if 0 < s.htmlReportBy(sb, true, false, func(cs checkState) bool {
		return cs.Status == nagiosUnknown && cs.changed()
	}) {
		changed = true
	}

	if 0 < s.htmlReportBy(sb, true, false, func(cs checkState) bool {
		return cs.Status == nagiosOk && cs.changed()
	}) {
		changed = true
	}

	return
}

// htmlReportUnhandledContent generates HTML content for unhandled alerts section.
// Mirrors state.reportUnhandled() from state.go:194-214.
func (s state) htmlReportUnhandledContent(sb *strings.Builder) {
	s.htmlReportBy(sb, false, false, func(cs checkState) bool {
		return cs.Status == nagiosCritical
	})

	s.htmlReportBy(sb, false, false, func(cs checkState) bool {
		return cs.Status == nagiosWarning
	})

	s.htmlReportBy(sb, false, false, func(cs checkState) bool {
		return cs.Status == nagiosUnknown
	})
}

// htmlReportStaleAlerts generates HTML for stale checks.
// Only reports stale alerts that are not OK, since stale OK alerts aren't concerning.
// Mirrors state.reportStaleAlerts() from state.go.
func (s state) htmlReportStaleAlerts(sb *strings.Builder) int {
	return s.htmlReportBy(sb, false, true, func(cs checkState) bool {
		return cs.Epoch < s.staleEpoch && cs.Status != nagiosOk
	})
}

// htmlReportBy is the generic HTML generator for check items.
// Mirrors state.reportBy() from state.go:222-262 but outputs HTML.
func (s state) htmlReportBy(sb *strings.Builder, showStatusChange, isStaleReport bool,
	filter func(cs checkState) bool,
) (count int) {
	for name, cs := range s.checks {
		if !filter(cs) {
			continue
		}
		if !isStaleReport && cs.Epoch < s.staleEpoch {
			continue // skip stale checks in non-stale report
		}
		count++

		sb.WriteString(`<div class="check-item">` + "\n")

		// Show status change if applicable
		if showStatusChange && cs.changed() {
			sb.WriteString(htmlStatusBadge(nagiosCode(cs.PrevStatus)))
			sb.WriteString(` <span class="arrow">→</span> `)
		}

		// Show current status
		sb.WriteString(htmlStatusBadge(nagiosCode(cs.Status)))
		sb.WriteString(": ")
		sb.WriteString(html.EscapeString(name))
		sb.WriteString(": ")
		sb.WriteString(html.EscapeString(cs.Output))

		// Show federated source if applicable
		if cs.federated() {
			sb.WriteString(" [federated from ")
			sb.WriteString(html.EscapeString(cs.FederatedFrom))
			sb.WriteString("]")
		}

		// Show stale duration if applicable
		if isStaleReport {
			lastCheckedAgo := time.Since(time.Unix(cs.Epoch, 0))
			sb.WriteString(fmt.Sprintf(" (last checked %v ago)", lastCheckedAgo))
		}

		sb.WriteString("\n</div>\n")
	}

	return
}

// countStale counts the number of stale checks (excluding OK status).
// Helper function for generating summary counts.
func (s state) countStale() int {
	return s.countBy(func(cs checkState) bool {
		return cs.Epoch < s.staleEpoch && cs.Status != nagiosOk
	})
}

// htmlHeader generates the HTML document header with embedded CSS and status summary.
func htmlHeader(subject string, numCriticals, numWarnings, numUnknown, numStale, numOK int) string {
	var sb strings.Builder

	sb.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta http-equiv="refresh" content="300">
    <title>`)
	sb.WriteString(html.EscapeString(subject))
	sb.WriteString(`</title>
    <style>
        body {
            font-family: sans-serif;
            text-align: center;
            padding-top: 50px;
        }
        .container {
            max-width: 800px;
            margin: 0 auto;
        }
        .summary {
            margin: 20px 0;
            font-weight: bold;
        }
        .section {
            margin: 30px 0;
            text-align: left;
        }
        .check-item {
            margin: 10px 0;
            padding: 5px;
        }
        .CRITICAL { color: #dc3545; }
        .WARNING { color: #ff8c00; }
        .UNKNOWN { color: #6c757d; }
        .OK { color: #28a745; }
        .footer {
            margin-top: 40px;
            font-size: 0.9em;
            color: #666;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>Gogios Status Report</h1>
        <div class="summary">C:`)
	sb.WriteString(fmt.Sprintf("%d W:%d U:%d S:%d OK:%d", numCriticals, numWarnings, numUnknown, numStale, numOK))
	sb.WriteString(`</div>
        <p>Last Updated: `)
	sb.WriteString(time.Now().Format("2006-01-02 15:04:05 MST"))
	sb.WriteString(`</p>

`)

	return sb.String()
}

// htmlFooter generates the HTML document footer.
func htmlFooter() string {
	var sb strings.Builder

	sb.WriteString(`        <div class="footer">
            Generated by Gogios at `)
	sb.WriteString(time.Now().Format("2006-01-02 15:04:05 MST"))
	sb.WriteString(`
        </div>
    </div>
</body>
</html>
`)

	return sb.String()
}

// htmlStatusBadge generates a colored HTML span for a status code.
func htmlStatusBadge(status nagiosCode) string {
	statusStr := status.Str()
	return fmt.Sprintf(`<span class="%s">%s</span>`, statusStr, statusStr)
}
