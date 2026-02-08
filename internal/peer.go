package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"
)

type peerReport struct {
	LastUpdated string `json:"lastUpdated"`
}

func peerActive(ctx context.Context, conf config) (bool, string) {
	if conf.PeerURL == "" {
		return true, "Peer failover: disabled (PeerURL not set)"
	}

	hostname, err := os.Hostname()
	if err != nil {
		return true, fmt.Sprintf("Peer failover: hostname lookup failed (%v); staying active", err)
	}

	return peerActiveAt(ctx, conf, time.Now(), hostname, fetchPeerLastUpdated)
}

func peerActiveAt(
	ctx context.Context,
	conf config,
	now time.Time,
	hostname string,
	fetch func(context.Context, string) (time.Time, error),
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

	staleThresholdS := conf.PeerStaleThresholdS
	if staleThresholdS == 0 {
		staleThresholdS = 600
	}

	lastUpdated, err := fetch(ctx, conf.PeerURL)
	if err != nil {
		return true, fmt.Sprintf("Peer failover: peer check failed (%v); staying active", err)
	}

	age := now.Sub(lastUpdated)
	if age > time.Duration(staleThresholdS)*time.Second {
		return true, fmt.Sprintf("Peer failover: peer stale (%v > %ds); staying active",
			age, staleThresholdS)
	}

	master := scheduledMaster(primary, secondary, now)
	if hostname == master {
		return true, fmt.Sprintf("Peer failover: peer healthy; scheduled master is %s", master)
	}

	return false, fmt.Sprintf("Peer failover: peer healthy; scheduled master is %s", master)
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

func fetchPeerLastUpdated(ctx context.Context, peerURL string) (time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, peerURL, nil)
	if err != nil {
		return time.Time{}, err
	}

	client := http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return time.Time{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var report peerReport
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return time.Time{}, err
	}
	if report.LastUpdated == "" {
		return time.Time{}, fmt.Errorf("missing lastUpdated")
	}

	lastUpdated, err := time.Parse(time.RFC3339, report.LastUpdated)
	if err != nil {
		return time.Time{}, err
	}

	return lastUpdated, nil
}
