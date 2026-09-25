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

const (
	// prometheusConnCheck reports whether the Prometheus API answered at all.
	prometheusConnCheck = "Prometheus alerts"
	// prometheusWatchdogCheck is the dead man's switch: OK only while the
	// always-firing Watchdog alert is seen through the Prometheus API.
	prometheusWatchdogCheck = "Prometheus: Watchdog"
	prometheusPrefix        = "Prometheus: "
)

// mergePrometheusAlerts folds the firing Prometheus alerts into state. Every
// run refreshes the epoch of the connection and Watchdog checks, so neither
// can age into the stale section while the outcome stays the same.
func mergePrometheusAlerts(ctx context.Context, state state, conf config) state {
	if len(conf.PrometheusHosts) == 0 {
		return state
	}

	timeout := time.Duration(conf.PrometheusTimeoutS) * time.Second
	alerts, host, err := fetchPrometheusAlerts(ctx, conf.PrometheusHosts, timeout)
	now := time.Now().Unix()
	if err != nil {
		log.Printf("Failed to fetch Prometheus alerts from any host: %v", err)
		markPrometheusUnreachable(state, err, now)
		return state
	}

	log.Printf("Fetched %d firing alerts from Prometheus host %s", len(alerts), host)
	markPrometheusReachable(state, now)
	firingAlerts := mergeFiringAlerts(state, alerts, now)
	clearResolvedPrometheusAlerts(state, firingAlerts)
	return state
}

// markPrometheusUnreachable records a failed Prometheus API query. The
// connection check goes WARNING, and the Watchdog goes CRITICAL: an
// unreachable API is exactly the case the dead man's switch exists for, and
// keeping the last OK here once reported a healthy Watchdog while Prometheus
// was down. Other Prometheus alerts keep their last known state and epoch, so
// they age into the stale section instead of being cleared or re-confirmed.
func markPrometheusUnreachable(state state, err error, now int64) {
	state.update(checkResult{
		name:   prometheusConnCheck,
		output: fmt.Sprintf("WARNING: %v", err),
		epoch:  now,
		status: nagiosWarning,
	})
	state.update(checkResult{
		name:   prometheusWatchdogCheck,
		output: fmt.Sprintf("CRITICAL [none]: Prometheus API unreachable, Watchdog state unknown: %v", err),
		epoch:  now,
		status: nagiosCritical,
	})
}

// markPrometheusReachable records a successful Prometheus API query.
func markPrometheusReachable(state state, now int64) {
	state.update(checkResult{
		name:   prometheusConnCheck,
		output: "OK: Prometheus API reachable",
		epoch:  now,
		status: nagiosOk,
	})
}

// mergeFiringAlerts turns every firing alert into a check and returns the
// set of check names that are firing. The Watchdog is inverted: firing is OK,
// absent is CRITICAL (Alertmanager's pipeline is broken).
func mergeFiringAlerts(state state, alerts []prometheusAlert, now int64) map[string]bool {
	firingAlerts := map[string]bool{prometheusWatchdogCheck: true}
	watchdog := checkResult{
		name:   prometheusWatchdogCheck,
		output: "CRITICAL [none]: Watchdog alert is not firing, Alertmanager may not be working",
		epoch:  now,
		status: nagiosCritical,
	}

	for _, alert := range alerts {
		if alert.State != "firing" {
			continue
		}
		alertname := alert.Labels["alertname"]
		if alertname == "Watchdog" {
			watchdog.output = "OK [none]: Alertmanager is working properly"
			watchdog.status = nagiosOk
			continue
		}
		cs := alertCheckResult(alert, now)
		firingAlerts[cs.name] = true
		state.update(cs)
	}

	state.update(watchdog)
	return firingAlerts
}

// alertCheckResult maps one firing, non-Watchdog alert to a check result:
// severity "critical" is CRITICAL, anything else WARNING.
func alertCheckResult(alert prometheusAlert, now int64) checkResult {
	alertname := alert.Labels["alertname"]
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

	return checkResult{
		name:   prometheusPrefix + alertname,
		output: fmt.Sprintf("%s [%s]: %s", alertname, severity, description),
		epoch:  now,
		status: status,
	}
}

// clearResolvedPrometheusAlerts removes Prometheus alerts from state that are
// no longer firing. This prevents stale alerts from accumulating.
func clearResolvedPrometheusAlerts(state state, firingAlerts map[string]bool) {
	for name := range state.checks {
		// Skip non-Prometheus alerts and the connection status check
		if !strings.HasPrefix(name, prometheusPrefix) || name == prometheusConnCheck {
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
