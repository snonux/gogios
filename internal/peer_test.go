package internal

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPeerActiveAtStale(t *testing.T) {
	now := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	conf := config{
		PeerURL:             "https://peer.example/gogios/index.json",
		PeerStaleThresholdS: 600,
		PeerPrimaryName:     "primary",
		PeerSecondaryName:   "secondary",
	}

	lastUpdated := now.Add(-11 * time.Minute)
	active, _ := peerActiveAt(context.Background(), conf, now, "secondary",
		func(context.Context, string) (time.Time, error) {
			return lastUpdated, nil
		})

	if !active {
		t.Fatalf("expected active when peer is stale")
	}
}

func TestPeerActiveAtFreshStandby(t *testing.T) {
	now := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	conf := config{
		PeerURL:             "https://peer.example/gogios/index.json",
		PeerStaleThresholdS: 600,
		PeerPrimaryName:     "primary",
		PeerSecondaryName:   "secondary",
	}

	lastUpdated := now.Add(-1 * time.Minute)
	active, _ := peerActiveAt(context.Background(), conf, now, "secondary",
		func(context.Context, string) (time.Time, error) {
			return lastUpdated, nil
		})

	if active {
		t.Fatalf("expected passive when peer is healthy and local is standby")
	}
}

func TestPeerActiveAtFetchError(t *testing.T) {
	now := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	conf := config{
		PeerURL:             "https://peer.example/gogios/index.json",
		PeerStaleThresholdS: 600,
		PeerPrimaryName:     "primary",
		PeerSecondaryName:   "secondary",
	}

	active, _ := peerActiveAt(context.Background(), conf, now, "secondary",
		func(context.Context, string) (time.Time, error) {
			return time.Time{}, errors.New("boom")
		})

	if !active {
		t.Fatalf("expected active on peer fetch error")
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
