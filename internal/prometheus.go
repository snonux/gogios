package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type prometheusResponse struct {
	Status string `json:"status"`
	Data   struct {
		Alerts []prometheusAlert `json:"alerts"`
	} `json:"data"`
}

type prometheusAlert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	State       string            `json:"state"`
}

func mergePrometheusAlerts(ctx context.Context, state state, conf config) state {
	if len(conf.PrometheusHosts) == 0 {
		return state
	}

	timeout := time.Duration(conf.PrometheusTimeoutS) * time.Second
	alerts, host, err := fetchPrometheusAlerts(ctx, conf.PrometheusHosts, timeout)
	if err != nil {
		log.Printf("Failed to fetch Prometheus alerts from any host: %v", err)
		checkName := "Prometheus alerts"
		newStatus := nagiosWarning
		if prevState, ok := state.checks[checkName]; ok && prevState.Status == newStatus {
			if prevState.PrevStatus != newStatus {
				prevState.PrevStatus = newStatus
				state.checks[checkName] = prevState
			}
			return state
		}
		cs := checkResult{
			name:   checkName,
			output: fmt.Sprintf("WARNING: %v", err),
			epoch:  time.Now().Unix(),
			status: newStatus,
		}
		state.update(cs)
		return state
	}

	log.Printf("Fetched %d firing alerts from Prometheus host %s", len(alerts), host)

	// Clear the "Prometheus alerts" check if fetch succeeded (was previously failing)
	checkName := "Prometheus alerts"
	if prevState, ok := state.checks[checkName]; ok && prevState.Status != nagiosOk {
		cs := checkResult{
			name:   checkName,
			output: "OK: Prometheus connection restored",
			epoch:  time.Now().Unix(),
			status: nagiosOk,
		}
		state.update(cs)
	}

	// Track currently firing alerts to clear resolved ones later
	firingAlerts := make(map[string]bool)
	watchdogFiring := false

	for _, alert := range alerts {
		alertname := alert.Labels["alertname"]

		// Special handling for Prometheus Watchdog alert
		if alertname == "Watchdog" {
			if alert.State == "firing" {
				watchdogFiring = true
				firingAlerts["Prometheus: Watchdog"] = true
				// Watchdog is firing as expected, treat as OK
				cs := checkResult{
					name:   "Prometheus: Watchdog",
					output: "OK [none]: Alertmanager is working properly",
					epoch:  time.Now().Unix(),
					status: nagiosOk,
				}
				state.update(cs)
			}
			continue
		}

		if alert.State != "firing" {
			continue
		}

		name := fmt.Sprintf("Prometheus: %s", alertname)
		firingAlerts[name] = true
		severity := alert.Labels["severity"]
		description := alert.Annotations["summary"]
		if description == "" {
			description = alert.Annotations["description"]
		}
		if description == "" {
			description = "no description"
		}

		status := nagiosWarning
		if severity == "critical" {
			status = nagiosCritical
		}

		cs := checkResult{
			name:   name,
			output: fmt.Sprintf("%s [%s]: %s", alertname, severity, description),
			epoch:  time.Now().Unix(),
			status: status,
		}
		state.update(cs)
	}

	// If Watchdog is not firing, alert as critical
	if !watchdogFiring {
		firingAlerts["Prometheus: Watchdog"] = true
		cs := checkResult{
			name:   "Prometheus: Watchdog",
			output: "CRITICAL [none]: Watchdog alert is not firing, Alertmanager may not be working",
			epoch:  time.Now().Unix(),
			status: nagiosCritical,
		}
		state.update(cs)
	}

	// Clear any Prometheus alerts that are no longer firing
	clearResolvedPrometheusAlerts(state, firingAlerts)

	return state
}

// clearResolvedPrometheusAlerts removes Prometheus alerts from state that are
// no longer firing. This prevents stale alerts from accumulating.
func clearResolvedPrometheusAlerts(state state, firingAlerts map[string]bool) {
	const prometheusPrefix = "Prometheus: "
	for name := range state.checks {
		// Skip non-Prometheus alerts and the connection status check
		if !strings.HasPrefix(name, prometheusPrefix) || name == "Prometheus alerts" {
			continue
		}
		// If this alert is not currently firing, remove it from state
		if !firingAlerts[name] {
			delete(state.checks, name)
			log.Printf("Cleared resolved Prometheus alert: %s", name)
		}
	}
}

func fetchPrometheusAlerts(ctx context.Context, hosts []string, timeout time.Duration) ([]prometheusAlert, string, error) {
	var lastErr error

	for _, host := range hosts {
		alerts, err := fetchFromHost(ctx, host, timeout)
		if err != nil {
			log.Printf("Failed to fetch from Prometheus host %s: %v", host, err)
			lastErr = err
			continue
		}
		return alerts, host, nil
	}

	return nil, "", fmt.Errorf("all Prometheus hosts failed, last error: %w", lastErr)
}

func fetchFromHost(ctx context.Context, host string, timeout time.Duration) ([]prometheusAlert, error) {
	url := fmt.Sprintf("http://%s/api/v1/alerts", host)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var promResp prometheusResponse
	if err := json.Unmarshal(body, &promResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if promResp.Status != "success" {
		return nil, fmt.Errorf("prometheus returned status: %s", promResp.Status)
	}

	return promResp.Data.Alerts, nil
}
