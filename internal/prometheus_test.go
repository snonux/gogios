package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
	var callCount atomic.Int32

	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		time.Sleep(3 * time.Second) // exceed timeout
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
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

	if !strings.Contains(watchdog.Output, "working properly") {
		t.Errorf("expected working properly message, got: %s", watchdog.Output)
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

	if !strings.Contains(watchdog.Output, "not firing") {
		t.Errorf("expected not firing message, got: %s", watchdog.Output)
	}
}

// unreachablePrometheusConf points at a closed listener, so every API query
// fails with a connection error.
func unreachablePrometheusConf(t *testing.T) config {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	host := strings.TrimPrefix(server.URL, "http://")
	server.Close()
	return config{PrometheusHosts: []string{host}, PrometheusTimeoutS: 1}
}

// A previously OK Watchdog must not stay OK when the API cannot be queried:
// that stale OK hid a Prometheus outage.
func TestMergePrometheusAlertsUnreachableWatchdogCritical(t *testing.T) {
	old := time.Now().Add(-2 * time.Hour).Unix()
	s := state{checks: map[string]checkState{
		prometheusWatchdogCheck: {Status: nagiosOk, PrevStatus: nagiosOk, Epoch: old, Output: "OK [none]: Alertmanager is working properly"},
		"Prometheus: HighCPU":   {Status: nagiosWarning, PrevStatus: nagiosWarning, Epoch: old, Output: "HighCPU"},
	}}

	result := mergePrometheusAlerts(context.Background(), s, unreachablePrometheusConf(t))

	watchdog := result.checks[prometheusWatchdogCheck]
	if watchdog.Status != nagiosCritical || watchdog.PrevStatus != nagiosOk {
		t.Fatalf("Watchdog = %v (prev %v), want CRITICAL (prev OK)", watchdog.Status, watchdog.PrevStatus)
	}
	if !strings.Contains(watchdog.Output, "unreachable") {
		t.Errorf("Watchdog output should name the unreachable API, got %q", watchdog.Output)
	}
	if watchdog.Epoch <= old {
		t.Errorf("Watchdog epoch not refreshed: %d", watchdog.Epoch)
	}
	if conn := result.checks[prometheusConnCheck]; conn.Status != nagiosWarning {
		t.Errorf("connection check = %v, want WARNING", conn.Status)
	}
	// Other alerts are neither cleared nor re-confirmed while blind.
	if cpu, ok := result.checks["Prometheus: HighCPU"]; !ok || cpu.Epoch != old {
		t.Errorf("HighCPU should keep its old state, got %+v (present %v)", cpu, ok)
	}
	if !result.hasCriticalChange(config{}) {
		t.Error("Watchdog OK->CRITICAL must count as a critical change (immediate notification)")
	}
}

// A lasting outage refreshes the epochs, so the checks stay unhandled rather
// than aging into the stale section, and does not re-report a change.
func TestMergePrometheusAlertsUnreachableTwiceStaysFresh(t *testing.T) {
	conf := unreachablePrometheusConf(t)
	old := time.Now().Add(-2 * time.Hour).Unix()
	s := state{checks: map[string]checkState{
		prometheusConnCheck:     {Status: nagiosWarning, PrevStatus: nagiosOk, Epoch: old},
		prometheusWatchdogCheck: {Status: nagiosCritical, PrevStatus: nagiosOk, Epoch: old},
	}}

	result := mergePrometheusAlerts(context.Background(), s, conf)

	for _, name := range []string{prometheusConnCheck, prometheusWatchdogCheck} {
		cs := result.checks[name]
		if cs.Epoch <= old {
			t.Errorf("%s epoch not refreshed", name)
		}
		if cs.changed() {
			t.Errorf("%s reported as changed on a repeated failure: %v -> %v", name, cs.PrevStatus, cs.Status)
		}
	}
}

// Recovery flips both checks back to OK.
func TestMergePrometheusAlertsRecovered(t *testing.T) {
	resp := prometheusResponse{Status: "success"}
	resp.Data.Alerts = []prometheusAlert{{Labels: map[string]string{"alertname": "Watchdog"}, State: "firing"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	s := state{checks: map[string]checkState{
		prometheusConnCheck:     {Status: nagiosWarning, PrevStatus: nagiosWarning},
		prometheusWatchdogCheck: {Status: nagiosCritical, PrevStatus: nagiosCritical},
	}}
	conf := config{PrometheusHosts: []string{strings.TrimPrefix(server.URL, "http://")}, PrometheusTimeoutS: 2}

	result := mergePrometheusAlerts(context.Background(), s, conf)

	if cs := result.checks[prometheusConnCheck]; cs.Status != nagiosOk || cs.PrevStatus != nagiosWarning {
		t.Errorf("connection check = %v (prev %v), want OK (prev WARNING)", cs.Status, cs.PrevStatus)
	}
	if cs := result.checks[prometheusWatchdogCheck]; cs.Status != nagiosOk || cs.PrevStatus != nagiosCritical {
		t.Errorf("Watchdog = %v (prev %v), want OK (prev CRITICAL)", cs.Status, cs.PrevStatus)
	}
}

// A pending (not yet firing) Watchdog does not count as firing.
func TestMergePrometheusAlertsWatchdogPending(t *testing.T) {
	resp := prometheusResponse{Status: "success"}
	resp.Data.Alerts = []prometheusAlert{{Labels: map[string]string{"alertname": "Watchdog"}, State: "pending"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	conf := config{PrometheusHosts: []string{strings.TrimPrefix(server.URL, "http://")}, PrometheusTimeoutS: 2}
	result := mergePrometheusAlerts(context.Background(), state{checks: map[string]checkState{}}, conf)

	if cs := result.checks[prometheusWatchdogCheck]; cs.Status != nagiosCritical {
		t.Errorf("pending Watchdog = %v, want CRITICAL", cs.Status)
	}
}
