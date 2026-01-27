package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
)

type config struct {
	EmailTo            string
	EmailFrom          string
	SMTPServer         string `json:"SMTPServer,omitempty"`
	SMTPDisable        bool   `json:"SMTPDisable,omitempty"` // TODO: Document this option
	StateDir           string `json:"StateDir,omitempty"`
	HTMLStatusFile     string `json:"HTMLStatusFile,omitempty"` // Path to HTML status file
	HTMLDisable        bool   `json:"HTMLDisable,omitempty"`    // Disable HTML status page generation
	StatusPageURL      string `json:"StatusPageURL,omitempty"`  // URL to the HTML status page for email notifications
	CheckTimeoutS      int
	CheckConcurrency   int
	StaleThreshold     int      `json:"StaleThreshold,omitempty"`
	Federated          []string `json:"Federated,omitempty"` // TODO: Document this option
	// MinNotifyIntervalS is the minimum interval in seconds between email notifications.
	// When set > 0, Gogios batches notifications and only sends an email when:
	// 1. The interval has elapsed since the last notification, AND
	// 2. There's been a state change since the last notification.
	// Set to 0 (default) for immediate notifications on every state change.
	MinNotifyIntervalS            int      `json:"MinNotifyIntervalS,omitempty"`
	PrometheusHosts               []string `json:"PrometheusHosts,omitempty"`
	PrometheusTimeoutS            int      `json:"PrometheusTimeoutS,omitempty"`
	PrometheusOnlyIfNotExists     string   `json:"PrometheusOnlyIfNotExists,omitempty"`     // Suppress Prometheus alerts if this file exists and is recent
	PrometheusOnlyIfNotExistsMaxS int      `json:"PrometheusOnlyIfNotExistsMaxS,omitempty"` // Max age in seconds for suppression file (default 86400)
	Checks                        map[string]check
}

func newConfig(configFile string) (config, error) {
	var conf config

	file, err := os.Open(configFile)
	if err != nil {
		return conf, err
	}
	defer file.Close()

	bytes, err := io.ReadAll(file)
	if err != nil {
		return conf, err
	}

	err = json.Unmarshal(bytes, &conf)
	if err != nil {
		return conf, err
	}

	if conf.SMTPServer == "" {
		hostname, err := os.Hostname()
		if err != nil {
			log.Fatal(err)
		}
		conf.SMTPServer = fmt.Sprintf("%s:25", hostname)
		log.Println("Set SMTPServer to " + conf.SMTPServer)
	}

	if conf.StateDir == "" {
		conf.StateDir = "."
		log.Println("Set StateDir to " + conf.StateDir)
	}

	if conf.StaleThreshold == 0 {
		conf.StaleThreshold = 3600 // Default to 1 hour
	}

	if conf.PrometheusTimeoutS == 0 {
		conf.PrometheusTimeoutS = 2 // Default to 2 seconds
	}

	if conf.PrometheusOnlyIfNotExistsMaxS == 0 {
		conf.PrometheusOnlyIfNotExistsMaxS = 86400 // Default to 24 hours
	}

	if !conf.HTMLDisable && conf.HTMLStatusFile == "" {
		conf.HTMLStatusFile = "/var/www/htdocs/buetow.org/self/gogios/index.html"
		log.Println("Set HTMLStatusFile to " + conf.HTMLStatusFile)
	}

	// Default URL for the status page link in email notifications
	if conf.StatusPageURL == "" {
		conf.StatusPageURL = "https://gogios.buetow.org"
		log.Println("Set StatusPageURL to " + conf.StatusPageURL)
	}

	return conf, nil
}

func (conf config) sanityCheck() error {
	for name, check := range conf.Checks {
		for _, depName := range check.DependsOn {
			if _, ok := conf.Checks[depName]; !ok {
				return fmt.Errorf("check '%s' depends on non existant check '%s'", name, depName)
			}
		}
	}
	return nil
}
