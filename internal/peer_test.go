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

// elect runs the election with a fixed clock and hostname and no DNS
// resolver, returning whether the local node stays active and why.
func elect(conf config, now time.Time, hostname string,
	fetch func(context.Context, string) (peerSnapshot, error), probe hostProber,
) (bool, string) {
	d := peerActiveAt(context.Background(), conf, peerEnv{now: now, hostname: hostname, fetch: fetch, probe: probe})
	return d.Active, d.Reason
}

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
	active, _ := elect(conf, now, "secondary",
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
	active, _ := elect(conf, now, "secondary",
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

	active, _ := elect(conf, now, "secondary",
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

	active, reason := elect(conf, now, "secondary",
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

	active, _ := elect(conf, now, "primary",
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

	active, _ := elect(conf, now, "primary",
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

	active, reason := elect(conf, now, "primary",
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

	active, _ := elect(conf, now, "primary",
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

// fakeResolver answers from a fixed table; unknown names fail.
func fakeResolver(table map[string][]string) hostResolver {
	return func(_ context.Context, host string) ([]string, error) {
		if addrs, ok := table[host]; ok {
			return addrs, nil
		}
		return nil, errors.New("no such host " + host)
	}
}

// dnsConf has a role file naming primary as standby, so a DNS answer that
// elects secondary proves the record takes precedence over the file.
func dnsConf(t *testing.T) config {
	t.Helper()
	standbyFile := filepath.Join(t.TempDir(), "current_standby")
	if err := os.WriteFile(standbyFile, []byte("primary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return config{
		PeerURL:           "https://peer.example/gogios/index.json",
		PeerPrimaryName:   "primary",
		PeerSecondaryName: "secondary",
		DNSStandbyFile:    standbyFile,
		DNSStandbyRecord:  "standby.example",
	}
}

func TestDNSStandbyNameRecordBeatsRoleFile(t *testing.T) {
	env := peerEnv{resolve: fakeResolver(map[string][]string{
		"standby.example": {"2001:db8::2", "192.0.2.2"},
		"primary":         {"192.0.2.1", "2001:db8::1"},
		"secondary":       {"192.0.2.2", "2001:db8::2"},
	})}
	if got := dnsStandbyName(context.Background(), dnsConf(t), "primary", "secondary", env); got != "secondary" {
		t.Fatalf("standby = %q, want secondary (from DNS, not the stale role file)", got)
	}
}

func TestDNSStandbyNameFallsBackToRoleFile(t *testing.T) {
	tests := map[string]map[string][]string{
		"record does not resolve": {"primary": {"192.0.2.1"}, "secondary": {"192.0.2.2"}},
		"peer does not resolve":   {"standby.example": {"192.0.2.2"}, "primary": {"192.0.2.1"}},
		"record matches no peer":  {"standby.example": {"192.0.2.9"}, "primary": {"192.0.2.1"}, "secondary": {"192.0.2.2"}},
		"record matches both":     {"standby.example": {"192.0.2.1", "192.0.2.2"}, "primary": {"192.0.2.1"}, "secondary": {"192.0.2.2"}},
		"record has junk address": {"standby.example": {"not-an-ip"}, "primary": {"not-an-ip"}, "secondary": {"192.0.2.2"}},
	}
	for name, table := range tests {
		t.Run(name, func(t *testing.T) {
			env := peerEnv{resolve: fakeResolver(table)}
			if got := dnsStandbyName(context.Background(), dnsConf(t), "primary", "secondary", env); got != "primary" {
				t.Fatalf("standby = %q, want primary from the role file", got)
			}
		})
	}
}

func TestDNSStandbyNameWithoutRecordIgnoresResolver(t *testing.T) {
	conf := dnsConf(t)
	conf.DNSStandbyRecord = ""
	env := peerEnv{resolve: func(context.Context, string) ([]string, error) {
		t.Fatal("resolver must not be used without DNSStandbyRecord")
		return nil, nil
	}}
	if got := dnsStandbyName(context.Background(), conf, "primary", "secondary", env); got != "primary" {
		t.Fatalf("standby = %q, want primary from the role file", got)
	}
}

// The passive decision carries the peer snapshot so Run can mirror it; an
// active decision carries none.
func TestPeerActiveAtPassiveCarriesPeerChecks(t *testing.T) {
	now := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	conf := dnsConf(t)
	peer := peerSnapshot{LastUpdated: now, ChecksActive: true, Checks: map[string]checkState{"A": {Status: nagiosOk}}}
	env := peerEnv{
		now: now, hostname: "primary", probe: reachable,
		fetch: func(context.Context, string) (peerSnapshot, error) { return peer, nil },
		resolve: fakeResolver(map[string][]string{
			"standby.example": {"192.0.2.2"}, "primary": {"192.0.2.1"}, "secondary": {"192.0.2.2"},
		}),
	}

	d := peerActiveAt(context.Background(), conf, env)
	if d.Active || len(d.Peer.Checks) != 1 {
		t.Fatalf("want passive with the peer's checks, got active=%v checks=%v (%s)", d.Active, d.Peer.Checks, d.Reason)
	}

	env.hostname = "secondary"
	if d := peerActiveAt(context.Background(), conf, env); !d.Active || d.Peer.Checks != nil {
		t.Fatalf("DNS standby must be active without a peer snapshot, got %+v", d)
	}
}
