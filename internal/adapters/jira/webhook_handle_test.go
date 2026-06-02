package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandle_IssueCreated(t *testing.T) {
	// Create mock server for fetching issue details
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issue := Issue{
			ID:  "10001",
			Key: "PROJ-42",
			Fields: Fields{
				Summary: "Test Issue",
				Labels:  []string{"pilot"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issue)
	}))
	defer server.Close()

	client := NewClient(server.URL, "user", "token", PlatformCloud)
	handler := NewWebhookHandler(client, "", "pilot")

	var receivedIssue *Issue
	handler.OnIssue(func(ctx context.Context, issue *Issue) error {
		receivedIssue = issue
		return nil
	})

	payload := map[string]interface{}{
		"webhookEvent": "jira:issue_created",
		"issue": map[string]interface{}{
			"id":   "10001",
			"key":  "PROJ-42",
			"self": "https://jira.example.com/rest/api/3/issue/10001",
			"fields": map[string]interface{}{
				"summary": "Test Issue",
				"labels":  []interface{}{"pilot"},
			},
		},
	}

	err := handler.Handle(context.Background(), payload)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if receivedIssue == nil {
		t.Fatal("OnIssue callback was not called")
	}
	if receivedIssue.Key != "PROJ-42" {
		t.Errorf("issue.Key = %s, want PROJ-42", receivedIssue.Key)
	}
}

func TestHandle_IssueCreated_NoPilotLabel(t *testing.T) {
	client := NewClient("https://jira.example.com", "user", "token", PlatformCloud)
	handler := NewWebhookHandler(client, "", "pilot")

	var callbackCalled bool
	handler.OnIssue(func(ctx context.Context, issue *Issue) error {
		callbackCalled = true
		return nil
	})

	payload := map[string]interface{}{
		"webhookEvent": "jira:issue_created",
		"issue": map[string]interface{}{
			"id":  "10001",
			"key": "PROJ-42",
			"fields": map[string]interface{}{
				"summary": "Test Issue",
				"labels":  []interface{}{"bug", "enhancement"},
			},
		},
	}

	err := handler.Handle(context.Background(), payload)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if callbackCalled {
		t.Error("OnIssue callback should not be called for issues without pilot label")
	}
}

func TestHandle_IssueUpdated_LabelAdded(t *testing.T) {
	// Create mock server for fetching issue details
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issue := Issue{
			ID:  "10001",
			Key: "PROJ-42",
			Fields: Fields{
				Summary: "Test Issue",
				Labels:  []string{"pilot"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issue)
	}))
	defer server.Close()

	client := NewClient(server.URL, "user", "token", PlatformCloud)
	handler := NewWebhookHandler(client, "", "pilot")

	var receivedIssue *Issue
	handler.OnIssue(func(ctx context.Context, issue *Issue) error {
		receivedIssue = issue
		return nil
	})

	payload := map[string]interface{}{
		"webhookEvent": "jira:issue_updated",
		"issue": map[string]interface{}{
			"id":  "10001",
			"key": "PROJ-42",
			"fields": map[string]interface{}{
				"summary": "Test Issue",
				"labels":  []interface{}{"pilot"},
			},
		},
		"changelog": map[string]interface{}{
			"id": "12345",
			"items": []interface{}{
				map[string]interface{}{
					"field":      "labels",
					"fieldtype":  "jira",
					"fromString": "",
					"toString":   "pilot",
				},
			},
		},
	}

	err := handler.Handle(context.Background(), payload)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if receivedIssue == nil {
		t.Fatal("OnIssue callback was not called")
	}
}

func TestHandle_IssueUpdated_DifferentLabel(t *testing.T) {
	client := NewClient("https://jira.example.com", "user", "token", PlatformCloud)
	handler := NewWebhookHandler(client, "", "pilot")

	var callbackCalled bool
	handler.OnIssue(func(ctx context.Context, issue *Issue) error {
		callbackCalled = true
		return nil
	})

	payload := map[string]interface{}{
		"webhookEvent": "jira:issue_updated",
		"issue": map[string]interface{}{
			"id":  "10001",
			"key": "PROJ-42",
			"fields": map[string]interface{}{
				"summary": "Test Issue",
				"labels":  []interface{}{"bug"},
			},
		},
		"changelog": map[string]interface{}{
			"id": "12345",
			"items": []interface{}{
				map[string]interface{}{
					"field":      "labels",
					"fieldtype":  "jira",
					"fromString": "",
					"toString":   "bug",
				},
			},
		},
	}

	err := handler.Handle(context.Background(), payload)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if callbackCalled {
		t.Error("OnIssue callback should not be called when non-pilot label is added")
	}
}

func TestHandle_UnknownEvent(t *testing.T) {
	client := NewClient("https://jira.example.com", "user", "token", PlatformCloud)
	handler := NewWebhookHandler(client, "", "pilot")

	var callbackCalled bool
	handler.OnIssue(func(ctx context.Context, issue *Issue) error {
		callbackCalled = true
		return nil
	})

	payload := map[string]interface{}{
		"webhookEvent": "comment_created",
		"issue": map[string]interface{}{
			"key": "PROJ-42",
		},
	}

	err := handler.Handle(context.Background(), payload)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if callbackCalled {
		t.Error("OnIssue callback should not be called for comment_created events")
	}
}
