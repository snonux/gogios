package internal

import (
	"strings"
	"testing"
)

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
