package internal

import (
	"context"
	"fmt"
	"log"
	"os"
)

func Run(ctx context.Context, configFile string, renotify, force bool) error {
	conf, err := newConfig(configFile)
	if err != nil {
		return err
	}

	if err := conf.sanityCheck(); err != nil {
		notifyError(conf, err)
	}

	state, err := newState(conf)
	if err != nil {
		notifyError(conf, err)
	}

	// Load notification state for batching (tracks when last email was sent
	// and what the check states were at that time)
	notifyStateData, err := newNotifyState(conf.StateDir)
	if err != nil {
		log.Println("warning: failed to load notification state:", err)
	}

	decision := peerDecision{Active: true}
	if conf.PeerURL != "" {
		decision = peerActive(ctx, conf)
		log.Println(decision.Reason)
	}
	state = collect(ctx, state, conf, decision)

	if err := state.persist(); err != nil {
		notifyError(conf, err)
	}

	passive := !decision.Active
	subject, body, doNotify := state.report(renotify, force, conf.StatusPageURL, conf, passive, decision.Reason)
	doNotify = gateNotification(doNotify, force, passive, conf, notifyStateData, state)

	if doNotify {
		if err := notify(conf, subject, body); err != nil {
			log.Println("error:", err)
			return nil
		}
		// Record notification timestamp and state snapshot for batching
		if err := notifyStateData.recordNotification(state); err != nil {
			log.Println("warning: failed to save notification state:", err)
		}
	}

	publishReports(state, subject, body, conf, decision.Active)
	return nil
}

// collect runs the checks on the elected checker. A passive node never runs
// plugins, not even with -force (which only forces notifications from the
// existing state); it mirrors the active peer's state instead.
func collect(ctx context.Context, state state, conf config, decision peerDecision) state {
	if !decision.Active {
		log.Println("Skipping checks: peer is active")
		return mirrorPeerState(state, decision.Peer)
	}
	state = runChecks(ctx, state, conf)
	state = mergePrometheusAlerts(ctx, state, conf)
	return mergeFederated(ctx, state, conf)
}

// gateNotification applies notification batching and the passive-node rule
// to the report's notify decision.
func gateNotification(doNotify, force, passive bool, conf config, ns notifyState, state state) bool {
	// Apply notification batching when MinNotifyIntervalS is configured.
	// Force flag bypasses batching to allow immediate notifications when needed.
	if doNotify && conf.MinNotifyIntervalS > 0 && !force {
		if ns.intervalElapsed(conf.MinNotifyIntervalS) {
			// Interval has elapsed - only notify if state changed since last notification
			if !ns.hasChanges(state) {
				doNotify = false
				log.Println("Notification suppressed: interval elapsed but no state changes since last notification")
			}
		} else {
			// Interval has not elapsed - suppress notification
			doNotify = false
			log.Println("Notification suppressed: minimum interval not elapsed")
		}
	}

	if passive && !force {
		doNotify = false
		log.Println("Notification suppressed: peer is active")
	} else if passive && force {
		log.Println("Force notify while passive: skipping checks but allowing notifications")
	}
	return doNotify
}

// publishReports writes the text, HTML and JSON reports. They always update,
// regardless of notification batching; checksActive tells the peer whether
// this node ran the checks.
func publishReports(state state, subject, body string, conf config, checksActive bool) {
	if err := persistReport(subject, body, conf); err != nil {
		notifyError(conf, err)
	}
	if conf.HTMLDisable {
		return
	}
	if err := persistHTMLReport(state, subject, conf); err != nil {
		notifyError(conf, err)
	}
	if err := persistJSONReport(state, subject, conf, checksActive); err != nil {
		notifyError(conf, err)
	}
}

func persistReport(subject, body string, conf config) error {
	reportFile := fmt.Sprintf("%s/report.txt", conf.StateDir)
	tmpFile := fmt.Sprintf("%s.tmp", reportFile)

	f, err := os.Create(tmpFile)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err = f.WriteString(fmt.Sprintf("%s\n\n", subject)); err != nil {
		return err
	}
	if _, err = f.WriteString(body); err != nil {
		return err
	}
	return os.Rename(tmpFile, reportFile)
}
