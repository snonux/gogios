package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultDNSStandbyFile = "/var/nsd/run/current_standby"

// peerReport is the part of the peer's JSON report (see jsonReport) that
// failover reads: freshness, whether it runs checks, and its check sections,
// which a passive node mirrors.
type peerReport struct {
	LastUpdated  string       `json:"lastUpdated"`
	ChecksActive bool         `json:"checksActive"`
	Sections     jsonSections `json:"sections"`
}

type peerSnapshot struct {
	LastUpdated  time.Time
	ChecksActive bool
	// Checks is the peer's check state rebuilt from its report sections; nil
	// when the report carries no sections (e.g. an older Gogios).
	Checks map[string]checkState
}

// peerDecision is the outcome of the failover election. Peer is the snapshot
// of the active peer when the local node goes passive, so the passive node
// can publish the peer's current state instead of its own frozen one.
type peerDecision struct {
	Active bool
	Reason string
	Peer   peerSnapshot
}

// hostProber reports whether a peer hostname is reachable. Used so the DNS
// master takes over checks when the standby is down even if a recent JSON
// report still advertises checksActive.
type hostProber func(ctx context.Context, host string) error

// hostResolver returns the IP addresses of a hostname.
type hostResolver func(ctx context.Context, host string) ([]string, error)

// peerEnv carries the clock, identity and I/O the election depends on, so
// tests can replace each of them.
type peerEnv struct {
	now      time.Time
	hostname string
	fetch    func(context.Context, string) (peerSnapshot, error)
	probe    hostProber
	resolve  hostResolver
}

func peerActive(ctx context.Context, conf config) peerDecision {
	if conf.PeerURL == "" {
		return peerDecision{Active: true, Reason: "Peer failover: disabled (PeerURL not set)"}
	}

	hostname, err := os.Hostname()
	if err != nil {
		return peerDecision{Active: true, Reason: fmt.Sprintf("Peer failover: hostname lookup failed (%v); staying active", err)}
	}

	return peerActiveAt(ctx, conf, peerEnv{
		now:      time.Now(),
		hostname: hostname,
		fetch:    fetchPeerSnapshot,
		probe:    probeHostReachable,
		resolve:  net.DefaultResolver.LookupHost,
	})
}

// peerActiveAt elects the checker: the DNS standby always runs checks; the
// other node goes passive only while the peer is fresh, checksActive and
// reachable, and stays active on any doubt.
func peerActiveAt(ctx context.Context, conf config, env peerEnv) peerDecision {
	active := func(reason string, args ...any) peerDecision {
		return peerDecision{Active: true, Reason: fmt.Sprintf(reason, args...)}
	}
	if conf.PeerURL == "" {
		return active("Peer failover: disabled (PeerURL not set)")
	}

	primary, secondary := peerNames(conf, env.hostname)
	if primary == "" || secondary == "" {
		return active("Peer failover: missing peer names; staying active")
	}
	if env.hostname != primary && env.hostname != secondary {
		return active("Peer failover: local hostname %s not in [%s, %s]; staying active",
			env.hostname, primary, secondary)
	}

	standby := dnsStandbyName(ctx, conf, primary, secondary, env)
	if env.hostname == standby {
		return active("Peer failover: local host is DNS standby checker (%s)", standby)
	}

	staleThresholdS := conf.PeerStaleThresholdS
	if staleThresholdS == 0 {
		staleThresholdS = 600
	}

	peer, err := env.fetch(ctx, conf.PeerURL)
	if err != nil {
		return active("Peer failover: peer check failed (%v); staying active", err)
	}
	if age := env.now.Sub(peer.LastUpdated); age > time.Duration(staleThresholdS)*time.Second {
		return active("Peer failover: peer stale (%v > %ds); staying active", age, staleThresholdS)
	}
	if !peer.ChecksActive {
		return active("Peer failover: peer healthy but not checksActive; taking over (standby %s)", standby)
	}
	if env.probe != nil {
		if err := env.probe(ctx, standby); err != nil {
			return active("Peer failover: DNS standby %s unreachable (%v); taking over", standby, err)
		}
	}

	return peerDecision{
		Reason: fmt.Sprintf("Peer failover: peer healthy and checksActive; DNS standby is %s", standby),
		Peer:   peer,
	}
}

// peerNames returns the configured primary and secondary peer names, falling
// back to the local hostname and the PeerURL host.
func peerNames(conf config, hostname string) (string, string) {
	primary := conf.PeerPrimaryName
	if primary == "" {
		primary = hostname
	}
	secondary := conf.PeerSecondaryName
	if secondary == "" {
		if parsedURL, err := url.Parse(conf.PeerURL); err == nil && parsedURL.Hostname() != "" {
			secondary = parsedURL.Hostname()
		}
	}
	return primary, secondary
}

// dnsStandbyName returns the FQDN that should run gogios plugin checks. In
// order of preference: the peer whose address DNSStandbyRecord resolves to
// (the live DNS answer, which cannot go stale when a DNS publisher stops
// writing role files), the DNSStandbyFile role file, then week parity (even
// week → secondary, odd week → primary).
func dnsStandbyName(ctx context.Context, conf config, primary, secondary string, env peerEnv) string {
	if conf.DNSStandbyRecord != "" && env.resolve != nil {
		name, err := standbyFromDNS(ctx, env.resolve, conf.DNSStandbyRecord, primary, secondary)
		if err == nil {
			return name
		}
		log.Printf("Peer failover: %v; falling back to role file", err)
	}

	path := conf.DNSStandbyFile
	if path == "" {
		path = defaultDNSStandbyFile
	}
	if name := readRoleFile(path); name != "" && (name == primary || name == secondary) {
		return name
	}
	return scheduledStandby(primary, secondary, env.now)
}

// standbyFromDNS resolves record and returns whichever of primary and
// secondary shares an address with it. It fails unless exactly one does.
func standbyFromDNS(ctx context.Context, resolve hostResolver, record, primary, secondary string) (string, error) {
	recordAddrs, err := resolve(ctx, record)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", record, err)
	}
	var matches []string
	for _, name := range []string{primary, secondary} {
		addrs, err := resolve(ctx, name)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", name, err)
		}
		if sharesAddress(recordAddrs, addrs) {
			matches = append(matches, name)
		}
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("%s %v matches %d of [%s, %s]", record, recordAddrs, len(matches), primary, secondary)
	}
	return matches[0], nil
}

func sharesAddress(a, b []string) bool {
	for _, x := range a {
		ipX := net.ParseIP(x)
		if ipX == nil {
			continue
		}
		for _, y := range b {
			if ipX.Equal(net.ParseIP(y)) {
				return true
			}
		}
	}
	return false
}

func readRoleFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// scheduledStandby matches dns-failover week parity: even → secondary, odd → primary.
func scheduledStandby(primary, secondary string, now time.Time) string {
	return scheduledMaster(primary, secondary, now)
}

func scheduledMaster(primary, secondary string, now time.Time) string {
	week := weekNumberSunday(now)
	if week%2 == 0 {
		return secondary
	}
	return primary
}

// weekNumberSunday matches strftime %U (Sunday-based week number, 00-53).
func weekNumberSunday(t time.Time) int {
	tUTC := t.In(time.UTC)
	yearStart := time.Date(tUTC.Year(), 1, 1, 0, 0, 0, 0, time.UTC)

	// Find the first Sunday on or after Jan 1.
	daysUntilSunday := (7 - int(yearStart.Weekday())) % 7
	firstSunday := yearStart.AddDate(0, 0, daysUntilSunday)

	if tUTC.Before(firstSunday) {
		return 0
	}

	daysSinceFirstSunday := int(tUTC.Sub(firstSunday).Hours() / 24)
	return 1 + (daysSinceFirstSunday / 7)
}

func fetchPeerSnapshot(ctx context.Context, peerURL string) (peerSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, peerURL, nil)
	if err != nil {
		return peerSnapshot{}, err
	}

	client := http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return peerSnapshot{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return peerSnapshot{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var report peerReport
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return peerSnapshot{}, err
	}
	if report.LastUpdated == "" {
		return peerSnapshot{}, fmt.Errorf("missing lastUpdated")
	}

	lastUpdated, err := time.Parse(time.RFC3339, report.LastUpdated)
	if err != nil {
		return peerSnapshot{}, err
	}

	return peerSnapshot{
		LastUpdated:  lastUpdated,
		ChecksActive: report.ChecksActive,
		Checks:       checksFromSections(report.Sections),
	}, nil
}

// probeHostReachable checks that the DNS standby is alive before the master
// goes passive. ICMP ping first; TCP :443 fallback for unprivileged contexts.
func probeHostReachable(ctx context.Context, host string) error {
	if err := probeHostICMP(ctx, host); err == nil {
		return nil
	}
	return probeHostTCP(ctx, host, "443")
}

func probeHostICMP(ctx context.Context, host string) error {
	// OpenBSD: -w max wait seconds. Linux busybox/iputils: -W often works too.
	cmd := exec.CommandContext(ctx, "ping", "-c", "1", "-w", "2", host)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

func probeHostTCP(ctx context.Context, host, port string) error {
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return fmt.Errorf("tcp/%s: %w", port, err)
	}
	_ = conn.Close()
	return nil
}
