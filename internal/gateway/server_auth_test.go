package gateway

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewServerWithAuth(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	authConfig := &AuthConfig{
		Type:  AuthTypeAPIToken,
		Token: "test-token",
	}

	server := NewServerWithAuth(config, authConfig)

	if server == nil {
		t.Fatal("NewServerWithAuth returned nil")
	}
	if server.authn.auth == nil {
		t.Error("Server auth not initialized")
	}
	if server.config != config {
		t.Error("Server config not set correctly")
	}
}

func TestNewServerWithAuth_NilAuth(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}

	server := NewServerWithAuth(config, nil)

	if server == nil {
		t.Fatal("NewServerWithAuth returned nil")
	}
	if server.authn.auth != nil {
		t.Error("Server auth should be nil when authConfig is nil")
	}
}

func TestAPIEndpointsRequireAuth(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 19092}
	authConfig := &AuthConfig{
		Type:  AuthTypeAPIToken,
		Token: "secret-api-token",
	}
	server := NewServerWithAuth(config, authConfig)

	// Start server
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		_ = server.Start(ctx)
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Test endpoints
	tests := []struct {
		name           string
		endpoint       string
		authHeader     string
		expectedStatus int
	}{
		{
			name:           "status without auth returns 401",
			endpoint:       "/api/v1/status",
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "status with valid auth returns 200",
			endpoint:       "/api/v1/status",
			authHeader:     "Bearer secret-api-token",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "tasks without auth returns 401",
			endpoint:       "/api/v1/tasks",
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "tasks with valid auth returns 200",
			endpoint:       "/api/v1/tasks",
			authHeader:     "Bearer secret-api-token",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "health is public (no auth required)",
			endpoint:       "/health",
			authHeader:     "",
			expectedStatus: http.StatusOK,
		},
	}

	client := &http.Client{Timeout: 5 * time.Second}
	baseURL := "http://127.0.0.1:19092"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, baseURL+tt.endpoint, nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Status = %d, want %d", resp.StatusCode, tt.expectedStatus)
			}
		})
	}
}

func TestWebhooksDoNotRequireBearerAuth(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 19093}
	authConfig := &AuthConfig{
		Type:  AuthTypeAPIToken,
		Token: "secret-api-token",
	}
	server := NewServerWithAuth(config, authConfig)

	// Start server
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		_ = server.Start(ctx)
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Webhooks should NOT require bearer auth (they use signature validation instead)
	webhooks := []string{
		"/webhooks/linear",
		"/webhooks/github",
		"/webhooks/jira",
		"/webhooks/asana",
	}

	client := &http.Client{Timeout: 5 * time.Second}
	baseURL := "http://127.0.0.1:19093"

	for _, endpoint := range webhooks {
		t.Run(endpoint, func(t *testing.T) {
			// Send valid JSON payload without bearer token
			payload := `{"action": "test", "type": "test"}`
			req, err := http.NewRequest(http.MethodPost, baseURL+endpoint, strings.NewReader(payload))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			// Should return 200 OK, not 401 Unauthorized
			if resp.StatusCode == http.StatusUnauthorized {
				t.Errorf("Webhook %s should not require bearer auth", endpoint)
			}
		})
	}
}
