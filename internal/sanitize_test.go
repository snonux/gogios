package internal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeOutput(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain text unchanged", "OK - all good", "OK - all good"},
		{"empty", "", ""},
		{"multibyte UTF-8 kept", "Temp 42°C → ok ü", "Temp 42°C → ok ü"},
		{"tab and newline kept", "a\tb\nc", "a\tb\nc"},
		{"SOH replaced", "a\x01b", "a\uFFFDb"},
		{"NUL replaced", "a\x00b", "a\uFFFDb"},
		{"DEL replaced", "a\x7fb", "a\uFFFDb"},
		{"C1 NEL replaced", "a\u0085b", "a\uFFFDb"},
		{"CRLF becomes LF", "a\r\nb", "a\nb"},
		{"lone CR becomes LF", "a\rb", "a\nb"},
		{"trailing CR becomes LF", "a\r", "a\n"},
		{"ANSI colour dropped", "\x1b[31mCRITICAL\x1b[0m done", "CRITICAL done"},
		{"ANSI with params dropped", "x\x1b[1;32;40my", "xy"},
		{"bare ESC replaced", "a\x1bb", "a\uFFFDb"},
		{"ESC at end replaced", "a\x1b", "a\uFFFD"},
		{"unterminated CSI at end dropped", "a\x1b[12", "a"},
		{"unterminated CSI keeps following text", "a\x1b[1\x01b", "a\uFFFDb"},
		{"invalid UTF-8 replaced", "a\xff\xfeb", "a\uFFFDb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeOutput(tt.in)
			if got != tt.want {
				t.Fatalf("sanitizeOutput(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if !isCleanOutput(got) {
				t.Fatalf("sanitizeOutput(%q) = %q is not clean", tt.in, got)
			}
			if again := sanitizeOutput(got); again != got {
				t.Fatalf("not idempotent: %q -> %q", got, again)
			}
		})
	}
}

// A plugin printing control characters, terminal escapes and invalid UTF-8
// must still produce a strictly valid JSON report (the index.json a peer and
// browsers fetch) and an HTML page and text report without raw controls.
func TestControlCharPluginOutputReports(t *testing.T) {
	dir := t.TempDir()
	plugin := filepath.Join(dir, "ctrl.sh")
	script := "#!/bin/sh\nprintf 'CRIT \\001 \\033[31mred\\033[0m\\ttab\\rcr\\nline2 \\377 \\000nul'\nexit 2\n"
	if err := os.WriteFile(plugin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	s := state{checks: map[string]checkState{}}
	s.update(check{Plugin: plugin}.run(context.Background(), "Ctrl"))
	conf := config{HTMLStatusFile: filepath.Join(dir, "index.html"), StateDir: dir}
	if err := persistJSONReport(s, "subject", conf, true); err != nil {
		t.Fatal(err)
	}
	if err := persistHTMLReport(s, "subject", conf); err != nil {
		t.Fatal(err)
	}
	subject, body := s.report("", conf, false, "")
	if err := persistReport(subject, body, conf); err != nil {
		t.Fatal(err)
	}

	data := readFile(t, filepath.Join(dir, "index.json"))
	var report jsonReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("index.json is not valid JSON: %v", err)
	}
	if len(report.Sections.Unhandled) != 1 {
		t.Fatalf("want the check in unhandled, got %+v", report.Sections)
	}
	want := "CRIT \uFFFD red\ttab\ncr\nline2 \uFFFD \uFFFDnul"
	if got := report.Sections.Unhandled[0].Output; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	for _, name := range []string{"index.json", "index.html", "report.txt"} {
		assertNoRawControls(t, name, readFile(t, filepath.Join(dir, name)))
	}
}

// Output that entered the state before sanitising (an old state.json, an
// older peer or a federated instance) is still escaped by encoding/json, so
// the JSON report stays valid for any content.
func TestJSONReportEscapesUnsanitisedOutput(t *testing.T) {
	dir := t.TempDir()
	conf := config{HTMLStatusFile: filepath.Join(dir, "index.html")}
	s := state{checks: map[string]checkState{
		"Raw": {Status: nagiosCritical, Output: "raw \x01\x1b[0m\t\r\n\x00 \xff end"},
	}}
	if err := persistJSONReport(s, "subject", conf, true); err != nil {
		t.Fatal(err)
	}
	data := readFile(t, filepath.Join(dir, "index.json"))
	if !json.Valid(data) {
		t.Fatalf("index.json is not valid JSON:\n%s", data)
	}
	assertNoRawControls(t, "index.json", data)
}

// Output from a federated instance or the peer is sanitised as it is merged.
func TestMergedOutputIsSanitised(t *testing.T) {
	s := state{checks: map[string]checkState{}}
	if err := s.mergeFromBytes([]byte(`{"Fed":{"Status":2,"Output":"a\u0001b\u001b[1mc"}}`)); err != nil {
		t.Fatal(err)
	}
	if got := s.checks["Fed"].Output; got != "a\uFFFDbc" {
		t.Fatalf("federated output = %q", got)
	}

	checks := checksFromSections(jsonSections{Ok: []jsonCheck{{Name: "Peer", Status: "OK", Output: "x\x07y"}}})
	if got := checks["Peer"].Output; got != "x\uFFFDy" {
		t.Fatalf("peer output = %q", got)
	}
}

// assertNoRawControls fails when data holds a control character other than
// newline and tab, or invalid UTF-8.
func assertNoRawControls(t *testing.T, name string, data []byte) {
	t.Helper()
	if !utf8.Valid(data) {
		t.Errorf("%s is not valid UTF-8", name)
	}
	for i, r := range string(data) {
		if r != '\n' && r != '\t' && isControl(r) {
			line := strings.Count(string(data[:i]), "\n") + 1
			t.Errorf("%s: raw control %U at byte %d (line %d)", name, r, i, line)
			return
		}
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
