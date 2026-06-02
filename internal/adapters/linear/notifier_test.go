package linear

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewNotifier(t *testing.T) {
	client := NewClient(testutil.FakeLinearAPIKey)
	notifier := NewNotifier(client)

	if notifier == nil {
		t.Fatal("NewNotifier returned nil")
	}
	if notifier.client != client {
		t.Error("notifier.client not set correctly")
	}
}

func TestNotifyTaskStarted_Success(t *testing.T) {
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		// Verify it's a commentCreate mutation
		if !strings.Contains(reqBody.Query, "commentCreate") {
			t.Errorf("query should contain 'commentCreate', got: %s", reqBody.Query)
		}

		capturedBody = reqBody.Variables["body"].(string)

		// Verify variables
		if reqBody.Variables["issueId"] != "issue-123" {
			t.Errorf("variables[issueId] = %v, want issue-123", reqBody.Variables["issueId"])
		}

		resp := GraphQLResponse{
			Data: json.RawMessage(`{"commentCreate": {"success": true}}`),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeLinearAPIKey, server.URL)
	notifier := NewNotifier(client)

	err := notifier.NotifyTaskStarted(context.Background(), "issue-123", "TASK-456")
	if err != nil {
		t.Fatalf("NotifyTaskStarted() error = %v", err)
	}

	if !strings.Contains(capturedBody, "Pilot started working") {
		t.Errorf("comment should contain 'Pilot started working', got: %s", capturedBody)
	}
	if !strings.Contains(capturedBody, "TASK-456") {
		t.Errorf("comment should contain task ID 'TASK-456', got: %s", capturedBody)
	}
}

func TestNotifyTaskStarted_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "Internal error"}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeLinearAPIKey, server.URL)
	notifier := NewNotifier(client)

	err := notifier.NotifyTaskStarted(context.Background(), "issue-123", "TASK-456")
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !strings.Contains(err.Error(), "failed to add start comment") {
		t.Errorf("error = %v, want to contain 'failed to add start comment'", err)
	}
}

func TestNotifyProgress_Phases(t *testing.T) {
	tests := []struct {
		name    string
		phase   string
		details string
	}{
		{name: "exploring phase", phase: "exploring", details: "Analyzing codebase"},
		{name: "research phase", phase: "research", details: "Reading docs"},
		{name: "implementing phase", phase: "implementing", details: "Writing code"},
		{name: "impl phase", phase: "impl", details: "Building feature"},
		{name: "testing phase", phase: "testing", details: "Running tests"},
		{name: "verify phase", phase: "verify", details: "Verifying build"},
		{name: "committing phase", phase: "committing", details: "Creating commit"},
		{name: "unknown phase", phase: "planning", details: "Planning work"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedBody string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var reqBody GraphQLRequest
				_ = json.NewDecoder(r.Body).Decode(&reqBody)
				capturedBody = reqBody.Variables["body"].(string)

				resp := GraphQLResponse{
					Data: json.RawMessage(`{"commentCreate": {"success": true}}`),
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeLinearAPIKey, server.URL)
			notifier := NewNotifier(client)

			err := notifier.NotifyProgress(context.Background(), "issue-123", tt.phase, tt.details)
			if err != nil {
				t.Fatalf("NotifyProgress() error = %v", err)
			}

			if !strings.Contains(capturedBody, tt.phase) {
				t.Errorf("comment should contain phase %q, got: %s", tt.phase, capturedBody)
			}
			if !strings.Contains(capturedBody, tt.details) {
				t.Errorf("comment should contain details %q, got: %s", tt.details, capturedBody)
			}
		})
	}
}

func TestNotifyProgress_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := GraphQLResponse{
			Errors: []GraphQLError{{Message: "Rate limited"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeLinearAPIKey, server.URL)
	notifier := NewNotifier(client)

	err := notifier.NotifyProgress(context.Background(), "issue-123", "testing", "Running tests")
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !strings.Contains(err.Error(), "failed to add progress comment") {
		t.Errorf("error = %v, want to contain 'failed to add progress comment'", err)
	}
}
