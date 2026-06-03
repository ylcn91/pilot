package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestHandleIssueUpdated_NoChangelog_ShortCircuits verifies that an
// issue_updated event with no changelog (wasLabelAdded == false) returns nil
// without fetching issue details or invoking the callback.
func TestHandleIssueUpdated_NoChangelog_ShortCircuits(t *testing.T) {
	var mu sync.Mutex
	var serverHit bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		serverHit = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Issue{Key: "PROJ-42"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user", "token", PlatformCloud)
	handler := NewWebhookHandler(client, "", "pilot")

	var callbackCalled bool
	handler.OnIssue(func(ctx context.Context, issue *Issue) error {
		callbackCalled = true
		return nil
	})

	payload := map[string]interface{}{
		"webhookEvent": "jira:issue_updated",
		"issue": map[string]interface{}{
			"key": "PROJ-42",
			"fields": map[string]interface{}{
				"labels": []interface{}{"pilot"},
			},
		},
		// No changelog → wasLabelAdded false.
	}

	if err := handler.Handle(context.Background(), payload); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if callbackCalled {
		t.Error("callback must not run when no label was added")
	}
	mu.Lock()
	defer mu.Unlock()
	if serverHit {
		t.Error("GetIssue must not be called when no label was added")
	}
}

// TestHandleIssueUpdated_LabelAddedButCurrentlyMissing verifies the
// re-verification branch in handleIssueUpdated: the changelog says the pilot
// label was added, but the payload's current labels no longer include it, so
// processing is skipped (no API fetch, no callback).
func TestHandleIssueUpdated_LabelAddedButCurrentlyMissing(t *testing.T) {
	var mu sync.Mutex
	var serverHit bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		serverHit = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Issue{Key: "PROJ-42"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user", "token", PlatformCloud)
	handler := NewWebhookHandler(client, "", "pilot")

	var callbackCalled bool
	handler.OnIssue(func(ctx context.Context, issue *Issue) error {
		callbackCalled = true
		return nil
	})

	payload := map[string]interface{}{
		"webhookEvent": "jira:issue_updated",
		"issue": map[string]interface{}{
			"key": "PROJ-42",
			"fields": map[string]interface{}{
				// Current labels do NOT include pilot (e.g. added then removed
				// before this event was processed).
				"labels": []interface{}{"bug"},
			},
		},
		"changelog": map[string]interface{}{
			"id": "999",
			"items": []interface{}{
				map[string]interface{}{
					"field":      "labels",
					"fromString": "",
					"toString":   "pilot",
				},
			},
		},
	}

	if err := handler.Handle(context.Background(), payload); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if callbackCalled {
		t.Error("callback must not run when the issue no longer carries the pilot label")
	}
	mu.Lock()
	defer mu.Unlock()
	if serverHit {
		t.Error("GetIssue must not be called when pilot label is currently absent")
	}
}

// TestHandleIssueUpdated_CustomPilotLabel verifies the label-added path honors a
// non-default pilot label, matching case-insensitively in both the changelog
// (wasLabelAdded) and the current-label check (hasPilotLabel).
func TestHandleIssueUpdated_CustomPilotLabel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issue := Issue{
			ID:  "10001",
			Key: "PROJ-77",
			Fields: Fields{
				Summary: "Custom label issue",
				Labels:  []string{"AutoPilot"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issue)
	}))
	defer server.Close()

	client := NewClient(server.URL, "user", "token", PlatformCloud)
	handler := NewWebhookHandler(client, "", "autopilot")

	var receivedKey string
	handler.OnIssue(func(ctx context.Context, issue *Issue) error {
		receivedKey = issue.Key
		return nil
	})

	payload := map[string]interface{}{
		"webhookEvent": "jira:issue_updated",
		"issue": map[string]interface{}{
			"key": "PROJ-77",
			"fields": map[string]interface{}{
				// Current labels use different casing than configured label.
				"labels": []interface{}{"AutoPilot"},
			},
		},
		"changelog": map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{
					"field":      "labels",
					"fromString": "bug",
					"toString":   "bug AutoPilot",
				},
			},
		},
	}

	if err := handler.Handle(context.Background(), payload); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if receivedKey != "PROJ-77" {
		t.Errorf("expected callback for PROJ-77, got %q", receivedKey)
	}
}
