package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "report.txt")

	for _, content := range []string{"first", "second"} {
		if err := writeFileAtomic(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != content {
			t.Fatalf("content = %q, want %q", got, content)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644 (reports must stay readable by httpd)", info.Mode().Perm())
	}
	assertNoTempFiles(t, filepath.Dir(path))
}

// A failed write must leave neither the target nor a temp file behind.
func TestWriteFileAtomicFailure(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "file")
	if err := os.WriteFile(notADir, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(filepath.Join(notADir, "x.json"), []byte("x"), 0o644); err == nil {
		t.Fatal("want an error when the parent is a regular file")
	}

	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	// Renaming a file over a non-empty directory fails after the temp file
	// was written, which exercises the cleanup path.
	if err := os.WriteFile(filepath.Join(target, "keep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(target, []byte("x"), 0o644); err == nil {
		t.Fatal("want an error when the target is a directory")
	}
	assertNoTempFiles(t, dir)
}

// Concurrent writers (overlapping runs) each rename a complete file into
// place; the result is one writer's full content, never a mix.
func TestWriteFileAtomicConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	contents := map[string]bool{}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		content := fmt.Sprintf("writer %d %0512d", i, i)
		contents[content] = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := writeFileAtomic(path, []byte(content), 0o644); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !contents[string(got)] {
		t.Fatalf("file holds a mix of writers: %q", got)
	}
	assertNoTempFiles(t, filepath.Dir(path))
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) > 0 {
		t.Errorf("temp files left behind: %v", matches)
	}
}

// Readers (httpd, the peer) that open the report while runs keep replacing
// it always read one complete, valid version: the rename swaps the whole
// file, and an open descriptor keeps reading the version it opened.
func TestJSONReportReadersNeverSeeTornFile(t *testing.T) {
	dir := t.TempDir()
	conf := config{HTMLStatusFile: filepath.Join(dir, "index.html")}
	jsonFile := jsonReportPath(conf.HTMLStatusFile)
	states := []state{
		{checks: map[string]checkState{"Short": {Status: nagiosOk, Output: "ok"}}},
		{checks: map[string]checkState{"Long": {Status: nagiosCritical, Output: strings.Repeat("long output ", 5000)}}},
	}
	if err := persistJSONReport(states[0], "s", conf, true); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				data, err := os.ReadFile(jsonFile)
				if err != nil {
					t.Error(err)
					return
				}
				if !json.Valid(data) {
					t.Errorf("reader saw an invalid report of %d bytes", len(data))
					return
				}
			}
		}()
	}
	for i := 0; i < 200; i++ {
		if err := persistJSONReport(states[i%2], "s", conf, true); err != nil {
			t.Error(err)
			break
		}
	}
	close(done)
	wg.Wait()
	assertNoTempFiles(t, dir)
}
