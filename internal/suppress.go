package internal

import (
	"log"
	"os"
	"strings"
	"time"
)

// isSuppressed checks if alerts should be suppressed based on a file's existence and age.
// Returns true if the file exists AND its modification time is within maxAgeS seconds of now.
// Returns false if filePath is empty, file doesn't exist, or file is too old.
func isSuppressed(filePath string, maxAgeS int) bool {
	if filePath == "" {
		return false
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return false // file doesn't exist or other error
	}
	age := time.Since(info.ModTime())
	return age <= time.Duration(maxAgeS)*time.Second
}

// isCheckSuppressed determines if a check should be suppressed from email reports.
// For Prometheus checks (name starts with "Prometheus"): uses PrometheusOnlyIfNotExists config.
// For regular checks: uses per-check OnlyIfNotExists config if set.
func isCheckSuppressed(name string, conf config) bool {
	// Check if this is a Prometheus alert (name starts with "Prometheus")
	if strings.HasPrefix(name, "Prometheus") {
		if isSuppressed(conf.PrometheusOnlyIfNotExists, conf.PrometheusOnlyIfNotExistsMaxS) {
			log.Printf("Suppressing %s: file %s exists and is recent", name, conf.PrometheusOnlyIfNotExists)
			return true
		}
		return false
	}

	// For regular checks, look up the check config
	chk, ok := conf.Checks[name]
	if !ok {
		return false // check not found in config (e.g., federated)
	}

	if chk.OnlyIfNotExists == "" {
		return false
	}

	// Use per-check max age if set, otherwise use global Prometheus default
	maxAgeS := chk.OnlyIfNotExistsMaxS
	if maxAgeS == 0 {
		maxAgeS = conf.PrometheusOnlyIfNotExistsMaxS
	}

	if isSuppressed(chk.OnlyIfNotExists, maxAgeS) {
		log.Printf("Suppressing %s: file %s exists and is recent", name, chk.OnlyIfNotExists)
		return true
	}
	return false
}
