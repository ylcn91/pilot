package asana

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestHasPilotTag(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	handler := NewWebhookHandler(client, "", "pilot")

	tests := []struct {
		name string
		task *Task
		want bool
	}{
		{
			name: "has pilot tag",
			task: &Task{
				Tags: []Tag{{GID: "1", Name: "pilot"}},
			},
			want: true,
		},
		{
			name: "has PILOT tag (case insensitive)",
			task: &Task{
				Tags: []Tag{{GID: "1", Name: "PILOT"}},
			},
			want: true,
		},
		{
			name: "no pilot tag",
			task: &Task{
				Tags: []Tag{{GID: "1", Name: "other"}},
			},
			want: false,
		},
		{
			name: "empty tags",
			task: &Task{
				Tags: []Tag{},
			},
			want: false,
		},
		{
			name: "pilot among other tags",
			task: &Task{
				Tags: []Tag{
					{GID: "1", Name: "bug"},
					{GID: "2", Name: "pilot"},
					{GID: "3", Name: "urgent"},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.hasPilotTag(tt.task)
			if got != tt.want {
				t.Errorf("hasPilotTag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWasTagAdded(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	handler := NewWebhookHandler(client, "", "pilot")

	tests := []struct {
		name   string
		change *WebhookChange
		want   bool
	}{
		{
			name: "pilot tag added",
			change: &WebhookChange{
				Field:  "tags",
				Action: "added",
				AddedValue: map[string]interface{}{
					"gid":  "123",
					"name": "pilot",
				},
			},
			want: true,
		},
		{
			name: "other tag added",
			change: &WebhookChange{
				Field:  "tags",
				Action: "added",
				AddedValue: map[string]interface{}{
					"gid":  "123",
					"name": "other",
				},
			},
			want: false,
		},
		{
			name: "tag removed (not added)",
			change: &WebhookChange{
				Field:  "tags",
				Action: "removed",
				RemovedValue: map[string]interface{}{
					"gid":  "123",
					"name": "pilot",
				},
			},
			want: false,
		},
		{
			name: "tag added by GID only",
			change: &WebhookChange{
				Field:  "tags",
				Action: "added",
				AddedValue: map[string]interface{}{
					"gid": "123",
				},
			},
			want: true, // Optimistically returns true
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.wasTagAdded(tt.change)
			if got != tt.want {
				t.Errorf("wasTagAdded() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandleRaw(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := APIResponse[Task]{
			Data: Task{
				GID:       "123456",
				Name:      "Test Task",
				Completed: false,
				Tags: []Tag{
					{GID: "1", Name: "pilot"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	handler := NewWebhookHandler(client, "", "pilot")

	var receivedTask *Task
	handler.OnTask(func(ctx context.Context, task *Task) error {
		receivedTask = task
		return nil
	})

	events := []map[string]interface{}{
		{
			"action": "added",
			"resource": map[string]interface{}{
				"gid":           "123456",
				"resource_type": "task",
			},
		},
	}

	err := handler.HandleRaw(context.Background(), events)
	if err != nil {
		t.Errorf("HandleRaw() returned error: %v", err)
	}
	if receivedTask == nil {
		t.Error("callback was not called")
	}
}

func TestParseEvent(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	handler := NewWebhookHandler(client, "", "pilot")

	data := map[string]interface{}{
		"action": "changed",
		"resource": map[string]interface{}{
			"gid":           "123",
			"resource_type": "task",
			"name":          "Test Task",
		},
		"parent": map[string]interface{}{
			"gid":           "456",
			"resource_type": "project",
		},
		"change": map[string]interface{}{
			"field":     "completed",
			"action":    "changed",
			"new_value": true,
		},
	}

	event := handler.parseEvent(data)

	if event.Action != "changed" {
		t.Errorf("event.Action = %s, want changed", event.Action)
	}
	if event.Resource.GID != "123" {
		t.Errorf("event.Resource.GID = %s, want 123", event.Resource.GID)
	}
	if event.Resource.ResourceType != "task" {
		t.Errorf("event.Resource.ResourceType = %s, want task", event.Resource.ResourceType)
	}
	if event.Parent == nil {
		t.Error("event.Parent is nil")
	} else if event.Parent.GID != "456" {
		t.Errorf("event.Parent.GID = %s, want 456", event.Parent.GID)
	}
	if event.Change == nil {
		t.Error("event.Change is nil")
	} else {
		if event.Change.Field != "completed" {
			t.Errorf("event.Change.Field = %s, want completed", event.Change.Field)
		}
	}
}

func TestOnTask_Callback(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	handler := NewWebhookHandler(client, "", "pilot")

	called := false
	handler.OnTask(func(ctx context.Context, task *Task) error {
		called = true
		return nil
	})

	if handler.onTask == nil {
		t.Error("onTask callback not set")
	}

	// Verify callback is stored
	err := handler.onTask(context.Background(), &Task{})
	if err != nil {
		t.Errorf("callback returned error: %v", err)
	}
	if !called {
		t.Error("callback was not called")
	}
}

func TestWebhookEventTypes(t *testing.T) {
	// Verify constants are defined correctly
	if EventTaskAdded != "added" {
		t.Errorf("EventTaskAdded = %s, want added", EventTaskAdded)
	}
	if EventTaskChanged != "changed" {
		t.Errorf("EventTaskChanged = %s, want changed", EventTaskChanged)
	}
	if EventTaskRemoved != "removed" {
		t.Errorf("EventTaskRemoved = %s, want removed", EventTaskRemoved)
	}
	if EventTaskDeleted != "deleted" {
		t.Errorf("EventTaskDeleted = %s, want deleted", EventTaskDeleted)
	}
	if EventTaskUndeleted != "undeleted" {
		t.Errorf("EventTaskUndeleted = %s, want undeleted", EventTaskUndeleted)
	}
}
