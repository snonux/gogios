package internal

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getcwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod walking up from cwd")
		}
		dir = parent
	}
}

func TestNoGoreleaserConfig(t *testing.T) {
	root := repoRoot(t)
	// Official GoReleaser discovery names, plus the task's typo variant.
	names := []string{
		".goreleaser.yaml",
		".goreleaser.yml",
		"goreleaser.yaml",
		"goreleaser.yml",
		".gireleaser.yaml",
		filepath.Join(".config", "goreleaser.yaml"),
		filepath.Join(".config", "goreleaser.yml"),
	}
	for _, name := range names {
		path := filepath.Join(root, name)
		_, err := os.Stat(path)
		if err == nil {
			t.Errorf("%s must not exist (goreleaser support removed)", name)
			continue
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("stat %s: %v", path, err)
		}
	}

	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	if strings.Contains(string(readme), "GoReleaser") || strings.Contains(string(readme), "goreleaser") {
		t.Error("README.md must not mention goreleaser")
	}

	gitignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	for _, line := range strings.Split(string(gitignore), "\n") {
		if strings.TrimSpace(line) == "dist/" {
			t.Error(".gitignore must not list dist/ (goreleaser artifact dir)")
		}
	}
}

func TestVersionBumpedPastCodebergModuleCutover(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
	if !strings.HasPrefix(Version, "v") {
		t.Fatalf("Version %q should start with 'v'", Version)
	}
	// v1.4.5 was the last release declaring module codeberg.org/snonux/gogios.
	if Version == "v1.4.5" {
		t.Fatal("Version still at v1.4.5; cutover requires a bump (expected v1.4.6+)")
	}
}

func TestHomepageIsGitHub(t *testing.T) {
	if Homepage == "" {
		t.Fatal("Homepage must not be empty")
	}
	if strings.Contains(Homepage, "codeberg.org") {
		t.Fatalf("Homepage %q still points at Codeberg; expected github.com/snonux", Homepage)
	}
	want := "https://github.com/snonux/gogios"
	if Homepage != want {
		t.Fatalf("Homepage = %q, want %q", Homepage, want)
	}
}

func TestVersionBannerUsesHomepage(t *testing.T) {
	got := VersionBanner()
	if !strings.Contains(got, Version) {
		t.Fatalf("banner missing Version %q:\n%s", Version, got)
	}
	if !strings.Contains(got, Homepage) {
		t.Fatalf("banner missing Homepage %q:\n%s", Homepage, got)
	}
	if strings.Contains(got, "codeberg.org") {
		t.Fatalf("banner still mentions codeberg.org:\n%s", got)
	}
}
