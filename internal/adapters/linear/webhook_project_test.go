package linear

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestIsAllowedProject(t *testing.T) {
	tests := []struct {
		name       string
		projectIDs []string
		issue      *Issue
		want       bool
	}{
		{
			name:       "no filter allows all",
			projectIDs: nil,
			issue:      &Issue{ID: "issue-1"},
			want:       true,
		},
		{
			name:       "empty filter allows all",
			projectIDs: []string{},
			issue:      &Issue{ID: "issue-1"},
			want:       true,
		},
		{
			name:       "filter rejects nil project",
			projectIDs: []string{"proj-1"},
			issue:      &Issue{ID: "issue-1", Project: nil},
			want:       false,
		},
		{
			name:       "filter allows matching project",
			projectIDs: []string{"proj-1"},
			issue:      &Issue{ID: "issue-1", Project: &Project{ID: "proj-1", Name: "Project 1"}},
			want:       true,
		},
		{
			name:       "filter rejects non-matching project",
			projectIDs: []string{"proj-1"},
			issue:      &Issue{ID: "issue-1", Project: &Project{ID: "proj-2", Name: "Project 2"}},
			want:       false,
		},
		{
			name:       "multiple filters allow any matching",
			projectIDs: []string{"proj-1", "proj-2", "proj-3"},
			issue:      &Issue{ID: "issue-1", Project: &Project{ID: "proj-2", Name: "Project 2"}},
			want:       true,
		},
		{
			name:       "multiple filters reject non-matching",
			projectIDs: []string{"proj-1", "proj-2"},
			issue:      &Issue{ID: "issue-1", Project: &Project{ID: "proj-3", Name: "Project 3"}},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(testutil.FakeLinearAPIKey)
			handler := NewWebhookHandler(client, "pilot", tt.projectIDs)

			got := handler.isAllowedProject(tt.issue)
			if got != tt.want {
				t.Errorf("isAllowedProject() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandle_IssueCreated_ProjectFilter(t *testing.T) {
	// Create mock server that returns issue with project
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
					"project": map[string]interface{}{
						"id":   "allowed-project-id",
						"name": "Allowed Project",
					},
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

	testHandler := &testWebhookHandler{
		pilotLabel: "pilot",
		serverURL:  server.URL,
		projectIDs: []string{"allowed-project-id"},
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
		t.Fatal("OnIssue callback was not called for allowed project")
	}
	if receivedIssue.Project == nil || receivedIssue.Project.ID != "allowed-project-id" {
		t.Error("issue should have the allowed project")
	}
}

func TestHandle_IssueCreated_ProjectFilter_Rejected(t *testing.T) {
	// Create mock server that returns issue with different project
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
					"project": map[string]interface{}{
						"id":   "other-project-id",
						"name": "Other Project",
					},
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

	testHandler := &testWebhookHandler{
		pilotLabel: "pilot",
		serverURL:  server.URL,
		projectIDs: []string{"allowed-project-id"},
	}

	var callbackCalled bool
	testHandler.onIssue = func(ctx context.Context, issue *Issue) error {
		callbackCalled = true
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

	if callbackCalled {
		t.Error("OnIssue callback should not be called for non-allowed project")
	}
}
