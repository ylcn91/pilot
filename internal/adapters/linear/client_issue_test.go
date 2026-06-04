package linear

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestGetIssue_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		// Verify the ID variable
		if reqBody.Variables["id"] != "issue-123" {
			t.Errorf("variables[id] = %v, want issue-123", reqBody.Variables["id"])
		}

		// Verify query contains expected fields
		if !contains(reqBody.Query, "issue(id: $id)") {
			t.Errorf("query should contain 'issue(id: $id)', got: %s", reqBody.Query)
		}

		resp := GraphQLResponse{
			Data: json.RawMessage(`{
				"issue": {
					"id": "issue-123",
					"identifier": "PROJ-42",
					"title": "Fix the bug",
					"description": "Description of the bug",
					"priority": 2,
					"state": {
						"id": "state-1",
						"name": "In Progress",
						"type": "started"
					},
					"labels": {
						"nodes": [
							{"id": "label-1", "name": "bug"},
							{"id": "label-2", "name": "pilot"}
						]
					},
					"assignee": {
						"id": "user-1",
						"name": "John Doe",
						"email": "john@example.com"
					},
					"project": {
						"id": "project-1",
						"name": "Main Project"
					},
					"team": {
						"id": "team-1",
						"name": "Engineering",
						"key": "ENG"
					},
					"createdAt": "2024-01-15T10:00:00Z",
					"updatedAt": "2024-01-16T12:00:00Z"
				}
			}`),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestableClient(server.URL, testutil.FakeLinearAPIKey)

	issue, err := client.getIssue(context.Background(), "issue-123")
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}

	if issue.ID != "issue-123" {
		t.Errorf("issue.ID = %s, want issue-123", issue.ID)
	}
	if issue.Identifier != "PROJ-42" {
		t.Errorf("issue.Identifier = %s, want PROJ-42", issue.Identifier)
	}
	if issue.Title != "Fix the bug" {
		t.Errorf("issue.Title = %s, want 'Fix the bug'", issue.Title)
	}
	if issue.Description != "Description of the bug" {
		t.Errorf("issue.Description = %s, want 'Description of the bug'", issue.Description)
	}
	if issue.Priority != 2 {
		t.Errorf("issue.Priority = %d, want 2", issue.Priority)
	}
	if issue.State.Name != "In Progress" {
		t.Errorf("issue.State.Name = %s, want 'In Progress'", issue.State.Name)
	}
	if issue.State.Type != "started" {
		t.Errorf("issue.State.Type = %s, want 'started'", issue.State.Type)
	}
	if issue.Team.Key != "ENG" {
		t.Errorf("issue.Team.Key = %s, want ENG", issue.Team.Key)
	}
	if issue.Assignee == nil {
		t.Error("issue.Assignee is nil")
	} else if issue.Assignee.Email != "john@example.com" {
		t.Errorf("issue.Assignee.Email = %s, want john@example.com", issue.Assignee.Email)
	}
	if issue.Project == nil {
		t.Error("issue.Project is nil")
	} else if issue.Project.Name != "Main Project" {
		t.Errorf("issue.Project.Name = %s, want 'Main Project'", issue.Project.Name)
	}
}

func TestGetIssue_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := GraphQLResponse{
			Errors: []GraphQLError{
				{Message: "Entity not found: Issue"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestableClient(server.URL, testutil.FakeLinearAPIKey)

	_, err := client.getIssue(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !contains(err.Error(), "Entity not found") {
		t.Errorf("error = %v, want to contain 'Entity not found'", err)
	}
}

func TestGetIssue_NullAssigneeAndProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := GraphQLResponse{
			Data: json.RawMessage(`{
				"issue": {
					"id": "issue-123",
					"identifier": "PROJ-42",
					"title": "Unassigned issue",
					"description": "",
					"priority": 0,
					"state": {
						"id": "state-1",
						"name": "Backlog",
						"type": "backlog"
					},
					"labels": {"nodes": []},
					"assignee": null,
					"project": null,
					"team": {
						"id": "team-1",
						"name": "Engineering",
						"key": "ENG"
					},
					"createdAt": "2024-01-15T10:00:00Z",
					"updatedAt": "2024-01-15T10:00:00Z"
				}
			}`),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestableClient(server.URL, testutil.FakeLinearAPIKey)

	issue, err := client.getIssue(context.Background(), "issue-123")
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}

	if issue.Assignee != nil {
		t.Errorf("issue.Assignee = %v, want nil", issue.Assignee)
	}
	if issue.Project != nil {
		t.Errorf("issue.Project = %v, want nil", issue.Project)
	}
	if len(issue.Labels) != 0 {
		t.Errorf("issue.Labels = %v, want empty", issue.Labels)
	}
}

func TestUpdateIssueState_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		// Verify mutation
		if !contains(reqBody.Query, "issueUpdate") {
			t.Errorf("query should contain 'issueUpdate', got: %s", reqBody.Query)
		}
		if !contains(reqBody.Query, "stateId") {
			t.Errorf("query should contain 'stateId', got: %s", reqBody.Query)
		}

		// Verify variables
		if reqBody.Variables["id"] != "issue-123" {
			t.Errorf("variables[id] = %v, want issue-123", reqBody.Variables["id"])
		}
		if reqBody.Variables["stateId"] != "state-456" {
			t.Errorf("variables[stateId] = %v, want state-456", reqBody.Variables["stateId"])
		}

		resp := GraphQLResponse{
			Data: json.RawMessage(`{"issueUpdate": {"success": true}}`),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestableClient(server.URL, testutil.FakeLinearAPIKey)

	err := client.updateIssueState(context.Background(), "issue-123", "state-456")
	if err != nil {
		t.Fatalf("UpdateIssueState failed: %v", err)
	}
}

func TestUpdateIssueState_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := GraphQLResponse{
			Errors: []GraphQLError{
				{Message: "Cannot update issue state"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestableClient(server.URL, testutil.FakeLinearAPIKey)

	err := client.updateIssueState(context.Background(), "issue-123", "invalid-state")
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

func TestAddComment_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		// Verify mutation
		if !contains(reqBody.Query, "commentCreate") {
			t.Errorf("query should contain 'commentCreate', got: %s", reqBody.Query)
		}

		// Verify variables
		if reqBody.Variables["issueId"] != "issue-123" {
			t.Errorf("variables[issueId] = %v, want issue-123", reqBody.Variables["issueId"])
		}
		if reqBody.Variables["body"] != "This is a test comment" {
			t.Errorf("variables[body] = %v, want 'This is a test comment'", reqBody.Variables["body"])
		}

		resp := GraphQLResponse{
			Data: json.RawMessage(`{"commentCreate": {"success": true}}`),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestableClient(server.URL, testutil.FakeLinearAPIKey)

	err := client.addComment(context.Background(), "issue-123", "This is a test comment")
	if err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}
}

func TestAddComment_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := GraphQLResponse{
			Errors: []GraphQLError{
				{Message: "Cannot add comment to issue"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestableClient(server.URL, testutil.FakeLinearAPIKey)

	err := client.addComment(context.Background(), "issue-123", "comment")
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

// TestClientMethodSignatures verifies all client methods have correct signatures
func TestClientMethodSignatures(t *testing.T) {
	// Use the no-retry test constructor pointed at an unreachable address so the
	// signature-compile calls fail fast instead of retrying against the real API.
	client := NewClientWithBaseURL(testutil.FakeLinearAPIKey, "http://127.0.0.1:0")
	ctx := context.Background()

	// These verify signatures compile correctly (actual calls will fail without mock server)
	var err error

	// Execute
	err = client.Execute(ctx, "query {}", nil, nil)
	_ = err

	// GetIssue
	_, err = client.GetIssue(ctx, "id")
	_ = err

	// UpdateIssueState
	err = client.UpdateIssueState(ctx, "issue", "state")
	_ = err

	// AddComment
	err = client.AddComment(ctx, "issue", "body")
	_ = err

	// CreateIssue
	_, _, err = client.CreateIssue(ctx, "parent", "title", "body", []string{"label"})
	_ = err
}
