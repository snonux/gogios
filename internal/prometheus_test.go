package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchFromHost(t *testing.T) {
	tests := []struct {
		name           string
		response       prometheusResponse
		statusCode     int
		wantAlertCount int
		wantErr        bool
	}{
		{
			name: "successful response with firing alerts",
			response: prometheusResponse{
				Status: "success",
				Data: struct {
					Alerts []prometheusAlert `json:"alerts"`
				}{
					Alerts: []prometheusAlert{
						{
							Labels:      map[string]string{"alertname": "HighCPU", "severity": "critical"},
							Annotations: map[string]string{"summary": "CPU usage is high"},
							State:       "firing",
						},
						{
							Labels:      map[string]string{"alertname": "DiskSpace", "severity": "warning"},
							Annotations: map[string]string{"summary": "Disk space low"},
							State:       "firing",
						},
					},
				},
			},
			statusCode:     http.StatusOK,
			wantAlertCount: 2,
			wantErr:        false,
		},
		{
			name: "empty alerts",
			response: prometheusResponse{
				Status: "success",
				Data: struct {
					Alerts []prometheusAlert `json:"alerts"`
				}{
					Alerts: []prometheusAlert{},
				},
			},
			statusCode:     http.StatusOK,
			wantAlertCount: 0,
			wantErr:        false,
		},
		{
			name:           "server error",
			response:       prometheusResponse{},
			statusCode:     http.StatusInternalServerError,
			wantAlertCount: 0,
			wantErr:        true,
		},
		{
			name: "prometheus error status",
			response: prometheusResponse{
				Status: "error",
			},
			statusCode:     http.StatusOK,
			wantAlertCount: 0,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/alerts" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			host := strings.TrimPrefix(server.URL, "http://")
			alerts, err := fetchFromHost(context.Background(), host, 2*time.Second)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(alerts) != tt.wantAlertCount {
				t.Errorf("got %d alerts, want %d", len(alerts), tt.wantAlertCount)
			}
		})
	}
}

func TestFetchPrometheusAlertsFailover(t *testing.T) {
	callCount := 0

	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		time.Sleep(3 * time.Second) // exceed timeout
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := prometheusResponse{
			Status: "success",
			Data: struct {
				Alerts []prometheusAlert `json:"alerts"`
			}{
				Alerts: []prometheusAlert{
					{
						Labels: map[string]string{"alertname": "Test"},
						State:  "firing",
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server2.Close()

	hosts := []string{
		strings.TrimPrefix(server1.URL, "http://"),
		strings.TrimPrefix(server2.URL, "http://"),
	}

	alerts, host, err := fetchPrometheusAlerts(context.Background(), hosts, 2*time.Second)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(alerts) != 1 {
		t.Errorf("got %d alerts, want 1", len(alerts))
	}
	if host != hosts[1] {
		t.Errorf("got host %s, want %s", host, hosts[1])
	}
}

func TestMergePrometheusAlertsNoHosts(t *testing.T) {
	conf := config{PrometheusHosts: nil}
	s := state{checks: make(map[string]checkState)}

	result := mergePrometheusAlerts(context.Background(), s, conf)

	if len(result.checks) != 0 {
		t.Errorf("expected no checks, got %d", len(result.checks))
	}
}

func TestMergePrometheusAlertsWatchdogFiring(t *testing.T) {
	resp := prometheusResponse{
		Status: "success",
		Data: struct {
			Alerts []prometheusAlert `json:"alerts"`
		}{
			Alerts: []prometheusAlert{
				{
					Labels:      map[string]string{"alertname": "Watchdog", "severity": "none"},
					Annotations: map[string]string{"summary": "An alert that should always be firing to certify that Alertmanager is working properly."},
					State:       "firing",
				},
				{
					Labels:      map[string]string{"alertname": "HighCPU", "severity": "critical"},
					Annotations: map[string]string{"summary": "CPU usage is high"},
					State:       "firing",
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	conf := config{
		PrometheusHosts:    []string{strings.TrimPrefix(server.URL, "http://")},
		PrometheusTimeoutS: 2,
	}
	s := state{checks: make(map[string]checkState)}

	result := mergePrometheusAlerts(context.Background(), s, conf)

	watchdog, ok := result.checks["Prometheus: Watchdog"]
	if !ok {
		t.Fatal("Watchdog check not found in state")
	}

	if watchdog.Status != nagiosOk {
		t.Errorf("expected Watchdog status OK, got %v", watchdog.Status)
	}

	if !strings.Contains(watchdog.output, "working properly") {
		t.Errorf("expected working properly message, got: %s", watchdog.output)
	}

	// Verify other alerts are still processed
	cpu, ok := result.checks["Prometheus: HighCPU"]
	if !ok {
		t.Fatal("HighCPU check not found in state")
	}
	if cpu.Status != nagiosCritical {
		t.Errorf("expected HighCPU status CRITICAL, got %v", cpu.Status)
	}
}

func TestMergePrometheusAlertsWatchdogNotFiring(t *testing.T) {
	resp := prometheusResponse{
		Status: "success",
		Data: struct {
			Alerts []prometheusAlert `json:"alerts"`
		}{
			Alerts: []prometheusAlert{
				{
					Labels:      map[string]string{"alertname": "HighCPU", "severity": "critical"},
					Annotations: map[string]string{"summary": "CPU usage is high"},
					State:       "firing",
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	conf := config{
		PrometheusHosts:    []string{strings.TrimPrefix(server.URL, "http://")},
		PrometheusTimeoutS: 2,
	}
	s := state{checks: make(map[string]checkState)}

	result := mergePrometheusAlerts(context.Background(), s, conf)

	watchdog, ok := result.checks["Prometheus: Watchdog"]
	if !ok {
		t.Fatal("Watchdog check not found in state")
	}

	if watchdog.Status != nagiosCritical {
		t.Errorf("expected Watchdog status CRITICAL, got %v", watchdog.Status)
	}

	if !strings.Contains(watchdog.output, "not firing") {
		t.Errorf("expected not firing message, got: %s", watchdog.output)
	}
}
