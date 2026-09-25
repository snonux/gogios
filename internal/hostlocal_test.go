package internal

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	activeHost  = "active.example"
	passiveHost = "passive.example"
)

// passiveConf is the passive node's config: one host-local check that exits
// CRITICAL and one shared check the passive must never run.
func passiveConf() config {
	return config{
		hostname:         passiveHost,
		CheckConcurrency: 2,
		CheckTimeoutS:    5,
		Checks: map[string]check{
			"Check Disk passive": {Plugin: "/bin/sh", Args: []string{"-c", "echo DISK CRITICAL; exit 2"}, Local: true},
			"Check Ping shared":  {Plugin: "/bin/sh", Args: []string{"-c", "echo ran; exit 0"}},
		},
	}
}

func TestLocalOnly(t *testing.T) {
	local := passiveConf().localOnly()
	if len(local.Checks) != 1 || !local.Checks["Check Disk passive"].Local {
		t.Fatalf("localOnly kept %v, want only the Local check", local.Checks)
	}
	if len(passiveConf().Checks) != 2 {
		t.Fatal("localOnly must not modify the original config's check map")
	}
	if got := (config{}).localOnly(); len(got.Checks) != 0 {
		t.Fatalf("empty config: got %v", got.Checks)
	}
}

func TestTagLocal(t *testing.T) {
	conf := passiveConf()
	s := state{checks: map[string]checkState{
		"Check Disk passive": {Status: nagiosOk},
		"Check Ping shared":  {Status: nagiosOk, Host: "leftover"}, // was Local once
		"Unconfigured":       {Status: nagiosOk, Host: activeHost},
	}}
	s.tagLocal(conf)
	if got := s.checks["Check Disk passive"].Host; got != passiveHost {
		t.Errorf("local check Host = %q, want %q", got, passiveHost)
	}
	if got := s.checks["Check Ping shared"].Host; got != "" {
		t.Errorf("non-local check Host = %q, want empty", got)
	}
	if got := s.checks["Unconfigured"].Host; got != activeHost {
		t.Errorf("unconfigured (merged) check Host changed to %q", got)
	}
}

// The passive node runs its host-local check (and only that), keeps the
// peer's view of everything else and judges the change against its own last
// result, not the peer's older copy of it.
func TestCollectPassive(t *testing.T) {
	now := time.Now().Unix()
	conf := passiveConf()
	persisted := state{checks: map[string]checkState{
		"Check Disk passive": {Status: nagiosOk, PrevStatus: nagiosOk, Epoch: now - 300, Host: passiveHost},
	}}
	peer := peerSnapshot{LastUpdated: time.Now(), ChecksActive: true, Checks: map[string]checkState{
		"Check Ping shared":  {Status: nagiosCritical, PrevStatus: nagiosCritical, Epoch: now, Output: "peer says"},
		"Check Disk active":  {Status: nagiosWarning, PrevStatus: nagiosWarning, Epoch: now, Host: activeHost},
		"Check Disk passive": {Status: nagiosWarning, PrevStatus: nagiosWarning, Epoch: now - 600, Host: passiveHost},
		"Check Gone passive": {Status: nagiosOk, Epoch: now - 600, Host: passiveHost},
	}}

	got := collectPassive(context.Background(), persisted, conf, peer)

	disk := got.checks["Check Disk passive"]
	if disk.Status != nagiosCritical || disk.PrevStatus != nagiosOk || disk.Host != passiveHost {
		t.Errorf("own local check = %+v, want CRITICAL with prev OK (own last result) and Host %s", disk, passiveHost)
	}
	if got.checks["Check Ping shared"].Output != "peer says" {
		t.Errorf("non-local check was run on the passive node: %+v", got.checks["Check Ping shared"])
	}
	if got.checks["Check Disk active"].Host != activeHost {
		t.Errorf("peer's host-local check missing from the mirrored state: %v", got.checks)
	}
	if _, ok := got.checks["Check Gone passive"]; ok {
		t.Error("a mirrored own check no longer configured must be dropped")
	}

	owned := got.ownedBy(passiveHost, true)
	if len(owned.checks) != 1 || !owned.hasCriticalChange(conf) {
		t.Fatalf("passive must own and notify for its local CRITICAL change only, owned %v", owned.checks)
	}
}

// Without a mirrorable peer snapshot the passive keeps its local state and
// still runs its host-local checks.
func TestCollectPassiveNoPeerChecks(t *testing.T) {
	persisted := state{checks: map[string]checkState{"Check Ping shared": {Status: nagiosOk, Output: "old"}}}
	got := collectPassive(context.Background(), persisted, passiveConf(), peerSnapshot{})
	if got.checks["Check Ping shared"].Output != "old" {
		t.Error("local state must be kept when the peer has no checks")
	}
	if got.checks["Check Disk passive"].Status != nagiosCritical {
		t.Errorf("host-local check did not run: %+v", got.checks["Check Disk passive"])
	}
}

func TestMergePeerLocal(t *testing.T) {
	now := time.Now()
	conf := config{hostname: activeHost, Checks: map[string]check{"Defined here": {}}}
	peer := peerSnapshot{LastUpdated: now.Add(-time.Minute), Checks: map[string]checkState{
		"Check Disk passive": {Status: nagiosCritical, PrevStatus: nagiosOk, Host: passiveHost},
		"Shared":             {Status: nagiosCritical},                    // not host-local
		"Check Disk active":  {Status: nagiosWarning, Host: activeHost},   // our own, mirrored back
		"Defined here":       {Status: nagiosCritical, Host: passiveHost}, // name clash
	}}
	s := state{checks: map[string]checkState{"Check Disk active": {Status: nagiosOk, Host: activeHost}}}

	got := mergePeerLocal(s, peer, conf, now, 10*time.Minute)

	if cs := got.checks["Check Disk passive"]; cs.Status != nagiosCritical || cs.PrevStatus != nagiosOk {
		t.Errorf("peer host-local check = %+v, want merged as is", cs)
	}
	if _, ok := got.checks["Shared"]; ok {
		t.Error("a peer check without Host must not be merged")
	}
	if got.checks["Check Disk active"].Status != nagiosOk {
		t.Error("our own host-local result must not be replaced by the peer's copy")
	}
	if _, ok := got.checks["Defined here"]; ok {
		t.Error("a locally defined check must not be overwritten by the peer")
	}

	owned := got.ownedBy(activeHost, false)
	if _, ok := owned.checks["Check Disk passive"]; ok {
		t.Error("the active node must not notify for the peer's host-local checks")
	}
	if owned.hasCriticalChange(config{}) {
		t.Error("the peer's CRITICAL change must not trigger a mail on the active node")
	}
}

// A stale peer report turns the peer's host-local checks UNKNOWN.
func TestMergePeerLocalStalePeer(t *testing.T) {
	now := time.Now()
	peer := peerSnapshot{LastUpdated: now.Add(-time.Hour), Checks: map[string]checkState{
		"Check Disk passive": {Status: nagiosOk, PrevStatus: nagiosOk, Epoch: now.Add(-time.Hour).Unix(), Output: "DISK OK", Host: passiveHost},
	}}
	got := mergePeerLocal(state{checks: map[string]checkState{}}, peer, config{hostname: activeHost}, now, 10*time.Minute)
	cs := got.checks["Check Disk passive"]
	if cs.Status != nagiosUnknown || cs.changed() || !strings.Contains(cs.Output, "stale") || !strings.Contains(cs.Output, "DISK OK") {
		t.Fatalf("stale peer check = %+v, want unchanged UNKNOWN naming the staleness and last result", cs)
	}
}

func TestOwnedBy(t *testing.T) {
	s := state{staleEpoch: 42, checks: map[string]checkState{
		"Shared":       {},
		"Mine":         {Host: activeHost},
		"Peer's local": {Host: passiveHost},
	}}
	tests := []struct {
		name    string
		host    string
		passive bool
		want    []string
	}{
		{"active owns all but the peer's local checks", activeHost, false, []string{"Shared", "Mine"}},
		{"passive owns only its local checks", passiveHost, true, []string{"Peer's local"}},
		{"passive without local checks owns nothing", "other.example", true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owned := s.ownedBy(tt.host, tt.passive)
			if len(owned.checks) != len(tt.want) || owned.staleEpoch != 42 {
				t.Fatalf("owned %v (staleEpoch %d), want %v", owned.checks, owned.staleEpoch, tt.want)
			}
			for _, name := range tt.want {
				if _, ok := owned.checks[name]; !ok {
					t.Errorf("%s not owned", name)
				}
			}
		})
	}
}

// Host and Hidden survive the JSON report round trip the peers use.
func TestHostLocalJSONRoundTrip(t *testing.T) {
	now := time.Now().Unix()
	s := state{checks: map[string]checkState{
		"Check Disk passive": {Status: nagiosCritical, PrevStatus: nagiosOk, Epoch: now, Host: passiveHost, Hidden: true},
	}}
	encoded, err := json.Marshal(s.jsonReport("s", config{}, false))
	if err != nil {
		t.Fatal(err)
	}
	var report peerReport
	if err := json.Unmarshal(encoded, &report); err != nil {
		t.Fatal(err)
	}
	if got := checksFromSections(report.Sections)["Check Disk passive"]; got.Host != passiveHost || got.PrevStatus != nagiosOk || !got.Hidden || !got.PrevHidden {
		t.Fatalf("round trip = %+v", got)
	}
}

// newState keeps a peer's host-local checks and peerLocalCheck when a peer
// is configured, so they can turn UNKNOWN when the peer is unreachable, and
// drops them (and this node's own unconfigured ones) otherwise.
func TestNewStateKeepsPeerLocal(t *testing.T) {
	persisted := map[string]checkState{
		"Check Disk passive": {Status: nagiosOk, Host: passiveHost},
		"Check Gone active":  {Status: nagiosOk, Host: activeHost},
		peerLocalCheck:       {Status: nagiosOk},
		"Removed":            {Status: nagiosOk},
		"Prometheus: X":      {Status: nagiosOk},
	}
	tests := []struct {
		name    string
		peerURL string
		want    []string
	}{
		{"with peer", "https://passive.example/gogios/index.json", []string{"Check Disk passive", peerLocalCheck, "Prometheus: X"}},
		{"without peer", "", []string{"Prometheus: X"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			s := state{stateFile: dir + "/state.json", checks: persisted}
			if err := s.persist(); err != nil {
				t.Fatal(err)
			}
			conf := config{StateDir: dir, hostname: activeHost, PeerURL: tt.peerURL, Checks: map[string]check{}}
			loaded, err := newState(conf)
			if err != nil {
				t.Fatal(err)
			}
			if len(loaded.checks) != len(tt.want) {
				t.Fatalf("kept %v, want %v", loaded.checks, tt.want)
			}
			for _, name := range tt.want {
				if _, ok := loaded.checks[name]; !ok {
					t.Errorf("%s dropped", name)
				}
			}
		})
	}
}

// An unreachable peer turns its host-local checks UNKNOWN instead of making
// them vanish; peerLocalCheck goes UNKNOWN, then CRITICAL (mailed by the
// active node) on the second failure in a row, and its recovery mails too.
func TestMergePeerUnreachable(t *testing.T) {
	now := time.Now()
	conf := config{hostname: activeHost, PeerURL: "https://passive.example/gogios/index.json", Checks: map[string]check{
		"Check Disk active": {Local: true},
	}}
	s := state{checks: map[string]checkState{
		"Check Disk passive": {Status: nagiosCritical, PrevStatus: nagiosCritical, Epoch: now.Unix() - 300, Output: "DISK CRITICAL", Host: passiveHost},
		"Check Disk active":  {Status: nagiosOk, PrevStatus: nagiosOk, Host: activeHost},
		peerLocalCheck:       {Status: nagiosOk, PrevStatus: nagiosOk},
	}}
	down := errors.New("connection refused")

	s = mergePeer(s, peerSnapshot{}, down, conf, now, 10*time.Minute)
	disk := s.checks["Check Disk passive"]
	if disk.Status != nagiosUnknown || disk.changed() || disk.Epoch != now.Unix()-300 ||
		!strings.Contains(disk.Output, "unreachable") || !strings.Contains(disk.Output, "DISK CRITICAL") {
		t.Fatalf("peer check after a failed fetch = %+v, want unchanged UNKNOWN with the old epoch and last result", disk)
	}
	if s.checks["Check Disk active"].Status != nagiosOk {
		t.Error("the node's own host-local check must not be touched")
	}
	if pc := s.checks[peerLocalCheck]; pc.Status != nagiosUnknown {
		t.Fatalf("first failure: %s = %+v, want UNKNOWN", peerLocalCheck, pc)
	}
	if s.ownedBy(activeHost, false).hasCriticalChange(conf) {
		t.Fatal("a single failed fetch must not mail")
	}

	s = mergePeer(s, peerSnapshot{}, down, conf, now, 10*time.Minute)
	if s.checks["Check Disk passive"].Output != disk.Output {
		t.Errorf("UNKNOWN output nested: %q", s.checks["Check Disk passive"].Output)
	}
	owned := s.ownedBy(activeHost, false)
	if pc := owned.checks[peerLocalCheck]; pc.Status != nagiosCritical || !owned.hasCriticalChange(conf) {
		t.Fatalf("second failure: %s = %+v, want a mailed CRITICAL owned by the active node", peerLocalCheck, pc)
	}

	peer := peerSnapshot{LastUpdated: now, Checks: map[string]checkState{
		"Check Disk passive": {Status: nagiosOk, PrevStatus: nagiosCritical, Host: passiveHost},
	}}
	s = mergePeer(s, peer, nil, conf, now, 10*time.Minute)
	if pc := s.checks[peerLocalCheck]; pc.Status != nagiosOk || !s.ownedBy(activeHost, false).hasCriticalChange(conf) {
		t.Fatalf("peer back: %s = %+v, want a mailed recovery to OK", peerLocalCheck, pc)
	}
	if !strings.Contains(s.checks[peerLocalCheck].Output, passiveHost) {
		t.Errorf("OK output %q does not name the peer host", s.checks[peerLocalCheck].Output)
	}
}

// A fetched but stale peer report makes peerLocalCheck CRITICAL right away.
func TestMergePeerStaleIsCritical(t *testing.T) {
	now := time.Now()
	conf := config{hostname: activeHost, PeerURL: "https://passive.example/"}
	s := state{checks: map[string]checkState{}}
	peer := peerSnapshot{LastUpdated: now.Add(-time.Hour), Checks: map[string]checkState{
		"Check Disk passive": {Status: nagiosOk, Host: passiveHost},
	}}
	s = mergePeer(s, peer, nil, conf, now, 10*time.Minute)
	if pc := s.checks[peerLocalCheck]; pc.Status != nagiosCritical || !strings.Contains(pc.Output, "stale") {
		t.Fatalf("%s = %+v, want CRITICAL naming the staleness", peerLocalCheck, pc)
	}
}

// A check the peer no longer reports disappears from the merged state.
func TestMergePeerLocalDropsVanished(t *testing.T) {
	now := time.Now()
	conf := config{hostname: activeHost}
	s := state{checks: map[string]checkState{
		"Check Old passive": {Status: nagiosOk, Host: passiveHost},
		"Shared":            {Status: nagiosOk},
	}}
	peer := peerSnapshot{LastUpdated: now, Checks: map[string]checkState{
		"Check Disk passive": {Status: nagiosOk, Host: passiveHost},
	}}
	got := mergePeerLocal(s, peer, conf, now, 10*time.Minute)
	if _, ok := got.checks["Check Old passive"]; ok {
		t.Error("a peer check missing from the peer report must be dropped")
	}
	if _, ok := got.checks["Shared"]; !ok {
		t.Error("a non-peer check must be kept")
	}
	if _, ok := got.checks["Check Disk passive"]; !ok {
		t.Error("the reported peer check must be merged")
	}
}
