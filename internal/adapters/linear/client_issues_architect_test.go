package linear

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

// TestCreateIssue_SetsParentID proves the issueCreate mutation now threads
// parentId into the input (so Linear builds a real epic -> sub-issue tree),
// matching the parentID passed to CreateIssue.
func TestCreateIssue_SetsParentID(t *testing.T) {
	callCount := 0
	var createVars map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var reqBody GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch callCount {
		case 1: // GetIssue parent context
			writeJSON(t, w, GraphQLResponse{Data: json.RawMessage(`{
				"issue": {
					"id": "parent-123", "identifier": "APP-42", "title": "Parent",
					"description": "", "priority": 1,
					"state": {"id": "s1", "name": "Open", "type": "unstarted"},
					"labels": {"nodes": []}, "assignee": null,
					"project": {"id": "project-1", "name": "Main"},
					"team": {"id": "team-1", "name": "Eng", "key": "ENG"},
					"createdAt": "2024-01-15T10:00:00Z", "updatedAt": "2024-01-16T12:00:00Z"
				}
			}`)})
		case 2: // GetOrCreateLabel "Pilot"
			writeJSON(t, w, GraphQLResponse{Data: json.RawMessage(`{
				"issueLabels": {"nodes": [{"id": "label-pilot", "name": "Pilot"}]}
			}`)})
		case 3: // issueCreate mutation
			if !contains(reqBody.Query, "parentId") {
				t.Errorf("issueCreate mutation must declare parentId, got: %s", reqBody.Query)
			}
			createVars = reqBody.Variables
			writeJSON(t, w, GraphQLResponse{Data: json.RawMessage(`{
				"issueCreate": {"success": true, "issue": {"id": "new-1", "identifier": "APP-99", "url": "https://linear.app/issue/APP-99"}}
			}`)})
		default:
			t.Errorf("unexpected call count %d", callCount)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeLinearAPIKey, server.URL)
	id, _, err := client.CreateIssue(context.Background(), "parent-123", "Title", "Body", nil)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if id != "APP-99" {
		t.Errorf("identifier = %q, want APP-99", id)
	}
	if createVars["parentId"] != "parent-123" {
		t.Errorf("variables[parentId] = %v, want parent-123", createVars["parentId"])
	}
}

func TestSearchIssuesContaining_CountsMatches(t *testing.T) {
	var gotPhrase interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if !contains(reqBody.Query, "description") || !contains(reqBody.Query, "contains") {
			t.Errorf("search query must filter on description contains, got: %s", reqBody.Query)
		}
		gotPhrase = reqBody.Variables["phrase"]
		writeJSON(t, w, GraphQLResponse{Data: json.RawMessage(`{
			"issues": {"nodes": [{"id": "i1"}]}
		}`)})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeLinearAPIKey, server.URL)
	count, err := client.SearchIssuesContaining(context.Background(), "owner", "repo", "pilot-architect:abc")
	if err != nil {
		t.Fatalf("SearchIssuesContaining: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if gotPhrase != "pilot-architect:abc" {
		t.Errorf("variables[phrase] = %v, want pilot-architect:abc", gotPhrase)
	}
}

func TestSearchIssuesContaining_NoMatches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, GraphQLResponse{Data: json.RawMessage(`{"issues": {"nodes": []}}`)})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeLinearAPIKey, server.URL)
	count, err := client.SearchIssuesContaining(context.Background(), "o", "r", "pilot-architect:missing")
	if err != nil {
		t.Fatalf("SearchIssuesContaining: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

// TestGetIssue_LabelsRoundTrip proves the production Client.GetIssue decodes the
// Linear `labels { nodes { ... } }` connection into Issue.Labels. The struct field
// is a flat []Label, so GetIssue must route the raw response through the
// issueListItem intermediary (matching the labels connection shape the GetIssue
// query selects) rather than unmarshalling straight into Issue — otherwise the
// labels silently drop to empty.
func TestGetIssue_LabelsRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, GraphQLResponse{Data: json.RawMessage(`{
			"issue": {
				"id": "issue-123",
				"identifier": "PROJ-42",
				"title": "Fix the bug",
				"description": "Description of the bug",
				"priority": 2,
				"state": {"id": "state-1", "name": "In Progress", "type": "started"},
				"labels": {
					"nodes": [
						{"id": "label-1", "name": "bug"},
						{"id": "label-2", "name": "pilot"}
					]
				},
				"assignee": {"id": "user-1", "name": "John Doe", "email": "john@example.com"},
				"project": {"id": "project-1", "name": "Main Project"},
				"team": {"id": "team-1", "name": "Engineering", "key": "ENG"},
				"createdAt": "2024-01-15T10:00:00Z",
				"updatedAt": "2024-01-16T12:00:00Z"
			}
		}`)})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeLinearAPIKey, server.URL)
	issue, err := client.GetIssue(context.Background(), "issue-123")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}

	if len(issue.Labels) != 2 {
		t.Fatalf("len(issue.Labels) = %d, want 2 (labels dropped on decode)", len(issue.Labels))
	}
	if issue.Labels[0].ID != "label-1" || issue.Labels[0].Name != "bug" {
		t.Errorf("issue.Labels[0] = %+v, want {label-1 bug}", issue.Labels[0])
	}
	if issue.Labels[1].ID != "label-2" || issue.Labels[1].Name != "pilot" {
		t.Errorf("issue.Labels[1] = %+v, want {label-2 pilot}", issue.Labels[1])
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, resp GraphQLResponse) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
