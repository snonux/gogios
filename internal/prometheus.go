package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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
		cs := checkResult{
			name:   "Prometheus alerts",
			output: fmt.Sprintf("CRITICAL: %v", err),
			epoch:  time.Now().Unix(),
			status: nagiosCritical,
		}
		state.update(cs)
		return state
	}

	log.Printf("Fetched %d firing alerts from Prometheus host %s", len(alerts), host)

	for _, alert := range alerts {
		if alert.State != "firing" {
			continue
		}

		name := fmt.Sprintf("Prometheus: %s", alert.Labels["alertname"])
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
			output: fmt.Sprintf("%s [%s]: %s", alert.Labels["alertname"], severity, description),
			epoch:  time.Now().Unix(),
			status: status,
		}
		state.update(cs)
	}

	return state
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
