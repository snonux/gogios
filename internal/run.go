package internal

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// RunOptions are the command-line settings of one run.
type RunOptions struct {
	ConfigFile string
	Renotify   bool
	Force      bool
	// LockWait bounds how long -renotify and -force wait for the run lock.
	LockWait time.Duration
	// Timeout bounds the checks once the lock is held. It starts only
	// then, so time spent waiting for a slow run does not eat into the
	// check budget (which timed checks out as false CRITICALs).
	Timeout time.Duration
}

// Run performs one Gogios run: elect the checker, collect, persist, notify
// and publish the reports. Runs are serialised by a lock in StateDir: a plain
// run skips when another run holds it (that run checks anyway), while
// -renotify and -force wait for it up to LockWait, since their mail must not
// be lost. A skip is loud when the lock is held for longer than any healthy
// run can take (see handleLockedRun). Once locked, a watchdog ends the
// process after Timeout plus runGrace, so no hang can hold the lock for good.
func Run(ctx context.Context, opts RunOptions) error {
	conf, err := newConfig(opts.ConfigFile)
	if err != nil {
		return err
	}

	lockCtx, cancelLock := context.WithTimeout(ctx, opts.LockWait)
	release, err := acquireRunLock(lockCtx, conf.StateDir, opts.Renotify || opts.Force)
	cancelLock()
	if errors.Is(err, errRunLocked) {
		return handleLockedRun(conf, time.Now(), opts.Timeout+runGrace+lockStaleMargin,
			func(subject, body string) error { return notify(conf, subject, body) })
	}
	if err != nil {
		return err
	}
	defer release()

	stop := startWatchdog(opts.Timeout+runGrace, func() {
		fmt.Fprintf(os.Stderr, "gogios: run exceeded %v, exiting to release the run lock\n", opts.Timeout+runGrace)
		os.Exit(3)
	})
	defer stop()
	removeStaleTemps(conf)

	runCtx, cancelRun := context.WithTimeout(ctx, opts.Timeout)
	defer cancelRun()
	runLocked(runCtx, conf, opts.Renotify, opts.Force)
	return nil
}

// runLocked is one run under the run lock. Only collecting honours ctx: persisting, mailing
// and publishing the results must happen even when the checks used up the
// run's time, and they are bounded on their own (smtpTimeout, local I/O).
func runLocked(ctx context.Context, conf config, renotify, force bool) {
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
	state.markHidden(conf)

	if err := state.persist(); err != nil {
		notifyError(conf, err)
	}

	notifyAndPublish(state, conf, decision, notifyStateData, renotify, force)
}

// notifyAndPublish mails the report when the checks this node owns warrant
// it (see ownedBy, notifyTrigger, gateNotification) and publishes the text,
// HTML and JSON reports of the full state.
func notifyAndPublish(state state, conf config, decision peerDecision, ns notifyState, renotify, force bool) {
	passive := !decision.Active
	subject, body := state.report(conf.StatusPageURL, conf, passive, decision.Reason)
	owned := state.ownedBy(conf.hostname, passive)
	doNotify := owned.notifyTrigger(renotify, force, conf)
	doNotify = gateNotification(doNotify, force, conf, ns, owned)

	if doNotify {
		// A failed mail still publishes the reports below: a report left
		// stale would make the peer take this node for dead.
		if err := notify(conf, subject, body); err != nil {
			log.Println("error:", err)
		} else if err := ns.recordNotification(owned); err != nil {
			// Record notification timestamp and state snapshot for batching
			log.Println("warning: failed to save notification state:", err)
		}
	}

	publishReports(state, subject, body, conf, decision.Active)
}

// collect runs the checks. The elected checker runs every check and adds the
// peer's host-local results (UNKNOWN when the peer is unreachable, see
// mergePeer); a passive node runs only its host-local checks
// (not even -force makes it run the others) on top of the mirrored state of
// the active peer.
func collect(ctx context.Context, state state, conf config, decision peerDecision) state {
	if !decision.Active {
		return collectPassive(ctx, state, conf, decision.Peer)
	}
	state = runChecks(ctx, state, conf)
	state.tagLocal(conf)
	state = mergePrometheusAlerts(ctx, state, conf)
	state = mergeFederated(ctx, state, conf)
	if conf.PeerURL == "" {
		return state
	}
	peer, err := fetchPeerSnapshot(ctx, conf.PeerURL)
	staleAfter := time.Duration(conf.PeerStaleThresholdS) * time.Second
	return mergePeer(state, peer, err, conf, time.Now(), staleAfter)
}

// collectPassive mirrors the active peer's state, keeps this node's own
// host-local results from its previous run (so their status changes are
// judged against this node's last result, not the peer's older copy) and
// runs the host-local checks.
func collectPassive(ctx context.Context, state state, conf config, peer peerSnapshot) state {
	log.Println("Running host-local checks only: peer is active")
	own := state.ownLocalChecks(conf)
	state = mirrorPeerState(state, peer)
	state.restoreOwnLocal(own, conf.hostname)
	state = runChecks(ctx, state, conf.localOnly())
	state.tagLocal(conf)
	return state
}

// gateNotification applies notification batching to the notify decision.
// state holds the checks this node notifies for (see ownedBy).
func gateNotification(doNotify, force bool, conf config, ns notifyState, state state) bool {
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

// persistReport atomically replaces StateDir/report.txt with the text report.
func persistReport(subject, body string, conf config) error {
	return writeFileAtomic(filepath.Join(conf.StateDir, "report.txt"), []byte(subject+"\n\n"+body), 0o644)
}
