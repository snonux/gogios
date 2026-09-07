package internal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func reachable(_ context.Context, _ string) error { return nil }

func unreachable(_ context.Context, host string) error {
	return errors.New("no route to " + host)
}

func TestPeerActiveAtStale(t *testing.T) {
	now := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	conf := config{
		PeerURL:             "https://peer.example/gogios/index.json",
		PeerStaleThresholdS: 600,
		PeerPrimaryName:     "primary",
		PeerSecondaryName:   "secondary",
		DNSStandbyFile:      filepath.Join(t.TempDir(), "missing"),
	}

	lastUpdated := now.Add(-11 * time.Minute)
	active, _ := peerActiveAt(context.Background(), conf, now, "secondary",
		func(context.Context, string) (peerSnapshot, error) {
			return peerSnapshot{LastUpdated: lastUpdated, ChecksActive: true}, nil
		}, reachable)

	if !active {
		t.Fatalf("expected active when peer is stale")
	}
}

func TestPeerActiveAtFreshDNSMasterPassive(t *testing.T) {
	// Week 1 (odd): scheduled standby is primary. Local secondary is DNS master.
	now := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	conf := config{
		PeerURL:             "https://peer.example/gogios/index.json",
		PeerStaleThresholdS: 600,
		PeerPrimaryName:     "primary",
		PeerSecondaryName:   "secondary",
		DNSStandbyFile:      filepath.Join(t.TempDir(), "missing"),
	}

	lastUpdated := now.Add(-1 * time.Minute)
	active, _ := peerActiveAt(context.Background(), conf, now, "secondary",
		func(context.Context, string) (peerSnapshot, error) {
			return peerSnapshot{LastUpdated: lastUpdated, ChecksActive: true}, nil
		}, reachable)

	if active {
		t.Fatalf("expected passive when peer is healthy, checksActive, and local is DNS master")
	}
}

func TestPeerActiveAtFetchError(t *testing.T) {
	now := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	conf := config{
		PeerURL:             "https://peer.example/gogios/index.json",
		PeerStaleThresholdS: 600,
		PeerPrimaryName:     "primary",
		PeerSecondaryName:   "secondary",
		DNSStandbyFile:      filepath.Join(t.TempDir(), "missing"),
	}

	active, _ := peerActiveAt(context.Background(), conf, now, "secondary",
		func(context.Context, string) (peerSnapshot, error) {
			return peerSnapshot{}, errors.New("boom")
		}, reachable)

	if !active {
		t.Fatalf("expected active on peer fetch error")
	}
}

func TestPeerActiveAtStandbyAlwaysActive(t *testing.T) {
	now := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	standbyFile := filepath.Join(dir, "current_standby")
	if err := os.WriteFile(standbyFile, []byte("secondary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conf := config{
		PeerURL:             "https://peer.example/gogios/index.json",
		PeerStaleThresholdS: 600,
		PeerPrimaryName:     "primary",
		PeerSecondaryName:   "secondary",
		DNSStandbyFile:      standbyFile,
	}

	active, reason := peerActiveAt(context.Background(), conf, now, "secondary",
		func(context.Context, string) (peerSnapshot, error) {
			t.Fatal("standby must not fetch peer to decide activity")
			return peerSnapshot{}, nil
		}, unreachable)
	if !active {
		t.Fatalf("expected standby active, reason=%s", reason)
	}
}

func TestPeerActiveAtMasterTakeoverWhenPeerNotChecking(t *testing.T) {
	now := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	standbyFile := filepath.Join(dir, "current_standby")
	if err := os.WriteFile(standbyFile, []byte("secondary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conf := config{
		PeerURL:             "https://peer.example/gogios/index.json",
		PeerStaleThresholdS: 600,
		PeerPrimaryName:     "primary",
		PeerSecondaryName:   "secondary",
		DNSStandbyFile:      standbyFile,
	}

	active, _ := peerActiveAt(context.Background(), conf, now, "primary",
		func(context.Context, string) (peerSnapshot, error) {
			return peerSnapshot{LastUpdated: now.Add(-time.Minute), ChecksActive: false}, nil
		}, reachable)
	if !active {
		t.Fatalf("expected master active when peer is not checksActive")
	}
}

func TestPeerActiveAtMasterPassiveWhenPeerChecksActive(t *testing.T) {
	now := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	standbyFile := filepath.Join(dir, "current_standby")
	if err := os.WriteFile(standbyFile, []byte("secondary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conf := config{
		PeerURL:             "https://peer.example/gogios/index.json",
		PeerStaleThresholdS: 600,
		PeerPrimaryName:     "primary",
		PeerSecondaryName:   "secondary",
		DNSStandbyFile:      standbyFile,
	}

	active, _ := peerActiveAt(context.Background(), conf, now, "primary",
		func(context.Context, string) (peerSnapshot, error) {
			return peerSnapshot{LastUpdated: now.Add(-time.Minute), ChecksActive: true}, nil
		}, reachable)
	if active {
		t.Fatalf("expected master passive when peer checksActive")
	}
}

func TestPeerActiveAtMasterTakeoverWhenStandbyUnreachable(t *testing.T) {
	now := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	standbyFile := filepath.Join(dir, "current_standby")
	if err := os.WriteFile(standbyFile, []byte("secondary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conf := config{
		PeerURL:             "https://peer.example/gogios/index.json",
		PeerStaleThresholdS: 600,
		PeerPrimaryName:     "primary",
		PeerSecondaryName:   "secondary",
		DNSStandbyFile:      standbyFile,
	}

	active, reason := peerActiveAt(context.Background(), conf, now, "primary",
		func(context.Context, string) (peerSnapshot, error) {
			return peerSnapshot{LastUpdated: now.Add(-time.Minute), ChecksActive: true}, nil
		}, unreachable)
	if !active {
		t.Fatalf("expected master active when standby unreachable, reason=%s", reason)
	}
	if !strings.Contains(reason, "unreachable") {
		t.Fatalf("reason should mention unreachable, got %q", reason)
	}
}

func TestPeerActiveAtInvalidRoleFileFallsBackToWeek(t *testing.T) {
	now := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC) // odd week → standby primary
	dir := t.TempDir()
	standbyFile := filepath.Join(dir, "current_standby")
	if err := os.WriteFile(standbyFile, []byte("unrelated.host\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conf := config{
		PeerURL:             "https://peer.example/gogios/index.json",
		PeerStaleThresholdS: 600,
		PeerPrimaryName:     "primary",
		PeerSecondaryName:   "secondary",
		DNSStandbyFile:      standbyFile,
	}

	active, _ := peerActiveAt(context.Background(), conf, now, "primary",
		func(context.Context, string) (peerSnapshot, error) {
			t.Fatal("week-fallback standby should not need peer fetch")
			return peerSnapshot{}, nil
		}, reachable)
	if !active {
		t.Fatalf("expected primary active via week fallback when role file invalid")
	}
}

func TestScheduledMasterWeekParity(t *testing.T) {
	primary := "primary"
	secondary := "secondary"

	weekOne := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC) // Sunday
	weekTwo := time.Date(2023, 1, 8, 0, 0, 0, 0, time.UTC) // Next Sunday

	if master := scheduledMaster(primary, secondary, weekOne); master != primary {
		t.Fatalf("expected primary to be master in week 1, got %s", master)
	}

	if master := scheduledMaster(primary, secondary, weekTwo); master != secondary {
		t.Fatalf("expected secondary to be master in week 2, got %s", master)
	}
}
