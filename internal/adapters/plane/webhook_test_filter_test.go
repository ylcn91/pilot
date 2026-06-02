package plane

import (
	"encoding/json"
	"testing"
)

func TestIsAllowedProject(t *testing.T) {
	tests := []struct {
		name       string
		projectIDs []string
		projectID  string
		want       bool
	}{
		{"no filter allows all", nil, "any-project", true},
		{"empty filter allows all", []string{}, "any-project", true},
		{"matching project allowed", []string{"proj-1"}, "proj-1", true},
		{"non-matching project rejected", []string{"proj-1"}, "proj-2", false},
		{"multiple filters - match", []string{"proj-1", "proj-2"}, "proj-2", true},
		{"multiple filters - no match", []string{"proj-1", "proj-2"}, "proj-3", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewWebhookHandler("", "", tt.projectIDs)
			got := h.isAllowedProject(tt.projectID)
			if got != tt.want {
				t.Errorf("isAllowedProject(%s) = %v, want %v", tt.projectID, got, tt.want)
			}
		})
	}
}

func TestHasPilotLabel(t *testing.T) {
	tests := []struct {
		name       string
		pilotLabel string
		labelIDs   []string
		want       bool
	}{
		{"has pilot label", "pilot-uuid", []string{"other", "pilot-uuid"}, true},
		{"no pilot label", "pilot-uuid", []string{"bug", "feature"}, false},
		{"empty labels", "pilot-uuid", nil, false},
		{"empty pilot label config", "", []string{"some-label"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewWebhookHandler("", tt.pilotLabel, nil)
			got := h.hasPilotLabel(tt.labelIDs)
			if got != tt.want {
				t.Errorf("hasPilotLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWebhookPayload_Unmarshal(t *testing.T) {
	raw := `{
		"event": "issue",
		"action": "created",
		"webhook_id": "wh-abc",
		"workspace_id": "ws-def",
		"data": {
			"id": "wi-123",
			"name": "Test item",
			"state": "state-uuid",
			"labels": ["lbl-1", "lbl-2"],
			"project": "proj-uuid",
			"sequence_id": 421
		}
	}`

	var wp WebhookPayload
	err := json.Unmarshal([]byte(raw), &wp)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if wp.Event != "issue" {
		t.Errorf("Event = %s, want 'issue'", wp.Event)
	}
	if wp.Action != "created" {
		t.Errorf("Action = %s, want 'created'", wp.Action)
	}
	if wp.WebhookID != "wh-abc" {
		t.Errorf("WebhookID = %s, want 'wh-abc'", wp.WebhookID)
	}
	if wp.WorkspaceID != "ws-def" {
		t.Errorf("WorkspaceID = %s, want 'ws-def'", wp.WorkspaceID)
	}

	var data WebhookWorkItemData
	if err := json.Unmarshal(wp.Data, &data); err != nil {
		t.Fatalf("failed to unmarshal data: %v", err)
	}
	if data.ID != "wi-123" {
		t.Errorf("data.ID = %s, want 'wi-123'", data.ID)
	}
	if data.SequenceID != 421 {
		t.Errorf("data.SequenceID = %d, want 421", data.SequenceID)
	}
	if len(data.LabelIDs) != 2 {
		t.Errorf("data.LabelIDs length = %d, want 2", len(data.LabelIDs))
	}
}
