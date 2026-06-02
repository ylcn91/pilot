package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockAutopilotProvider is a test double for AutopilotProvider.
type mockAutopilotProvider struct {
	environment  string
	activePRs    []*AutopilotPRState
	failureCount int
	autoRelease  bool
}

func (m *mockAutopilotProvider) GetEnvironment() string            { return m.environment }
func (m *mockAutopilotProvider) GetActivePRs() []*AutopilotPRState { return m.activePRs }
func (m *mockAutopilotProvider) GetFailureCount() int              { return m.failureCount }
func (m *mockAutopilotProvider) IsAutoReleaseEnabled() bool        { return m.autoRelease }

func TestHandleAutopilotNoProvider(t *testing.T) {
	server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/autopilot", nil)
	w := httptest.NewRecorder()

	server.handleAutopilot(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["enabled"] != false {
		t.Errorf("Expected enabled=false without provider, got %v", response["enabled"])
	}
	if response["environment"] != "" {
		t.Errorf("Expected empty environment, got %v", response["environment"])
	}
	prs, ok := response["activePRs"].([]interface{})
	if !ok {
		t.Fatal("activePRs should be an array")
	}
	if len(prs) != 0 {
		t.Errorf("Expected empty activePRs, got %d", len(prs))
	}
}

func TestHandleAutopilotWithProvider(t *testing.T) {
	server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})
	server.SetAutopilotProvider(&mockAutopilotProvider{
		environment:  "stage",
		failureCount: 2,
		autoRelease:  true,
		activePRs: []*AutopilotPRState{
			{
				PRNumber:   42,
				PRURL:      "https://github.com/test/repo/pull/42",
				Stage:      "waiting_ci",
				CIStatus:   "pending",
				BranchName: "pilot/GH-100",
			},
			{
				PRNumber:   43,
				PRURL:      "https://github.com/test/repo/pull/43",
				Stage:      "merging",
				CIStatus:   "success",
				Error:      "",
				BranchName: "pilot/GH-101",
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/autopilot", nil)
	w := httptest.NewRecorder()

	server.handleAutopilot(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Error("Expected Content-Type application/json")
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["enabled"] != true {
		t.Errorf("Expected enabled=true, got %v", response["enabled"])
	}
	if response["environment"] != "stage" {
		t.Errorf("Expected environment=stage, got %v", response["environment"])
	}
	if response["autoRelease"] != true {
		t.Errorf("Expected autoRelease=true, got %v", response["autoRelease"])
	}
	if response["failureCount"] != float64(2) {
		t.Errorf("Expected failureCount=2, got %v", response["failureCount"])
	}

	prs, ok := response["activePRs"].([]interface{})
	if !ok {
		t.Fatal("activePRs should be an array")
	}
	if len(prs) != 2 {
		t.Fatalf("Expected 2 active PRs, got %d", len(prs))
	}

	pr0 := prs[0].(map[string]interface{})
	if pr0["number"] != float64(42) {
		t.Errorf("Expected PR number=42, got %v", pr0["number"])
	}
	if pr0["stage"] != "waiting_ci" {
		t.Errorf("Expected stage=waiting_ci, got %v", pr0["stage"])
	}
	if pr0["branchName"] != "pilot/GH-100" {
		t.Errorf("Expected branchName=pilot/GH-100, got %v", pr0["branchName"])
	}
}

func TestHandleAutopilotEmptyPRs(t *testing.T) {
	server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})
	server.SetAutopilotProvider(&mockAutopilotProvider{
		environment:  "dev",
		failureCount: 0,
		autoRelease:  false,
		activePRs:    []*AutopilotPRState{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/autopilot", nil)
	w := httptest.NewRecorder()

	server.handleAutopilot(w, req)

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["enabled"] != true {
		t.Errorf("Expected enabled=true, got %v", response["enabled"])
	}
	prs := response["activePRs"].([]interface{})
	if len(prs) != 0 {
		t.Errorf("Expected 0 active PRs, got %d", len(prs))
	}
}

func TestSetAutopilotProvider(t *testing.T) {
	server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})

	if server.autopilotProvider != nil {
		t.Error("Expected nil autopilot provider initially")
	}

	provider := &mockAutopilotProvider{environment: "prod"}
	server.SetAutopilotProvider(provider)

	if server.autopilotProvider == nil {
		t.Error("Expected autopilot provider to be set")
	}
}
