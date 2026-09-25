package internal

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Bounds that keep one run from holding the run lock indefinitely, and make a
// lock that is held anyway visible instead of silently skipping every run.
const (
	// runGrace is the time a run gets after its Timeout to persist, mail
	// and publish before the watchdog ends the process.
	runGrace = 2 * time.Minute
	// lockStaleMargin is added to the longest healthy run (Timeout +
	// runGrace) before a held lock counts as stuck.
	lockStaleMargin = 5 * time.Minute
	// lockAlertInterval rate-limits the stuck-lock mail, which every
	// skipped run (each cron interval) would otherwise send.
	lockAlertInterval = time.Hour
	// lockAlertStamp is the file in StateDir whose mtime records the last
	// stuck-lock mail.
	lockAlertStamp = "lockalert.stamp"
)

// startWatchdog calls onExpire once limit has passed, unless the returned
// stop function was called first. Run uses it to end the process (the kernel
// then releases the flock) when a hang that ignores the run's context, e.g.
// a stuck write, would otherwise keep it alive.
func startWatchdog(limit time.Duration, onExpire func()) (stop func()) {
	t := time.AfterFunc(limit, onExpire)
	return func() { t.Stop() }
}

// handleLockedRun deals with a plain run that found the lock taken. Normally
// another run is just busy, and skipping is right. When the lock has been
// held for longer than staleAfter, the holder is stuck and this node is not
// monitoring anything: that is reported on stderr (the returned error), and
// by mail through send at most once per lockAlertInterval.
func handleLockedRun(conf config, now time.Time, staleAfter time.Duration, send func(subject, body string) error) error {
	pid, since, err := runLockHolder(conf.StateDir)
	if err != nil {
		log.Printf("Skipping run: %v (%v)", errRunLocked, err)
		return nil
	}
	held := now.Sub(since).Truncate(time.Second)
	if held <= staleAfter {
		log.Printf("Skipping run: %v (PID %d, held for %v)", errRunLocked, pid, held)
		return nil
	}

	stuck := fmt.Errorf("run lock in %s held by PID %d since %s (%v > %v): every run is skipped, %s is not monitoring",
		conf.StateDir, pid, since.Format(time.RFC3339), held, staleAfter, conf.hostname)
	if lockAlertDue(conf.StateDir, now) {
		subject := fmt.Sprintf("GOGIOS: runs on %s stuck behind the run lock", conf.hostname)
		body := stuck.Error() + "\n\nKill the stuck process (or remove it) to resume monitoring.\n"
		if err := send(subject, body); err != nil {
			log.Println("error: stuck-lock mail:", err)
		} else {
			markLockAlert(conf.StateDir, now)
		}
	}
	return stuck
}

// lockAlertDue reports whether the last stuck-lock mail is older than
// lockAlertInterval (or there was none).
func lockAlertDue(stateDir string, now time.Time) bool {
	info, err := os.Stat(filepath.Join(stateDir, lockAlertStamp))
	return err != nil || now.Sub(info.ModTime()) >= lockAlertInterval
}

// markLockAlert records now as the time of the last stuck-lock mail.
func markLockAlert(stateDir string, now time.Time) {
	path := filepath.Join(stateDir, lockAlertStamp)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		log.Println("warning: stuck-lock stamp:", err)
		return
	}
	if err := os.Chtimes(path, now, now); err != nil {
		log.Println("warning: stuck-lock stamp:", err)
	}
}

// removeStaleTemps deletes the temporary files writeFileAtomic leaves behind
// when a run dies between creating and renaming them (e.g. ended by the
// watchdog). Call it only while holding the run lock: no other run can be
// writing them then.
func removeStaleTemps(conf config) {
	targets := []string{
		filepath.Join(conf.StateDir, "state.json"),
		filepath.Join(conf.StateDir, "report.txt"),
		filepath.Join(conf.StateDir, notifyStateFile),
	}
	if conf.HTMLStatusFile != "" {
		targets = append(targets, conf.HTMLStatusFile, jsonReportPath(conf.HTMLStatusFile))
	}
	for _, target := range targets {
		matches, err := filepath.Glob(target + ".*.tmp")
		if err != nil {
			continue // only a malformed pattern errors
		}
		for _, tmp := range matches {
			if err := os.Remove(tmp); err != nil {
				log.Println("warning: remove stale temp file:", err)
			} else {
				log.Println("Removed stale temp file", tmp)
			}
		}
	}
}
