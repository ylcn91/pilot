package linear

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestCreateIssue_Success(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var reqBody GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		// First call: GetIssue for parent context
		if callCount == 1 {
			if !contains(reqBody.Query, "issue(id: $id)") {
				t.Errorf("first query should fetch parent issue, got: %s", reqBody.Query)
			}
			if reqBody.Variables["id"] != "parent-123" {
				t.Errorf("variables[id] = %v, want parent-123", reqBody.Variables["id"])
			}

			resp := GraphQLResponse{
				Data: json.RawMessage(`{
					"issue": {
						"id": "parent-123",
						"identifier": "APP-42",
						"title": "Parent issue",
						"description": "Parent description",
						"priority": 1,
						"state": {"id": "state-1", "name": "In Progress", "type": "started"},
						"labels": {"nodes": []},
						"assignee": null,
						"project": {"id": "project-1", "name": "Main Project"},
						"team": {"id": "team-1", "name": "Engineering", "key": "ENG"},
						"createdAt": "2024-01-15T10:00:00Z",
						"updatedAt": "2024-01-16T12:00:00Z"
					}
				}`),
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Second call: GetOrCreateLabel for "Pilot" label
		if callCount == 2 {
			if !contains(reqBody.Query, "issueLabels") {
				t.Errorf("second query should fetch labels, got: %s", reqBody.Query)
			}
			if reqBody.Variables["name"] != "Pilot" {
				t.Errorf("variables[name] = %v, want Pilot", reqBody.Variables["name"])
			}

			resp := GraphQLResponse{
				Data: json.RawMessage(`{
					"issueLabels": {
						"nodes": [{"id": "label-pilot", "name": "Pilot"}]
					}
				}`),
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Third call: issueCreate mutation
		if callCount == 3 {
			if !contains(reqBody.Query, "issueCreate") {
				t.Errorf("third query should create issue, got: %s", reqBody.Query)
			}

			// Verify variables
			if reqBody.Variables["teamId"] != "team-1" {
				t.Errorf("variables[teamId] = %v, want team-1", reqBody.Variables["teamId"])
			}
			if reqBody.Variables["title"] != "Sub issue title" {
				t.Errorf("variables[title] = %v, want 'Sub issue title'", reqBody.Variables["title"])
			}
			expectedDesc := "Parent: parent-123\n\nSub issue description"
			if reqBody.Variables["description"] != expectedDesc {
				t.Errorf("variables[description] = %v, want %q", reqBody.Variables["description"], expectedDesc)
			}
			if reqBody.Variables["projectId"] != "project-1" {
				t.Errorf("variables[projectId] = %v, want project-1", reqBody.Variables["projectId"])
			}

			// Verify labelIds includes pilot label
			labelIds, ok := reqBody.Variables["labelIds"].([]interface{})
			if !ok || len(labelIds) != 1 || labelIds[0] != "label-pilot" {
				t.Errorf("variables[labelIds] = %v, want [\"label-pilot\"]", reqBody.Variables["labelIds"])
			}

			resp := GraphQLResponse{
				Data: json.RawMessage(`{
					"issueCreate": {
						"success": true,
						"issue": {
							"id": "new-issue-123",
							"identifier": "APP-123",
							"url": "https://linear.app/team/issue/APP-123"
						}
					}
				}`),
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		t.Errorf("unexpected call count: %d", callCount)
	}))
	defer server.Close()

	client := newTestableCreateIssueClient(server.URL, testutil.FakeLinearAPIKey)

	identifier, url, err := client.CreateIssue(context.Background(), "parent-123", "Sub issue title", "Sub issue description", []string{})
	if err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	if identifier != "APP-123" {
		t.Errorf("identifier = %s, want APP-123", identifier)
	}
	if url != "https://linear.app/team/issue/APP-123" {
		t.Errorf("url = %s, want https://linear.app/team/issue/APP-123", url)
	}
	if callCount != 3 {
		t.Errorf("expected 3 API calls, got %d", callCount)
	}
}

func TestCreateIssue_ParentNotFound(t *testing.T) {
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

	client := newTestableCreateIssueClient(server.URL, testutil.FakeLinearAPIKey)

	_, _, err := client.CreateIssue(context.Background(), "nonexistent", "Title", "Description", []string{})
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !contains(err.Error(), "failed to fetch parent issue") {
		t.Errorf("error = %v, want to contain 'failed to fetch parent issue'", err)
	}
}

func TestCreateIssue_CreateFails(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		// First two calls succeed (GetIssue and GetLabel)
		if callCount <= 2 {
			if callCount == 1 {
				// GetIssue response
				resp := GraphQLResponse{
					Data: json.RawMessage(`{
						"issue": {
							"id": "parent-123",
							"identifier": "APP-42",
							"title": "Parent",
							"description": "",
							"priority": 1,
							"state": {"id": "state-1", "name": "Open", "type": "unstarted"},
							"labels": {"nodes": []},
							"assignee": null,
							"project": null,
							"team": {"id": "team-1", "name": "Engineering", "key": "ENG"},
							"createdAt": "2024-01-15T10:00:00Z",
							"updatedAt": "2024-01-16T12:00:00Z"
						}
					}`),
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
			} else {
				// GetLabel response
				resp := GraphQLResponse{
					Data: json.RawMessage(`{
						"issueLabels": {
							"nodes": [{"id": "label-pilot", "name": "Pilot"}]
						}
					}`),
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
			}
			return
		}

		// Third call fails (issueCreate)
		resp := GraphQLResponse{
			Errors: []GraphQLError{
				{Message: "Cannot create issue in this team"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestableCreateIssueClient(server.URL, testutil.FakeLinearAPIKey)

	_, _, err := client.CreateIssue(context.Background(), "parent-123", "Title", "Description", []string{})
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !contains(err.Error(), "failed to create issue") {
		t.Errorf("error = %v, want to contain 'failed to create issue'", err)
	}
}
