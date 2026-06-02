package linear

import (
	"context"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewWebhookHandler(t *testing.T) {
	client := NewClient(testutil.FakeLinearAPIKey)
	handler := NewWebhookHandler(client, "pilot", nil)

	if handler == nil {
		t.Fatal("NewWebhookHandler returned nil")
	}
	if handler.client != client {
		t.Error("handler.client does not match provided client")
	}
	if handler.pilotLabel != "pilot" {
		t.Errorf("handler.pilotLabel = %s, want 'pilot'", handler.pilotLabel)
	}
	if handler.onIssue != nil {
		t.Error("handler.onIssue should be nil initially")
	}
}

func TestNewWebhookHandler_WithProjectIDs(t *testing.T) {
	client := NewClient(testutil.FakeLinearAPIKey)
	projectIDs := []string{"proj-1", "proj-2"}
	handler := NewWebhookHandler(client, "pilot", projectIDs)

	if handler == nil {
		t.Fatal("NewWebhookHandler returned nil")
	}
	if len(handler.projectIDs) != 2 {
		t.Errorf("handler.projectIDs length = %d, want 2", len(handler.projectIDs))
	}
	if handler.projectIDs[0] != "proj-1" {
		t.Errorf("handler.projectIDs[0] = %s, want 'proj-1'", handler.projectIDs[0])
	}
}

func TestOnIssue(t *testing.T) {
	client := NewClient(testutil.FakeLinearAPIKey)
	handler := NewWebhookHandler(client, "pilot", nil)

	var callbackSet bool
	handler.OnIssue(func(ctx context.Context, issue *Issue) error {
		callbackSet = true
		return nil
	})

	if handler.onIssue == nil {
		t.Error("handler.onIssue should not be nil after OnIssue called")
	}

	// Invoke callback to verify it was set correctly
	_ = handler.onIssue(context.Background(), &Issue{})
	if !callbackSet {
		t.Error("callback was not invoked")
	}
}

func TestHandle_IssueCreated_NoPilotLabel(t *testing.T) {
	client := NewClient(testutil.FakeLinearAPIKey)
	handler := NewWebhookHandler(client, "pilot", nil)

	var callbackCalled bool
	handler.OnIssue(func(ctx context.Context, issue *Issue) error {
		callbackCalled = true
		return nil
	})

	payload := map[string]interface{}{
		"action": "create",
		"type":   "Issue",
		"data": map[string]interface{}{
			"id": "issue-123",
			"labels": []interface{}{
				map[string]interface{}{"id": "label-1", "name": "bug"},
				map[string]interface{}{"id": "label-2", "name": "enhancement"},
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

func TestHandle_IssueUpdated(t *testing.T) {
	client := NewClient(testutil.FakeLinearAPIKey)
	handler := NewWebhookHandler(client, "pilot", nil)

	var callbackCalled bool
	handler.OnIssue(func(ctx context.Context, issue *Issue) error {
		callbackCalled = true
		return nil
	})

	// Update events should be ignored
	payload := map[string]interface{}{
		"action": "update",
		"type":   "Issue",
		"data": map[string]interface{}{
			"id": "issue-123",
			"labels": []interface{}{
				map[string]interface{}{"id": "label-1", "name": "pilot"},
			},
		},
	}

	err := handler.Handle(context.Background(), payload)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if callbackCalled {
		t.Error("OnIssue callback should not be called for update events")
	}
}

func TestHandle_CommentEvent(t *testing.T) {
	client := NewClient(testutil.FakeLinearAPIKey)
	handler := NewWebhookHandler(client, "pilot", nil)

	var callbackCalled bool
	handler.OnIssue(func(ctx context.Context, issue *Issue) error {
		callbackCalled = true
		return nil
	})

	payload := map[string]interface{}{
		"action": "create",
		"type":   "Comment",
		"data": map[string]interface{}{
			"id":   "comment-123",
			"body": "Test comment",
		},
	}

	err := handler.Handle(context.Background(), payload)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if callbackCalled {
		t.Error("OnIssue callback should not be called for Comment events")
	}
}

func TestHandle_IssueDeleted(t *testing.T) {
	client := NewClient(testutil.FakeLinearAPIKey)
	handler := NewWebhookHandler(client, "pilot", nil)

	var callbackCalled bool
	handler.OnIssue(func(ctx context.Context, issue *Issue) error {
		callbackCalled = true
		return nil
	})

	payload := map[string]interface{}{
		"action": "remove",
		"type":   "Issue",
		"data": map[string]interface{}{
			"id": "issue-123",
		},
	}

	err := handler.Handle(context.Background(), payload)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if callbackCalled {
		t.Error("OnIssue callback should not be called for remove events")
	}
}

func TestHandle_InvalidDataType(t *testing.T) {
	client := NewClient(testutil.FakeLinearAPIKey)
	handler := NewWebhookHandler(client, "pilot", nil)

	var callbackCalled bool
	handler.OnIssue(func(ctx context.Context, issue *Issue) error {
		callbackCalled = true
		return nil
	})

	// data is not a map
	payload := map[string]interface{}{
		"action": "create",
		"type":   "Issue",
		"data":   "invalid",
	}

	err := handler.Handle(context.Background(), payload)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if callbackCalled {
		t.Error("OnIssue callback should not be called when data is invalid")
	}
}

func TestHandle_MissingActionOrType(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]interface{}
	}{
		{
			name: "missing action",
			payload: map[string]interface{}{
				"type": "Issue",
				"data": map[string]interface{}{"id": "123"},
			},
		},
		{
			name: "missing type",
			payload: map[string]interface{}{
				"action": "create",
				"data":   map[string]interface{}{"id": "123"},
			},
		},
		{
			name:    "empty payload",
			payload: map[string]interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(testutil.FakeLinearAPIKey)
			handler := NewWebhookHandler(client, "pilot", nil)

			var callbackCalled bool
			handler.OnIssue(func(ctx context.Context, issue *Issue) error {
				callbackCalled = true
				return nil
			})

			err := handler.Handle(context.Background(), tt.payload)
			if err != nil {
				t.Fatalf("Handle failed: %v", err)
			}

			if callbackCalled {
				t.Error("OnIssue callback should not be called for incomplete payloads")
			}
		})
	}
}
