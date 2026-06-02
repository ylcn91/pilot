package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewServer(t *testing.T) {
	config := &Config{
		Host: "127.0.0.1",
		Port: 9090,
	}

	server := NewServer(config)

	if server == nil {
		t.Fatal("NewServer returned nil")
	}
	if server.config != config {
		t.Error("Server config not set correctly")
	}
	if server.sessions == nil {
		t.Error("Sessions manager not initialized")
	}
	if server.router == nil {
		t.Error("Router not initialized")
	}
	if server.liveness == nil {
		t.Error("Liveness state not initialized")
	}
	if server.readinessCheckers == nil {
		t.Error("Readiness checkers not initialized")
	}
}

func TestHealthEndpoint(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", response["status"])
	}
}

// mockReadinessChecker is a test double for ReadinessChecker
type mockReadinessChecker struct {
	name  string
	ready bool
}

func (m *mockReadinessChecker) Name() string { return m.name }
func (m *mockReadinessChecker) Ready() bool  { return m.ready }

func TestReadyEndpointNoCheckers(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	server.handleReady(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["ready"] != true {
		t.Errorf("Expected ready=true with no checkers, got %v", response["ready"])
	}
}

func TestReadyEndpointTableDriven(t *testing.T) {
	tests := []struct {
		name           string
		checkers       []ReadinessChecker
		expectedStatus int
		expectedReady  bool
	}{
		{
			name:           "no checkers returns ready",
			checkers:       nil,
			expectedStatus: http.StatusOK,
			expectedReady:  true,
		},
		{
			name: "all checkers pass",
			checkers: []ReadinessChecker{
				&mockReadinessChecker{name: "github", ready: true},
				&mockReadinessChecker{name: "database", ready: true},
			},
			expectedStatus: http.StatusOK,
			expectedReady:  true,
		},
		{
			name: "one checker fails",
			checkers: []ReadinessChecker{
				&mockReadinessChecker{name: "github", ready: true},
				&mockReadinessChecker{name: "database", ready: false},
			},
			expectedStatus: http.StatusServiceUnavailable,
			expectedReady:  false,
		},
		{
			name: "all checkers fail",
			checkers: []ReadinessChecker{
				&mockReadinessChecker{name: "github", ready: false},
				&mockReadinessChecker{name: "database", ready: false},
			},
			expectedStatus: http.StatusServiceUnavailable,
			expectedReady:  false,
		},
		{
			name: "single checker passes",
			checkers: []ReadinessChecker{
				&mockReadinessChecker{name: "poller", ready: true},
			},
			expectedStatus: http.StatusOK,
			expectedReady:  true,
		},
		{
			name: "single checker fails",
			checkers: []ReadinessChecker{
				&mockReadinessChecker{name: "poller", ready: false},
			},
			expectedStatus: http.StatusServiceUnavailable,
			expectedReady:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{Host: "127.0.0.1", Port: 9090}
			server := NewServer(config)

			for _, checker := range tt.checkers {
				server.RegisterReadinessChecker(checker)
			}

			req := httptest.NewRequest(http.MethodGet, "/ready", nil)
			w := httptest.NewRecorder()

			server.handleReady(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			var response map[string]interface{}
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if response["ready"] != tt.expectedReady {
				t.Errorf("Expected ready=%v, got %v", tt.expectedReady, response["ready"])
			}

			if w.Header().Get("Content-Type") != "application/json" {
				t.Error("Expected Content-Type application/json")
			}

			// Verify checks map exists
			if _, ok := response["checks"]; !ok {
				t.Error("Response should include 'checks' field")
			}
		})
	}
}

func TestLiveEndpointHealthy(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	req := httptest.NewRequest(http.MethodGet, "/live", nil)
	w := httptest.NewRecorder()

	server.handleLive(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["alive"] != true {
		t.Errorf("Expected alive=true, got %v", response["alive"])
	}

	checks, ok := response["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected checks to be a map")
	}

	// Verify goroutines check
	goroutines, ok := checks["goroutines"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected goroutines check")
	}
	if goroutines["ok"] != true {
		t.Error("Expected goroutines check to pass")
	}

	// Verify panics check
	panics, ok := checks["panics"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected panics check")
	}
	if panics["ok"] != true {
		t.Error("Expected panics check to pass")
	}

	// Verify heartbeat check
	heartbeat, ok := checks["heartbeat"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected heartbeat check")
	}
	if heartbeat["ok"] != true {
		t.Error("Expected heartbeat check to pass")
	}
}

func TestLiveEndpointAfterPanic(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	// Record a panic
	server.RecordPanic()

	req := httptest.NewRequest(http.MethodGet, "/live", nil)
	w := httptest.NewRecorder()

	server.handleLive(w, req)

	// Should return 503 after recent panic
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503 after panic, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["alive"] != false {
		t.Errorf("Expected alive=false after panic, got %v", response["alive"])
	}

	checks := response["checks"].(map[string]interface{})
	panics := checks["panics"].(map[string]interface{})
	if panics["ok"] != false {
		t.Error("Expected panics check to fail after recent panic")
	}
	if panics["count"].(float64) != 1 {
		t.Errorf("Expected panic count=1, got %v", panics["count"])
	}
}

func TestLiveEndpointStaleHeartbeat(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	// Set heartbeat to 120 seconds ago (older than 60s threshold)
	server.liveness.lastHeartbeat.Store(time.Now().Unix() - 120)

	req := httptest.NewRequest(http.MethodGet, "/live", nil)
	w := httptest.NewRecorder()

	server.handleLive(w, req)

	// Should return 503 with stale heartbeat
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503 with stale heartbeat, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["alive"] != false {
		t.Errorf("Expected alive=false with stale heartbeat, got %v", response["alive"])
	}
}

func TestHeartbeatMethod(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	// Set heartbeat to old value
	server.liveness.lastHeartbeat.Store(time.Now().Unix() - 120)

	// Call heartbeat
	server.Heartbeat()

	// Verify heartbeat is fresh
	age := time.Now().Unix() - server.liveness.lastHeartbeat.Load()
	if age > 2 {
		t.Errorf("Expected fresh heartbeat, got age=%d seconds", age)
	}
}

func TestRecordPanicMethod(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	// Initial state
	if server.liveness.panicCount.Load() != 0 {
		t.Error("Expected initial panic count of 0")
	}

	// Record multiple panics
	server.RecordPanic()
	server.RecordPanic()
	server.RecordPanic()

	if server.liveness.panicCount.Load() != 3 {
		t.Errorf("Expected panic count=3, got %d", server.liveness.panicCount.Load())
	}

	// Verify last panic time is recent
	age := time.Now().Unix() - server.liveness.lastPanicTime.Load()
	if age > 2 {
		t.Errorf("Expected recent last panic time, got age=%d seconds", age)
	}
}

func TestRegisterReadinessChecker(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	if len(server.readinessCheckers) != 0 {
		t.Error("Expected empty readiness checkers initially")
	}

	checker1 := &mockReadinessChecker{name: "checker1", ready: true}
	checker2 := &mockReadinessChecker{name: "checker2", ready: false}

	server.RegisterReadinessChecker(checker1)
	server.RegisterReadinessChecker(checker2)

	if len(server.readinessCheckers) != 2 {
		t.Errorf("Expected 2 readiness checkers, got %d", len(server.readinessCheckers))
	}
}
