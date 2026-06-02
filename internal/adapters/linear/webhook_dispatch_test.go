package linear

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandle_IssueCreated_WithPilotLabel(t *testing.T) {
	// Create mock server for fetching issue details
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"issue": map[string]interface{}{
					"id":          "issue-123",
					"identifier":  "PROJ-42",
					"title":       "Test Issue",
					"description": "Test description",
					"priority":    2,
					"state": map[string]interface{}{
						"id":   "state-1",
						"name": "In Progress",
						"type": "started",
					},
					"labels": map[string]interface{}{
						"nodes": []interface{}{
							map[string]interface{}{"id": "label-1", "name": "pilot"},
						},
					},
					"assignee": nil,
					"project":  nil,
					"team": map[string]interface{}{
						"id":   "team-1",
						"name": "Engineering",
						"key":  "ENG",
					},
					"createdAt": "2024-01-15T10:00:00Z",
					"updatedAt": "2024-01-15T10:00:00Z",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create a test handler with mock
	testHandler := &testWebhookHandler{
		pilotLabel: "pilot",
		serverURL:  server.URL,
	}

	var receivedIssue *Issue
	testHandler.onIssue = func(ctx context.Context, issue *Issue) error {
		receivedIssue = issue
		return nil
	}

	payload := map[string]interface{}{
		"action": "create",
		"type":   "Issue",
		"data": map[string]interface{}{
			"id": "issue-123",
			"labels": []interface{}{
				map[string]interface{}{"id": "label-1", "name": "pilot"},
			},
		},
	}

	err := testHandler.Handle(context.Background(), payload)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if receivedIssue == nil {
		t.Fatal("OnIssue callback was not called")
	}
	if receivedIssue.Identifier != "PROJ-42" {
		t.Errorf("issue.Identifier = %s, want PROJ-42", receivedIssue.Identifier)
	}
}

func TestHandle_NoCallback(t *testing.T) {
	// Create mock server to return an issue
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"issue": map[string]interface{}{
					"id":          "issue-123",
					"identifier":  "PROJ-42",
					"title":       "Test Issue",
					"description": "",
					"priority":    0,
					"state":       map[string]interface{}{"id": "s1", "name": "Todo", "type": "unstarted"},
					"labels":      map[string]interface{}{"nodes": []interface{}{}},
					"team":        map[string]interface{}{"id": "t1", "name": "Eng", "key": "ENG"},
					"createdAt":   "2024-01-15T10:00:00Z",
					"updatedAt":   "2024-01-15T10:00:00Z",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	testHandler := &testWebhookHandler{
		pilotLabel: "pilot",
		serverURL:  server.URL,
		onIssue:    nil, // No callback
	}

	payload := map[string]interface{}{
		"action": "create",
		"type":   "Issue",
		"data": map[string]interface{}{
			"id":       "issue-123",
			"labelIds": []interface{}{"label-1"}, // Has label IDs so it passes hasPilotLabel
		},
	}

	// Should not panic or error
	err := testHandler.Handle(context.Background(), payload)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
}

func TestHandle_CallbackError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"issue": map[string]interface{}{
					"id":          "issue-123",
					"identifier":  "PROJ-42",
					"title":       "Test Issue",
					"description": "",
					"priority":    0,
					"state":       map[string]interface{}{"id": "s1", "name": "Todo", "type": "unstarted"},
					"labels":      map[string]interface{}{"nodes": []interface{}{}},
					"team":        map[string]interface{}{"id": "t1", "name": "Eng", "key": "ENG"},
					"createdAt":   "2024-01-15T10:00:00Z",
					"updatedAt":   "2024-01-15T10:00:00Z",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	expectedErr := errors.New("callback error")
	testHandler := &testWebhookHandler{
		pilotLabel: "pilot",
		serverURL:  server.URL,
		onIssue: func(ctx context.Context, issue *Issue) error {
			return expectedErr
		},
	}

	payload := map[string]interface{}{
		"action": "create",
		"type":   "Issue",
		"data": map[string]interface{}{
			"id": "issue-123",
			"labels": []interface{}{
				map[string]interface{}{"id": "label-1", "name": "pilot"},
			},
		},
	}

	err := testHandler.Handle(context.Background(), payload)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if err != expectedErr {
		t.Errorf("error = %v, want %v", err, expectedErr)
	}
}
