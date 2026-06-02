package linear

import (
	"encoding/json"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestHasPilotLabel_WithLabels(t *testing.T) {
	tests := []struct {
		name       string
		pilotLabel string
		data       map[string]interface{}
		want       bool
	}{
		{
			name:       "has pilot label",
			pilotLabel: "pilot",
			data: map[string]interface{}{
				"labels": []interface{}{
					map[string]interface{}{"id": "1", "name": "bug"},
					map[string]interface{}{"id": "2", "name": "pilot"},
				},
			},
			want: true,
		},
		{
			name:       "no pilot label",
			pilotLabel: "pilot",
			data: map[string]interface{}{
				"labels": []interface{}{
					map[string]interface{}{"id": "1", "name": "bug"},
					map[string]interface{}{"id": "2", "name": "enhancement"},
				},
			},
			want: false,
		},
		{
			name:       "empty labels",
			pilotLabel: "pilot",
			data: map[string]interface{}{
				"labels": []interface{}{},
			},
			want: false,
		},
		{
			name:       "custom pilot label",
			pilotLabel: "ai-task",
			data: map[string]interface{}{
				"labels": []interface{}{
					map[string]interface{}{"id": "1", "name": "ai-task"},
				},
			},
			want: true,
		},
		{
			name:       "label without name field",
			pilotLabel: "pilot",
			data: map[string]interface{}{
				"labels": []interface{}{
					map[string]interface{}{"id": "1"},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(testutil.FakeLinearAPIKey)
			handler := NewWebhookHandler(client, tt.pilotLabel, nil)

			got := handler.hasPilotLabel(tt.data)
			if got != tt.want {
				t.Errorf("hasPilotLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasPilotLabel_WithLabelIds(t *testing.T) {
	tests := []struct {
		name       string
		pilotLabel string
		data       map[string]interface{}
		want       bool
	}{
		{
			name:       "has label IDs (assumes pilot label present)",
			pilotLabel: "pilot",
			data: map[string]interface{}{
				"labelIds": []interface{}{"label-1", "label-2"},
			},
			want: true,
		},
		{
			name:       "empty label IDs",
			pilotLabel: "pilot",
			data: map[string]interface{}{
				"labelIds": []interface{}{},
			},
			want: false,
		},
		{
			name:       "no labels or labelIds",
			pilotLabel: "pilot",
			data:       map[string]interface{}{},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(testutil.FakeLinearAPIKey)
			handler := NewWebhookHandler(client, tt.pilotLabel, nil)

			got := handler.hasPilotLabel(tt.data)
			if got != tt.want {
				t.Errorf("hasPilotLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasPilotLabel_InvalidLabelFormat(t *testing.T) {
	tests := []struct {
		name string
		data map[string]interface{}
		want bool
	}{
		{
			name: "labels not an array",
			data: map[string]interface{}{
				"labels": "invalid",
			},
			want: false,
		},
		{
			name: "labels contains non-map elements",
			data: map[string]interface{}{
				"labels": []interface{}{
					"string-label",
					123,
				},
			},
			want: false,
		},
		{
			name: "labelIds not an array",
			data: map[string]interface{}{
				"labelIds": "invalid",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(testutil.FakeLinearAPIKey)
			handler := NewWebhookHandler(client, "pilot", nil)

			got := handler.hasPilotLabel(tt.data)
			if got != tt.want {
				t.Errorf("hasPilotLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWebhookEventTypes(t *testing.T) {
	tests := []struct {
		eventType WebhookEventType
		expected  string
	}{
		{EventIssueCreated, "Issue.create"},
		{EventIssueUpdated, "Issue.update"},
		{EventIssueDeleted, "Issue.delete"},
		{EventCommentAdded, "Comment.create"},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			if string(tt.eventType) != tt.expected {
				t.Errorf("event type = %s, want %s", tt.eventType, tt.expected)
			}
		})
	}
}

func TestWebhookPayloadStructure(t *testing.T) {
	// Test that WebhookPayload can be properly unmarshaled
	jsonPayload := `{
		"action": "create",
		"type": "Issue",
		"data": {"id": "issue-123", "title": "Test"},
		"url": "https://linear.app/team/PROJ-42",
		"createdAt": "2024-01-15T10:00:00Z",
		"webhookId": "webhook-123",
		"webhookTimestamp": 1705318800000
	}`

	var payload WebhookPayload
	err := json.Unmarshal([]byte(jsonPayload), &payload)
	if err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if payload.Action != "create" {
		t.Errorf("payload.Action = %s, want 'create'", payload.Action)
	}
	if payload.Type != "Issue" {
		t.Errorf("payload.Type = %s, want 'Issue'", payload.Type)
	}
	if payload.URL != "https://linear.app/team/PROJ-42" {
		t.Errorf("payload.URL = %s, want 'https://linear.app/team/PROJ-42'", payload.URL)
	}
	if payload.WebhookID != "webhook-123" {
		t.Errorf("payload.WebhookID = %s, want 'webhook-123'", payload.WebhookID)
	}
	if payload.WebhookTS != 1705318800000 {
		t.Errorf("payload.WebhookTS = %d, want 1705318800000", payload.WebhookTS)
	}

	// Verify data field
	if payload.Data["id"] != "issue-123" {
		t.Errorf("payload.Data[id] = %v, want 'issue-123'", payload.Data["id"])
	}
}
