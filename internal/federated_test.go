package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMergeFederated(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse map[string]checkState
		serverStatus   int
		expectedChecks []string
		expectError    bool
	}{
		{
			name: "successful federation",
			serverResponse: map[string]checkState{
				"Remote Check 1": {
					Status: nagiosOk,
					Epoch:  time.Now().Unix(),
				},
				"Remote Check 2": {
					Status: nagiosCritical,
					Epoch:  time.Now().Unix(),
				},
			},
			serverStatus:   200,
			expectedChecks: []string{"Remote Check 1", "Remote Check 2"},
			expectError:    false,
		},
		{
			name:           "server error",
			serverResponse: nil,
			serverStatus:   500,
			expectedChecks: []string{},
			expectError:    true,
		},
		{
			name:           "invalid json",
			serverResponse: nil,
			serverStatus:   200,
			expectedChecks: []string{},
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.serverStatus != 200 {
					w.WriteHeader(tt.serverStatus)
					return
				}

				if tt.serverResponse == nil && tt.name == "invalid json" {
					w.Write([]byte("invalid json"))
					return
				}

				if tt.serverResponse != nil {
					jsonData, _ := json.Marshal(tt.serverResponse)
					w.Header().Set("Content-Type", "application/json")
					w.Write(jsonData)
				}
			}))
			defer server.Close()

			// Create initial state
			state := state{
				checks:     make(map[string]checkState),
				staleEpoch: time.Now().Unix() - 3600,
			}

			// Create config with federated endpoint
			conf := config{
				Federated: []string{server.URL},
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Execute federation
			resultState := mergeFederated(ctx, state, conf)

			// Verify results
			if tt.expectError {
				// Should have a federated endpoint check that failed
				federatedCheckName := "Federated endpoint " + server.URL
				if check, exists := resultState.checks[federatedCheckName]; !exists {
					t.Errorf("Expected federated endpoint check to exist")
				} else if check.Status != nagiosCritical {
					t.Errorf("Expected federated endpoint check to be CRITICAL, got %v", check.Status)
				}
			} else {
				// Should have merged the remote checks
				for _, checkName := range tt.expectedChecks {
					if _, exists := resultState.checks[checkName]; !exists {
						t.Errorf("Expected check '%s' to be merged", checkName)
					}
				}

				// Should have successful federated endpoint check
				federatedCheckName := "Federated endpoint " + server.URL
				if check, exists := resultState.checks[federatedCheckName]; !exists {
					t.Errorf("Expected federated endpoint check to exist")
				} else if check.Status != nagiosOk {
					t.Errorf("Expected federated endpoint check to be OK, got %v", check.Status)
				}
			}
		})
	}
}

func TestMergeFederatedMultipleEndpoints(t *testing.T) {
	// Create two mock servers
	server1Response := map[string]checkState{
		"Server1 Check": {
			Status: nagiosOk,
			Epoch:  time.Now().Unix(),
		},
	}

	server2Response := map[string]checkState{
		"Server2 Check": {
			Status: nagiosWarning,
			Epoch:  time.Now().Unix(),
		},
	}

	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonData, _ := json.Marshal(server1Response)
		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonData)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonData, _ := json.Marshal(server2Response)
		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonData)
	}))
	defer server2.Close()

	// Create initial state
	state := state{
		checks:     make(map[string]checkState),
		staleEpoch: time.Now().Unix() - 3600,
	}

	// Create config with multiple federated endpoints
	conf := config{
		Federated: []string{server1.URL, server2.URL},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Execute federation
	resultState := mergeFederated(ctx, state, conf)

	// Verify both servers' checks were merged
	if _, exists := resultState.checks["Server1 Check"]; !exists {
		t.Errorf("Expected Server1 Check to be merged")
	}

	if _, exists := resultState.checks["Server2 Check"]; !exists {
		t.Errorf("Expected Server2 Check to be merged")
	}

	// Verify both federated endpoint checks exist and are OK
	federatedCheck1 := "Federated endpoint " + server1.URL
	federatedCheck2 := "Federated endpoint " + server2.URL

	if check, exists := resultState.checks[federatedCheck1]; !exists {
		t.Errorf("Expected federated endpoint check 1 to exist")
	} else if check.Status != nagiosOk {
		t.Errorf("Expected federated endpoint check 1 to be OK, got %v", check.Status)
	}

	if check, exists := resultState.checks[federatedCheck2]; !exists {
		t.Errorf("Expected federated endpoint check 2 to exist")
	} else if check.Status != nagiosOk {
		t.Errorf("Expected federated endpoint check 2 to be OK, got %v", check.Status)
	}
}

func TestMergeFederatedTimeout(t *testing.T) {
	// Create a server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(200)
	}))
	defer server.Close()

	// Create initial state
	state := state{
		checks:     make(map[string]checkState),
		staleEpoch: time.Now().Unix() - 3600,
	}

	// Create config with federated endpoint
	conf := config{
		Federated: []string{server.URL},
	}

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Execute federation
	resultState := mergeFederated(ctx, state, conf)

	// Should have a failed federated endpoint check
	federatedCheckName := "Federated endpoint " + server.URL
	if check, exists := resultState.checks[federatedCheckName]; !exists {
		t.Errorf("Expected federated endpoint check to exist")
	} else if check.Status != nagiosCritical {
		t.Errorf("Expected federated endpoint check to be CRITICAL due to timeout, got %v", check.Status)
	}
}

func TestMergeFederatedDuplicateCheckNames(t *testing.T) {
	// Create server with check name that conflicts with local state
	serverResponse := map[string]checkState{
		"Duplicate Check": {
			Status: nagiosOk,
			Epoch:  time.Now().Unix(),
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonData, _ := json.Marshal(serverResponse)
		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonData)
	}))
	defer server.Close()

	// Create initial state with existing check
	state := state{
		checks: map[string]checkState{
			"Duplicate Check": {
				Status: nagiosCritical,
				Epoch:  time.Now().Unix(),
			},
		},
		staleEpoch: time.Now().Unix() - 3600,
	}

	// Create config with federated endpoint
	conf := config{
		Federated: []string{server.URL},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Execute federation
	resultState := mergeFederated(ctx, state, conf)

	// Should have a failed federated endpoint check due to duplicate name
	federatedCheckName := "Federated endpoint " + server.URL
	if check, exists := resultState.checks[federatedCheckName]; !exists {
		t.Errorf("Expected federated endpoint check to exist")
	} else if check.Status != nagiosCritical {
		t.Errorf("Expected federated endpoint check to be CRITICAL due to duplicate name, got %v", check.Status)
	}

	// Original check should remain unchanged
	if check, exists := resultState.checks["Duplicate Check"]; !exists {
		t.Errorf("Expected original check to remain")
	} else if check.Status != nagiosCritical {
		t.Errorf("Expected original check status to remain CRITICAL, got %v", check.Status)
	}
}