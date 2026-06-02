package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGithubWebhookTableDriven(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	tests := []struct {
		name           string
		method         string
		payload        string
		eventType      string
		signature      string
		expectedStatus int
	}{
		{
			name:           "GET method not allowed",
			method:         http.MethodGet,
			payload:        "",
			eventType:      "",
			signature:      "",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "PUT method not allowed",
			method:         http.MethodPut,
			payload:        "",
			eventType:      "",
			signature:      "",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "DELETE method not allowed",
			method:         http.MethodDelete,
			payload:        "",
			eventType:      "",
			signature:      "",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "valid POST with issue event",
			method:         http.MethodPost,
			payload:        `{"action": "opened", "issue": {"number": 1}}`,
			eventType:      "issues",
			signature:      "sha256=abc123",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid POST with push event",
			method:         http.MethodPost,
			payload:        `{"ref": "refs/heads/main", "commits": []}`,
			eventType:      "push",
			signature:      "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid POST with pull_request event",
			method:         http.MethodPost,
			payload:        `{"action": "opened", "pull_request": {"number": 42}}`,
			eventType:      "pull_request",
			signature:      "sha256=def456",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid JSON",
			method:         http.MethodPost,
			payload:        "not valid json",
			eventType:      "issues",
			signature:      "",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "empty payload",
			method:         http.MethodPost,
			payload:        "",
			eventType:      "ping",
			signature:      "",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/webhooks/github", strings.NewReader(tt.payload))
			if tt.eventType != "" {
				req.Header.Set("X-GitHub-Event", tt.eventType)
			}
			if tt.signature != "" {
				req.Header.Set("X-Hub-Signature-256", tt.signature)
			}
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.handleGithubWebhook(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestJiraWebhookTableDriven(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	tests := []struct {
		name           string
		method         string
		payload        string
		signature      string
		expectedStatus int
	}{
		{
			name:           "GET method not allowed",
			method:         http.MethodGet,
			payload:        "",
			signature:      "",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "PUT method not allowed",
			method:         http.MethodPut,
			payload:        "",
			signature:      "",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "DELETE method not allowed",
			method:         http.MethodDelete,
			payload:        "",
			signature:      "",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "valid POST with issue_created event",
			method:         http.MethodPost,
			payload:        `{"webhookEvent": "jira:issue_created", "issue": {"key": "PROJ-123"}}`,
			signature:      "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid POST with issue_updated event",
			method:         http.MethodPost,
			payload:        `{"webhookEvent": "jira:issue_updated", "issue": {"key": "PROJ-456"}}`,
			signature:      "sha1=signature123",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid POST with comment_created event",
			method:         http.MethodPost,
			payload:        `{"webhookEvent": "comment_created", "comment": {"body": "test"}}`,
			signature:      "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid JSON",
			method:         http.MethodPost,
			payload:        "{invalid json",
			signature:      "",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "empty payload",
			method:         http.MethodPost,
			payload:        "",
			signature:      "",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/webhooks/jira", strings.NewReader(tt.payload))
			if tt.signature != "" {
				req.Header.Set("X-Hub-Signature", tt.signature)
			}
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.handleJiraWebhook(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

// TestJiraWebhookPreservesRawBody asserts the gateway buffers the exact request
// body into payload["_raw_body"] (TASK-333) so downstream body-HMAC verification
// runs over the bytes Jira signed, and that JSON still decodes from the buffer.
func TestJiraWebhookPreservesRawBody(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	var captured map[string]interface{}
	server.Router().RegisterWebhookHandler("jira", func(payload map[string]interface{}) {
		captured = payload
	})

	// Non-canonical formatting (whitespace + key order) that json.Marshal would
	// not reproduce — proves the raw bytes are preserved verbatim.
	rawBody := `{ "webhookEvent":  "jira:issue_created", "issue": {"key":"PROJ-9"} }`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/jira", strings.NewReader(rawBody))
	req.Header.Set("X-Hub-Signature", "sha256=deadbeef")
	w := httptest.NewRecorder()

	server.handleJiraWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if captured == nil {
		t.Fatal("webhook was not routed to the registered handler")
	}
	if got := captured["_raw_body"]; got != rawBody {
		t.Errorf("_raw_body = %q, want exact request body %q", got, rawBody)
	}
	if got := captured["_signature"]; got != "sha256=deadbeef" {
		t.Errorf("_signature = %q, want propagated header", got)
	}
	// JSON still decoded from the buffered bytes.
	if got := captured["webhookEvent"]; got != "jira:issue_created" {
		t.Errorf("decoded webhookEvent = %q, want jira:issue_created", got)
	}
}
