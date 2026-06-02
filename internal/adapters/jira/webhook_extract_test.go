package jira

import (
	"testing"
)

func TestHasPilotLabel(t *testing.T) {
	client := NewClient("https://jira.example.com", "user", "token", PlatformCloud)
	handler := NewWebhookHandler(client, "", "pilot")

	tests := []struct {
		name   string
		labels []string
		want   bool
	}{
		{
			name:   "has pilot label",
			labels: []string{"bug", "pilot", "enhancement"},
			want:   true,
		},
		{
			name:   "has PILOT label (case insensitive)",
			labels: []string{"bug", "PILOT"},
			want:   true,
		},
		{
			name:   "no pilot label",
			labels: []string{"bug", "enhancement"},
			want:   false,
		},
		{
			name:   "empty labels",
			labels: []string{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &Issue{
				Fields: Fields{Labels: tt.labels},
			}
			got := handler.hasPilotLabel(issue)
			if got != tt.want {
				t.Errorf("hasPilotLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWasLabelAdded(t *testing.T) {
	client := NewClient("https://jira.example.com", "user", "token", PlatformCloud)
	handler := NewWebhookHandler(client, "", "pilot")

	tests := []struct {
		name    string
		payload map[string]interface{}
		want    bool
	}{
		{
			name: "pilot label added",
			payload: map[string]interface{}{
				"changelog": map[string]interface{}{
					"items": []interface{}{
						map[string]interface{}{
							"field":      "labels",
							"fromString": "bug",
							"toString":   "bug pilot",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "pilot label already present",
			payload: map[string]interface{}{
				"changelog": map[string]interface{}{
					"items": []interface{}{
						map[string]interface{}{
							"field":      "labels",
							"fromString": "pilot",
							"toString":   "pilot bug",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "different label added",
			payload: map[string]interface{}{
				"changelog": map[string]interface{}{
					"items": []interface{}{
						map[string]interface{}{
							"field":      "labels",
							"fromString": "",
							"toString":   "bug",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "no changelog",
			payload: map[string]interface{}{
				"issue": map[string]interface{}{},
			},
			want: false,
		},
		{
			name: "status changed, not labels",
			payload: map[string]interface{}{
				"changelog": map[string]interface{}{
					"items": []interface{}{
						map[string]interface{}{
							"field":      "status",
							"fromString": "To Do",
							"toString":   "In Progress",
						},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.wasLabelAdded(tt.payload)
			if got != tt.want {
				t.Errorf("wasLabelAdded() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractIssue(t *testing.T) {
	client := NewClient("https://jira.example.com", "user", "token", PlatformCloud)
	handler := NewWebhookHandler(client, "", "pilot")

	payload := map[string]interface{}{
		"issue": map[string]interface{}{
			"id":   "10001",
			"key":  "PROJ-42",
			"self": "https://jira.example.com/rest/api/3/issue/10001",
			"fields": map[string]interface{}{
				"summary":     "Test Issue",
				"description": "Issue description",
				"labels":      []interface{}{"pilot", "bug"},
				"issuetype": map[string]interface{}{
					"name": "Story",
				},
				"status": map[string]interface{}{
					"name": "To Do",
				},
				"priority": map[string]interface{}{
					"name": "High",
				},
				"project": map[string]interface{}{
					"key":  "PROJ",
					"name": "My Project",
				},
			},
		},
	}

	issue, err := handler.extractIssue(payload)
	if err != nil {
		t.Fatalf("extractIssue failed: %v", err)
	}

	if issue.Key != "PROJ-42" {
		t.Errorf("issue.Key = %s, want PROJ-42", issue.Key)
	}
	if issue.Fields.Summary != "Test Issue" {
		t.Errorf("issue.Fields.Summary = %s, want 'Test Issue'", issue.Fields.Summary)
	}
	if len(issue.Fields.Labels) != 2 {
		t.Errorf("issue.Fields.Labels = %v, want 2 labels", issue.Fields.Labels)
	}
	if issue.Fields.IssueType.Name != "Story" {
		t.Errorf("issue.Fields.IssueType.Name = %s, want 'Story'", issue.Fields.IssueType.Name)
	}
	if issue.Fields.Status.Name != "To Do" {
		t.Errorf("issue.Fields.Status.Name = %s, want 'To Do'", issue.Fields.Status.Name)
	}
	if issue.Fields.Priority.Name != "High" {
		t.Errorf("issue.Fields.Priority.Name = %s, want 'High'", issue.Fields.Priority.Name)
	}
}

func TestExtractIssue_MissingIssue(t *testing.T) {
	client := NewClient("https://jira.example.com", "user", "token", PlatformCloud)
	handler := NewWebhookHandler(client, "", "pilot")

	payload := map[string]interface{}{
		"webhookEvent": "jira:issue_created",
	}

	_, err := handler.extractIssue(payload)
	if err == nil {
		t.Error("expected error for missing issue in payload")
	}
}

func TestExtractADFText(t *testing.T) {
	client := NewClient("https://jira.example.com", "user", "token", PlatformCloud)
	handler := NewWebhookHandler(client, "", "pilot")

	tests := []struct {
		name string
		adf  map[string]interface{}
		want string
	}{
		{
			name: "simple paragraph",
			adf: map[string]interface{}{
				"type":    "doc",
				"version": 1,
				"content": []interface{}{
					map[string]interface{}{
						"type": "paragraph",
						"content": []interface{}{
							map[string]interface{}{
								"type": "text",
								"text": "Hello world",
							},
						},
					},
				},
			},
			want: "Hello world",
		},
		{
			name: "multiple paragraphs",
			adf: map[string]interface{}{
				"type":    "doc",
				"version": 1,
				"content": []interface{}{
					map[string]interface{}{
						"type": "paragraph",
						"content": []interface{}{
							map[string]interface{}{
								"type": "text",
								"text": "First paragraph",
							},
						},
					},
					map[string]interface{}{
						"type": "paragraph",
						"content": []interface{}{
							map[string]interface{}{
								"type": "text",
								"text": "Second paragraph",
							},
						},
					},
				},
			},
			want: "First paragraph\nSecond paragraph",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.extractADFText(tt.adf)
			if got != tt.want {
				t.Errorf("extractADFText() = %q, want %q", got, tt.want)
			}
		})
	}
}
