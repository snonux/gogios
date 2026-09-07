package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultDNSStandbyFile = "/var/nsd/run/current_standby"

type peerReport struct {
	LastUpdated  string `json:"lastUpdated"`
	ChecksActive bool   `json:"checksActive"`
}

type peerSnapshot struct {
	LastUpdated  time.Time
	ChecksActive bool
}

// hostProber reports whether a peer hostname is reachable. Used so the DNS
// master takes over checks when the standby is down even if a recent JSON
// report still advertises checksActive.
type hostProber func(ctx context.Context, host string) error

func peerActive(ctx context.Context, conf config) (bool, string) {
	if conf.PeerURL == "" {
		return true, "Peer failover: disabled (PeerURL not set)"
	}

	hostname, err := os.Hostname()
	if err != nil {
		return true, fmt.Sprintf("Peer failover: hostname lookup failed (%v); staying active", err)
	}

	return peerActiveAt(ctx, conf, time.Now(), hostname, fetchPeerSnapshot, probeHostReachable)
}

func peerActiveAt(
	ctx context.Context,
	conf config,
	now time.Time,
	hostname string,
	fetch func(context.Context, string) (peerSnapshot, error),
	probe hostProber,
) (bool, string) {
	if conf.PeerURL == "" {
		return true, "Peer failover: disabled (PeerURL not set)"
	}

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

	if primary == "" || secondary == "" {
		return true, "Peer failover: missing peer names; staying active"
	}

	if hostname != primary && hostname != secondary {
		return true, fmt.Sprintf("Peer failover: local hostname %s not in [%s, %s]; staying active",
			hostname, primary, secondary)
	}

	standby := dnsStandbyName(conf, primary, secondary, now)
	if hostname == standby {
		return true, fmt.Sprintf("Peer failover: local host is DNS standby checker (%s)", standby)
	}

	staleThresholdS := conf.PeerStaleThresholdS
	if staleThresholdS == 0 {
		staleThresholdS = 600
	}

	peer, err := fetch(ctx, conf.PeerURL)
	if err != nil {
		return true, fmt.Sprintf("Peer failover: peer check failed (%v); staying active", err)
	}

	age := now.Sub(peer.LastUpdated)
	if age > time.Duration(staleThresholdS)*time.Second {
		return true, fmt.Sprintf("Peer failover: peer stale (%v > %ds); staying active",
			age, staleThresholdS)
	}

	if !peer.ChecksActive {
		return true, fmt.Sprintf("Peer failover: peer healthy but not checksActive; taking over (standby %s)", standby)
	}

	if probe != nil {
		if err := probe(ctx, standby); err != nil {
			return true, fmt.Sprintf("Peer failover: DNS standby %s unreachable (%v); taking over", standby, err)
		}
	}

	return false, fmt.Sprintf("Peer failover: peer healthy and checksActive; DNS standby is %s", standby)
}

// dnsStandbyName returns the FQDN that should run gogios plugin checks.
// Prefer /var/nsd/run/current_standby from dns-failover; fall back to week parity
// (even week → secondary, odd week → primary), matching DNS HA standby.
func dnsStandbyName(conf config, primary, secondary string, now time.Time) string {
	path := conf.DNSStandbyFile
	if path == "" {
		path = defaultDNSStandbyFile
	}
	if name := readRoleFile(path); name != "" && (name == primary || name == secondary) {
		return name
	}
	return scheduledStandby(primary, secondary, now)
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
