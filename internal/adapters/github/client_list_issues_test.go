package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestGetRepository(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   interface{}
		wantErr    bool
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			response: Repository{
				ID:       12345,
				Name:     "repo",
				FullName: "owner/repo",
				Owner:    User{Login: "owner"},
				CloneURL: "https://github.com/owner/repo.git",
			},
			wantErr: false,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			response:   map[string]string{"message": "Not Found"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/owner/repo" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			repo, err := client.GetRepository(context.Background(), "owner", "repo")

			if (err != nil) != tt.wantErr {
				t.Errorf("GetRepository() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && repo.Name != "repo" {
				t.Errorf("repo.Name = %s, want repo", repo.Name)
			}
		})
	}
}

func TestListIssues(t *testing.T) {
	tests := []struct {
		name       string
		opts       *ListIssuesOptions
		statusCode int
		response   interface{}
		wantErr    bool
		wantCount  int
	}{
		{
			name:       "success - no options",
			opts:       nil,
			statusCode: http.StatusOK,
			response: []*Issue{
				{Number: 1, Title: "Issue 1"},
				{Number: 2, Title: "Issue 2"},
			},
			wantErr:   false,
			wantCount: 2,
		},
		{
			name: "success - with labels",
			opts: &ListIssuesOptions{
				Labels: []string{"pilot", "bug"},
				State:  StateOpen,
			},
			statusCode: http.StatusOK,
			response: []*Issue{
				{Number: 1, Title: "Issue 1", Labels: []Label{{Name: "pilot"}, {Name: "bug"}}},
				{Number: 2, Title: "Issue 2", Labels: []Label{{Name: "pilot"}}}, // missing bug, won't match
			},
			wantErr:   false,
			wantCount: 1, // Only issue 1 has both labels
		},
		{
			name: "labels filtered case-insensitively in code",
			opts: &ListIssuesOptions{
				Labels: []string{"pilot"},
				State:  StateOpen,
			},
			statusCode: http.StatusOK,
			response: []*Issue{
				{Number: 1, Title: "Issue 1", Labels: []Label{{Name: "Pilot"}}}, // uppercase
				{Number: 2, Title: "Issue 2", Labels: []Label{{Name: "bug"}}},   // no match
			},
			wantErr:   false,
			wantCount: 1, // Only issue 1 matches (Pilot == pilot case-insensitive)
		},
		{
			name: "success - with since",
			opts: &ListIssuesOptions{
				Since: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				Sort:  "updated",
			},
			statusCode: http.StatusOK,
			response:   []*Issue{},
			wantErr:    false,
			wantCount:  0,
		},
		{
			name:       "unauthorized",
			opts:       nil,
			statusCode: http.StatusUnauthorized,
			response:   map[string]string{"message": "Bad credentials"},
			wantErr:    true,
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				if !strings.HasPrefix(r.URL.Path, "/repos/owner/repo/issues") {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				// Note: Query params are appended to path in this implementation
				// so we just verify the request was made to the correct base path

				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			issues, err := client.ListIssues(context.Background(), "owner", "repo", tt.opts)

			if (err != nil) != tt.wantErr {
				t.Errorf("ListIssues() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(issues) != tt.wantCount {
				t.Errorf("ListIssues() returned %d issues, want %d", len(issues), tt.wantCount)
			}
		})
	}
}

func TestListIssues_LabelsFilteredCaseInsensitively(t *testing.T) {
	var receivedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.String()
		w.WriteHeader(http.StatusOK)
		// Return issues with various label cases
		_ = json.NewEncoder(w).Encode([]*Issue{
			{Number: 1, Title: "Has pilot lowercase", Labels: []Label{{Name: "pilot"}}},
			{Number: 2, Title: "Has Pilot uppercase", Labels: []Label{{Name: "Pilot"}}},
			{Number: 3, Title: "Has PILOT all caps", Labels: []Label{{Name: "PILOT"}}},
			{Number: 4, Title: "No pilot label", Labels: []Label{{Name: "bug"}}},
		})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	issues, err := client.ListIssues(context.Background(), "owner", "repo", &ListIssuesOptions{
		Labels: []string{"Pilot"}, // Request with mixed case
	})
	if err != nil {
		t.Fatalf("ListIssues() error = %v", err)
	}

	// Verify labels are NOT passed to API (filtered in code instead)
	if strings.Contains(receivedPath, "labels=") {
		t.Errorf("labels should not be in API query (filtered in code), got: %s", receivedPath)
	}

	// Should return 3 issues (all with pilot/Pilot/PILOT label, excluding the one with only "bug")
	if len(issues) != 3 {
		t.Errorf("expected 3 issues with pilot label (case-insensitive), got %d", len(issues))
	}

	// Verify the correct issues were returned
	for _, issue := range issues {
		if issue.Number == 4 {
			t.Errorf("issue #4 should not be returned (has 'bug' not 'pilot')")
		}
	}
}

// TestListIssues_Paginates verifies C6 (TASK-346): ListIssues requests per_page=100
// and follows pages until a short page, returning the full set (not just the first ~30).
func TestListIssues_Paginates(t *testing.T) {
	var perPageSeen string
	var pageRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageRequests++
		perPageSeen = r.URL.Query().Get("per_page")
		var batch []*Issue
		switch r.URL.Query().Get("page") {
		case "1":
			batch = make([]*Issue, 100) // full page → keep paging
			for i := range batch {
				batch[i] = &Issue{Number: i + 1}
			}
		case "2":
			batch = make([]*Issue, 15) // short page → stop
			for i := range batch {
				batch[i] = &Issue{Number: 101 + i}
			}
		default:
			batch = []*Issue{}
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(batch)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	issues, err := client.ListIssues(context.Background(), "owner", "repo", nil)
	if err != nil {
		t.Fatalf("ListIssues() error = %v", err)
	}
	if len(issues) != 115 {
		t.Errorf("expected 115 issues across 2 pages, got %d", len(issues))
	}
	if perPageSeen != "100" {
		t.Errorf("expected per_page=100, got %q", perPageSeen)
	}
	if pageRequests != 2 {
		t.Errorf("expected 2 page requests (100 + short 15 stops paging), got %d", pageRequests)
	}
}

func TestHasLabel(t *testing.T) {
	tests := []struct {
		name      string
		issue     *Issue
		labelName string
		want      bool
	}{
		{
			name: "has label - first",
			issue: &Issue{
				Labels: []Label{
					{Name: "pilot"},
					{Name: "bug"},
				},
			},
			labelName: "pilot",
			want:      true,
		},
		{
			name: "has label - last",
			issue: &Issue{
				Labels: []Label{
					{Name: "bug"},
					{Name: "enhancement"},
					{Name: "pilot"},
				},
			},
			labelName: "pilot",
			want:      true,
		},
		{
			name: "does not have label",
			issue: &Issue{
				Labels: []Label{
					{Name: "bug"},
					{Name: "enhancement"},
				},
			},
			labelName: "pilot",
			want:      false,
		},
		{
			name: "empty labels",
			issue: &Issue{
				Labels: []Label{},
			},
			labelName: "pilot",
			want:      false,
		},
		{
			name:      "nil labels",
			issue:     &Issue{},
			labelName: "pilot",
			want:      false,
		},
		{
			name: "case insensitive",
			issue: &Issue{
				Labels: []Label{
					{Name: "Pilot"},
				},
			},
			labelName: "pilot",
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasLabel(tt.issue, tt.labelName)
			if got != tt.want {
				t.Errorf("HasLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}
