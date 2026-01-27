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

	state = runChecks(ctx, state, conf)
	state = mergePrometheusAlerts(ctx, state, conf)
	state = mergeFederated(ctx, state, conf)

	if err := state.persist(); err != nil {
		notifyError(conf, err)
	}

	subject, body, doNotify := state.report(renotify, force, conf.StatusPageURL, conf)

	// Apply notification batching when MinNotifyIntervalS is configured.
	// Force flag bypasses batching to allow immediate notifications when needed.
	if doNotify && conf.MinNotifyIntervalS > 0 && !force {
		if notifyStateData.intervalElapsed(conf.MinNotifyIntervalS) {
			// Interval has elapsed - only notify if state changed since last notification
			if !notifyStateData.hasChanges(state) {
				doNotify = false
				log.Println("Notification suppressed: interval elapsed but no state changes since last notification")
			}
		} else {
			// Interval has not elapsed - suppress notification
			doNotify = false
			log.Println("Notification suppressed: minimum interval not elapsed")
		}
	}

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

	// Text and HTML reports always update regardless of notification batching
	if err := persistReport(subject, body, conf); err != nil {
		notifyError(conf, err)
	}

	// Generate HTML status page (unless disabled)
	if !conf.HTMLDisable {
		if err := persistHTMLReport(state, subject, conf); err != nil {
			notifyError(conf, err)
		}
	}

	return nil
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
