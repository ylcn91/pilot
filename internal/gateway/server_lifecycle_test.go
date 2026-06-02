package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStatusEndpoint(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	w := httptest.NewRecorder()

	server.handleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["version"] != "0.1.0" {
		t.Errorf("Expected version '0.1.0', got '%v'", response["version"])
	}
}

func TestServerStartStop(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 19090} // Use different port for test
	server := NewServer(config)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(ctx)
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Server should shutdown gracefully
	select {
	case err := <-errCh:
		if err != nil {
			t.Logf("Server returned: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Server did not shut down in time")
	}
}

func TestRouter(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	router := server.Router()
	if router == nil {
		t.Error("Router() returned nil")
	}
}

func TestHandleHealthTableDriven(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	tests := []struct {
		name           string
		method         string
		expectedStatus int
		checkBody      bool
	}{
		{
			name:           "GET request",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
			checkBody:      true,
		},
		{
			name:           "POST request",
			method:         http.MethodPost,
			expectedStatus: http.StatusOK,
			checkBody:      true,
		},
		{
			name:           "HEAD request",
			method:         http.MethodHead,
			expectedStatus: http.StatusOK,
			checkBody:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/health", nil)
			w := httptest.NewRecorder()

			server.handleHealth(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if w.Header().Get("Content-Type") != "application/json" {
				t.Error("Expected Content-Type application/json")
			}

			if tt.checkBody {
				var response map[string]string
				if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if response["status"] != "healthy" {
					t.Errorf("Expected status 'healthy', got '%s'", response["status"])
				}
			}
		})
	}
}

func TestHandleStatusTableDriven(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	tests := []struct {
		name           string
		method         string
		expectedStatus int
	}{
		{
			name:           "GET request",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST request",
			method:         http.MethodPost,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1/status", nil)
			w := httptest.NewRecorder()

			server.handleStatus(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if w.Header().Get("Content-Type") != "application/json" {
				t.Error("Expected Content-Type application/json")
			}

			var response map[string]interface{}
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if response["version"] != "0.1.0" {
				t.Errorf("Expected version '0.1.0', got '%v'", response["version"])
			}

			if _, ok := response["running"]; !ok {
				t.Error("Response should include 'running' field")
			}

			if _, ok := response["sessions"]; !ok {
				t.Error("Response should include 'sessions' field")
			}
		})
	}
}

func TestHandleTasksTableDriven(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	tests := []struct {
		name           string
		method         string
		expectedStatus int
	}{
		{
			name:           "GET request returns empty tasks",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST request",
			method:         http.MethodPost,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1/tasks", nil)
			w := httptest.NewRecorder()

			server.handleTasks(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if w.Header().Get("Content-Type") != "application/json" {
				t.Error("Expected Content-Type application/json")
			}

			var response map[string]interface{}
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			tasks, ok := response["tasks"]
			if !ok {
				t.Error("Response should include 'tasks' field")
			}

			taskArray, ok := tasks.([]interface{})
			if !ok {
				t.Error("Tasks should be an array")
			}

			if len(taskArray) != 0 {
				t.Errorf("Expected empty tasks array, got %d items", len(taskArray))
			}
		})
	}
}

func TestServerStartAlreadyRunning(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 19091}
	server := NewServer(config)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Start server in background
	go func() {
		_ = server.Start(ctx)
	}()

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Try to start again - should fail
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()

	err := server.Start(ctx2)
	if err == nil {
		t.Error("Expected error when starting already running server")
	}
	if err != nil && !strings.Contains(err.Error(), "already running") {
		t.Errorf("Expected 'already running' error, got: %v", err)
	}
}

func TestServerShutdownNotRunning(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	// Shutdown without starting - should be no-op
	err := server.Shutdown()
	if err != nil {
		t.Errorf("Shutdown on non-running server should not error: %v", err)
	}
}
