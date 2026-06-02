package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLinearWebhook(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	// Test invalid method
	req := httptest.NewRequest(http.MethodGet, "/webhooks/linear", nil)
	w := httptest.NewRecorder()
	server.handleLinearWebhook(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 for GET, got %d", w.Code)
	}

	// Test valid POST
	payload := `{"action": "create", "type": "Issue", "data": {}}`
	req = httptest.NewRequest(http.MethodPost, "/webhooks/linear", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.handleLinearWebhook(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for POST, got %d", w.Code)
	}

	// Test invalid JSON
	req = httptest.NewRequest(http.MethodPost, "/webhooks/linear", strings.NewReader("invalid"))
	w = httptest.NewRecorder()
	server.handleLinearWebhook(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid JSON, got %d", w.Code)
	}
}

// TestAsanaWebhookPreservesRawBody asserts the gateway buffers the exact request
// body into payload["_raw_body"] (TASK-333) for body-HMAC verification, and that
// JSON still decodes from the buffer.
func TestAsanaWebhookPreservesRawBody(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	var captured map[string]interface{}
	server.Router().RegisterWebhookHandler("asana", func(payload map[string]interface{}) {
		captured = payload
	})

	rawBody := `{ "events":  [ {"action":"changed"} ] }`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/asana", strings.NewReader(rawBody))
	req.Header.Set("X-Hook-Signature", "abc123")
	w := httptest.NewRecorder()

	server.handleAsanaWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if captured == nil {
		t.Fatal("webhook was not routed to the registered handler")
	}
	if got := captured["_raw_body"]; got != rawBody {
		t.Errorf("_raw_body = %q, want exact request body %q", got, rawBody)
	}
	if got := captured["_signature"]; got != "abc123" {
		t.Errorf("_signature = %q, want propagated header", got)
	}
	if _, ok := captured["events"]; !ok {
		t.Error("decoded payload missing 'events' — JSON did not decode from buffer")
	}
}

func TestLinearWebhookTableDriven(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	tests := []struct {
		name           string
		method         string
		payload        string
		expectedStatus int
	}{
		{
			name:           "GET method not allowed",
			method:         http.MethodGet,
			payload:        "",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "PUT method not allowed",
			method:         http.MethodPut,
			payload:        "",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "DELETE method not allowed",
			method:         http.MethodDelete,
			payload:        "",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "PATCH method not allowed",
			method:         http.MethodPatch,
			payload:        "",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "valid POST with issue create",
			method:         http.MethodPost,
			payload:        `{"action": "create", "type": "Issue", "data": {"id": "123"}}`,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid POST with issue update",
			method:         http.MethodPost,
			payload:        `{"action": "update", "type": "Issue", "data": {"id": "123"}}`,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid POST with comment create",
			method:         http.MethodPost,
			payload:        `{"action": "create", "type": "Comment", "data": {"body": "test"}}`,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid JSON",
			method:         http.MethodPost,
			payload:        "not json at all",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "empty payload",
			method:         http.MethodPost,
			payload:        "",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "malformed JSON",
			method:         http.MethodPost,
			payload:        `{"action": "create", "type":}`,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/webhooks/linear", strings.NewReader(tt.payload))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.handleLinearWebhook(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestAsanaWebhookTableDriven(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	tests := []struct {
		name           string
		method         string
		payload        string
		hookSecret     string
		signature      string
		expectedStatus int
		expectedHeader string
	}{
		{
			name:           "handshake returns X-Hook-Secret",
			method:         http.MethodPost,
			payload:        "",
			hookSecret:     "asana-webhook-secret-123",
			signature:      "",
			expectedStatus: http.StatusOK,
			expectedHeader: "asana-webhook-secret-123",
		},
		{
			name:           "GET method not allowed",
			method:         http.MethodGet,
			payload:        "",
			hookSecret:     "",
			signature:      "",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "PUT method not allowed",
			method:         http.MethodPut,
			payload:        "",
			hookSecret:     "",
			signature:      "",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "DELETE method not allowed",
			method:         http.MethodDelete,
			payload:        "",
			hookSecret:     "",
			signature:      "",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "valid POST with task event",
			method:         http.MethodPost,
			payload:        `{"events": [{"action": "added", "resource": {"gid": "123", "resource_type": "task"}}]}`,
			hookSecret:     "",
			signature:      "sha256=abc123",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid POST with task changed event",
			method:         http.MethodPost,
			payload:        `{"events": [{"action": "changed", "resource": {"gid": "456", "resource_type": "task"}}]}`,
			hookSecret:     "",
			signature:      "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid JSON",
			method:         http.MethodPost,
			payload:        "not valid json",
			hookSecret:     "",
			signature:      "",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "empty payload",
			method:         http.MethodPost,
			payload:        "",
			hookSecret:     "",
			signature:      "",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/webhooks/asana", strings.NewReader(tt.payload))
			if tt.hookSecret != "" {
				req.Header.Set("X-Hook-Secret", tt.hookSecret)
			}
			if tt.signature != "" {
				req.Header.Set("X-Hook-Signature", tt.signature)
			}
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.handleAsanaWebhook(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectedHeader != "" {
				if got := w.Header().Get("X-Hook-Secret"); got != tt.expectedHeader {
					t.Errorf("Expected X-Hook-Secret header %q, got %q", tt.expectedHeader, got)
				}
			}
		})
	}
}
