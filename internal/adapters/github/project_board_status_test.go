package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestUpdateProjectItemStatus_EmptyStatus(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)
	pbs := &ProjectBoardSync{
		client: client,
		config: &ProjectBoardConfig{Enabled: true, ProjectNumber: 1, StatusField: "Status"},
		owner:  "testorg",
	}

	err := pbs.UpdateProjectItemStatus(context.Background(), "ISSUE_123", "")
	if err != nil {
		t.Errorf("expected nil for empty status, got %v", err)
	}
}

func TestUpdateProjectItemStatus_FullFlow(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		var resp string
		switch {
		case strings.Contains(req.Query, "organization"):
			resp = `{"data":{"organization":{"projectV2":{"id":"PVT_org123"}}}}`
			requestCount++
		case strings.Contains(req.Query, "field(name:"):
			resp = `{"data":{"node":{"field":{"id":"PVTSSF_field1","options":[{"id":"OPT_todo","name":"Todo"},{"id":"OPT_indev","name":"In Dev"},{"id":"OPT_done","name":"Done"}]}}}}`
			requestCount++
		case strings.Contains(req.Query, "projectItems"):
			resp = `{"data":{"node":{"projectItems":{"nodes":[{"id":"PVTI_item1","project":{"id":"PVT_org123"}}]}}}}`
			requestCount++
		case strings.Contains(req.Query, "updateProjectV2ItemFieldValue"):
			resp = `{"data":{"updateProjectV2ItemFieldValue":{"projectV2Item":{"id":"PVTI_item1"}}}}`
			requestCount++
		default:
			t.Fatalf("unexpected query: %s", req.Query)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	pbs := NewProjectBoardSync(client, &ProjectBoardConfig{
		Enabled:       true,
		ProjectNumber: 5,
		StatusField:   "Status",
	}, "testorg")

	err := pbs.UpdateProjectItemStatus(context.Background(), "ISSUE_node1", "In Dev")
	if err != nil {
		t.Fatalf("UpdateProjectItemStatus() error = %v", err)
	}

	if requestCount != 4 {
		t.Errorf("expected 4 GraphQL requests, got %d", requestCount)
	}
}

func TestUpdateProjectItemStatus_CachesIDs(t *testing.T) {
	var resolveCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		var resp string
		switch {
		case strings.Contains(req.Query, "organization"):
			resolveCount.Add(1)
			resp = `{"data":{"organization":{"projectV2":{"id":"PVT_cached"}}}}`
		case strings.Contains(req.Query, "field(name:"):
			resolveCount.Add(1)
			resp = `{"data":{"node":{"field":{"id":"PVTSSF_f1","options":[{"id":"OPT_done","name":"Done"}]}}}}`
		case strings.Contains(req.Query, "projectItems"):
			resp = `{"data":{"node":{"projectItems":{"nodes":[{"id":"PVTI_i1","project":{"id":"PVT_cached"}}]}}}}`
		case strings.Contains(req.Query, "updateProjectV2ItemFieldValue"):
			resp = `{"data":{"updateProjectV2ItemFieldValue":{"projectV2Item":{"id":"PVTI_i1"}}}}`
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	pbs := NewProjectBoardSync(client, &ProjectBoardConfig{
		Enabled:       true,
		ProjectNumber: 1,
		StatusField:   "Status",
	}, "testorg")

	// Call twice — resolution queries should only happen once.
	for i := 0; i < 2; i++ {
		err := pbs.UpdateProjectItemStatus(context.Background(), "ISSUE_1", "Done")
		if err != nil {
			t.Fatalf("call %d: UpdateProjectItemStatus() error = %v", i+1, err)
		}
	}

	if resolveCount.Load() != 2 { // 1 for org project, 1 for field+options
		t.Errorf("expected 2 resolve requests (cached), got %d", resolveCount.Load())
	}
}

func TestUpdateProjectItemStatus_IssueNotInProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}

		var resp string
		switch {
		case strings.Contains(req.Query, "organization"):
			resp = `{"data":{"organization":{"projectV2":{"id":"PVT_p1"}}}}`
		case strings.Contains(req.Query, "field(name:"):
			resp = `{"data":{"node":{"field":{"id":"PVTSSF_f1","options":[{"id":"OPT_done","name":"Done"}]}}}}`
		case strings.Contains(req.Query, "projectItems"):
			// Issue not in this project — different project ID.
			resp = `{"data":{"node":{"projectItems":{"nodes":[{"id":"PVTI_other","project":{"id":"PVT_different"}}]}}}}`
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	pbs := NewProjectBoardSync(client, &ProjectBoardConfig{
		Enabled:       true,
		ProjectNumber: 1,
		StatusField:   "Status",
	}, "testorg")

	// Should return nil (not error) when issue isn't in project.
	err := pbs.UpdateProjectItemStatus(context.Background(), "ISSUE_orphan", "Done")
	if err != nil {
		t.Errorf("expected nil for issue not in project, got %v", err)
	}
}

func TestUpdateProjectItemStatus_StatusNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}

		var resp string
		switch {
		case strings.Contains(req.Query, "organization"):
			resp = `{"data":{"organization":{"projectV2":{"id":"PVT_p1"}}}}`
		case strings.Contains(req.Query, "field(name:"):
			resp = `{"data":{"node":{"field":{"id":"PVTSSF_f1","options":[{"id":"OPT_todo","name":"Todo"}]}}}}`
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	pbs := NewProjectBoardSync(client, &ProjectBoardConfig{
		Enabled:       true,
		ProjectNumber: 1,
		StatusField:   "Status",
	}, "testorg")

	// "Nonexistent" isn't in the options — should return nil.
	err := pbs.UpdateProjectItemStatus(context.Background(), "ISSUE_1", "Nonexistent")
	if err != nil {
		t.Errorf("expected nil for unknown status, got %v", err)
	}
}

func TestUpdateProjectItemStatus_CaseInsensitiveMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}

		var resp string
		switch {
		case strings.Contains(req.Query, "organization"):
			resp = `{"data":{"organization":{"projectV2":{"id":"PVT_p1"}}}}`
		case strings.Contains(req.Query, "field(name:"):
			resp = `{"data":{"node":{"field":{"id":"PVTSSF_f1","options":[{"id":"OPT_indev","name":"In Dev"}]}}}}`
		case strings.Contains(req.Query, "projectItems"):
			resp = `{"data":{"node":{"projectItems":{"nodes":[{"id":"PVTI_i1","project":{"id":"PVT_p1"}}]}}}}`
		case strings.Contains(req.Query, "updateProjectV2ItemFieldValue"):
			// Verify the correct option ID was resolved.
			vars := req.Variables
			if vars["optionID"] != "OPT_indev" {
				t.Errorf("expected optionID OPT_indev, got %v", vars["optionID"])
			}
			resp = `{"data":{"updateProjectV2ItemFieldValue":{"projectV2Item":{"id":"PVTI_i1"}}}}`
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	pbs := NewProjectBoardSync(client, &ProjectBoardConfig{
		Enabled:       true,
		ProjectNumber: 1,
		StatusField:   "Status",
	}, "testorg")

	// "in dev" should match "In Dev" (case insensitive).
	err := pbs.UpdateProjectItemStatus(context.Background(), "ISSUE_1", "in dev")
	if err != nil {
		t.Fatalf("UpdateProjectItemStatus() error = %v", err)
	}
}

func TestUpdateProjectItemStatus_GraphQLError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":null,"errors":[{"message":"insufficient permissions"}]}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	pbs := NewProjectBoardSync(client, &ProjectBoardConfig{
		Enabled:       true,
		ProjectNumber: 1,
		StatusField:   "Status",
	}, "testorg")

	err := pbs.UpdateProjectItemStatus(context.Background(), "ISSUE_1", "Done")
	if err == nil {
		t.Fatal("expected error for GraphQL failure")
	}
	if !strings.Contains(err.Error(), "insufficient permissions") {
		t.Errorf("error should mention permissions, got: %v", err)
	}
}

func TestUpdateProjectItemStatus_AlreadyInTargetStatus(t *testing.T) {
	var updateCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}

		var resp string
		switch {
		case strings.Contains(req.Query, "organization"):
			resp = `{"data":{"organization":{"projectV2":{"id":"PVT_p1"}}}}`
		case strings.Contains(req.Query, "field(name:"):
			resp = `{"data":{"node":{"field":{"id":"PVTSSF_f1","options":[{"id":"OPT_done","name":"Done"}]}}}}`
		case strings.Contains(req.Query, "projectItems"):
			// Card already sits in the target column (OPT_done).
			resp = `{"data":{"node":{"projectItems":{"nodes":[{"id":"PVTI_i1","project":{"id":"PVT_p1"},"fieldValueByName":{"optionId":"OPT_done"}}]}}}}`
		case strings.Contains(req.Query, "updateProjectV2ItemFieldValue"):
			updateCalled = true
			resp = `{"data":{"updateProjectV2ItemFieldValue":{"projectV2Item":{"id":"PVTI_i1"}}}}`
		default:
			t.Fatalf("unexpected query: %s", req.Query)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	pbs := NewProjectBoardSync(client, &ProjectBoardConfig{
		Enabled:       true,
		ProjectNumber: 1,
		StatusField:   "Status",
	}, "testorg")

	err := pbs.UpdateProjectItemStatus(context.Background(), "ISSUE_1", "Done")
	if err != nil {
		t.Fatalf("UpdateProjectItemStatus() error = %v", err)
	}
	if updateCalled {
		t.Error("expected no field-update mutation when card is already in target status")
	}
}

func TestUpdateProjectItemStatus_DifferentStatusWritesUpdate(t *testing.T) {
	var updateCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}

		var resp string
		switch {
		case strings.Contains(req.Query, "organization"):
			resp = `{"data":{"organization":{"projectV2":{"id":"PVT_p1"}}}}`
		case strings.Contains(req.Query, "field(name:"):
			resp = `{"data":{"node":{"field":{"id":"PVTSSF_f1","options":[{"id":"OPT_todo","name":"Todo"},{"id":"OPT_done","name":"Done"}]}}}}`
		case strings.Contains(req.Query, "projectItems"):
			// Card currently in Todo; target is Done — should write.
			resp = `{"data":{"node":{"projectItems":{"nodes":[{"id":"PVTI_i1","project":{"id":"PVT_p1"},"fieldValueByName":{"optionId":"OPT_todo"}}]}}}}`
		case strings.Contains(req.Query, "updateProjectV2ItemFieldValue"):
			updateCalled = true
			if req.Variables["optionID"] != "OPT_done" {
				t.Errorf("expected optionID OPT_done, got %v", req.Variables["optionID"])
			}
			resp = `{"data":{"updateProjectV2ItemFieldValue":{"projectV2Item":{"id":"PVTI_i1"}}}}`
		default:
			t.Fatalf("unexpected query: %s", req.Query)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	pbs := NewProjectBoardSync(client, &ProjectBoardConfig{
		Enabled:       true,
		ProjectNumber: 1,
		StatusField:   "Status",
	}, "testorg")

	err := pbs.UpdateProjectItemStatus(context.Background(), "ISSUE_1", "Done")
	if err != nil {
		t.Fatalf("UpdateProjectItemStatus() error = %v", err)
	}
	if !updateCalled {
		t.Error("expected a field-update mutation when card is in a different status")
	}
}
