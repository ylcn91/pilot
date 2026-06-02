package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestMergePullRequest(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		commitTitle string
		statusCode  int
		wantErr     bool
	}{
		{
			name:        "success - squash merge",
			method:      MergeMethodSquash,
			commitTitle: "feat: add new feature (#42)",
			statusCode:  http.StatusOK,
			wantErr:     false,
		},
		{
			name:        "success - merge commit",
			method:      MergeMethodMerge,
			commitTitle: "",
			statusCode:  http.StatusOK,
			wantErr:     false,
		},
		{
			name:        "success - rebase",
			method:      MergeMethodRebase,
			commitTitle: "",
			statusCode:  http.StatusOK,
			wantErr:     false,
		},
		{
			name:        "not mergeable - conflicts",
			method:      MergeMethodSquash,
			commitTitle: "",
			statusCode:  http.StatusMethodNotAllowed,
			wantErr:     true,
		},
		{
			name:        "not found",
			method:      MergeMethodSquash,
			commitTitle: "",
			statusCode:  http.StatusNotFound,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					t.Errorf("expected PUT, got %s", r.Method)
				}
				if r.URL.Path != "/repos/owner/repo/pulls/42/merge" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}

				if body["merge_method"] != tt.method {
					t.Errorf("unexpected merge_method: %s, want %s", body["merge_method"], tt.method)
				}
				if tt.commitTitle != "" && body["commit_title"] != tt.commitTitle {
					t.Errorf("unexpected commit_title: %s, want %s", body["commit_title"], tt.commitTitle)
				}

				w.WriteHeader(tt.statusCode)
				if tt.statusCode == http.StatusOK {
					_, _ = w.Write([]byte(`{"sha": "abc123", "merged": true}`))
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			err := client.MergePullRequest(context.Background(), "owner", "repo", 42, tt.method, tt.commitTitle)

			if (err != nil) != tt.wantErr {
				t.Errorf("MergePullRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestListPullRequests(t *testing.T) {
	tests := []struct {
		name       string
		state      string
		statusCode int
		response   interface{}
		wantErr    bool
		wantCount  int
	}{
		{
			name:       "success - open PRs",
			state:      "open",
			statusCode: http.StatusOK,
			response: []*PullRequest{
				{
					Number:  1,
					Title:   "PR 1",
					Head:    PRRef{Ref: "pilot/GH-100", SHA: "abc123"},
					Base:    PRRef{Ref: "main", SHA: "def456"},
					State:   "open",
					HTMLURL: "https://github.com/owner/repo/pull/1",
				},
				{
					Number:  2,
					Title:   "PR 2",
					Head:    PRRef{Ref: "pilot/GH-101", SHA: "xyz789"},
					Base:    PRRef{Ref: "main", SHA: "def456"},
					State:   "open",
					HTMLURL: "https://github.com/owner/repo/pull/2",
				},
			},
			wantErr:   false,
			wantCount: 2,
		},
		{
			name:       "success - no PRs",
			state:      "open",
			statusCode: http.StatusOK,
			response:   []*PullRequest{},
			wantErr:    false,
			wantCount:  0,
		},
		{
			name:       "success - closed PRs",
			state:      "closed",
			statusCode: http.StatusOK,
			response: []*PullRequest{
				{
					Number: 3,
					Title:  "Closed PR",
					Head:   PRRef{Ref: "feature/old", SHA: "old123"},
					Base:   PRRef{Ref: "main", SHA: "def456"},
					State:  "closed",
				},
			},
			wantErr:   false,
			wantCount: 1,
		},
		{
			name:       "unauthorized",
			state:      "open",
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
				expectedPath := "/repos/owner/repo/pulls"
				if !strings.HasPrefix(r.URL.Path, expectedPath) {
					t.Errorf("unexpected path: %s, want prefix %s", r.URL.Path, expectedPath)
				}

				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			prs, err := client.ListPullRequests(context.Background(), "owner", "repo", tt.state)

			if (err != nil) != tt.wantErr {
				t.Errorf("ListPullRequests() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(prs) != tt.wantCount {
				t.Errorf("ListPullRequests() returned %d PRs, want %d", len(prs), tt.wantCount)
			}
		})
	}
}

func TestDeleteBranch(t *testing.T) {
	tests := []struct {
		name       string
		branch     string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "success - branch deleted",
			branch:     "pilot/GH-123",
			statusCode: http.StatusNoContent,
			wantErr:    false,
		},
		{
			name:       "success - branch already deleted (404)",
			branch:     "pilot/GH-456",
			statusCode: http.StatusNotFound,
			wantErr:    false, // 404 is OK - branch may have been deleted by GitHub setting
		},
		{
			name:       "success - branch already deleted (422)",
			branch:     "pilot/GH-789",
			statusCode: http.StatusUnprocessableEntity,
			wantErr:    false, // 422 is OK - branch reference doesn't exist
		},
		{
			name:       "server error",
			branch:     "pilot/GH-999",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("expected DELETE, got %s", r.Method)
				}
				// r.URL.RawPath preserves percent-encoding; verify slash is encoded
				expectedRaw := "/repos/owner/repo/git/refs/heads/" + url.PathEscape(tt.branch)
				if r.URL.RawPath != "" {
					if r.URL.RawPath != expectedRaw {
						t.Errorf("unexpected raw path: %s, want %s", r.URL.RawPath, expectedRaw)
					}
				}
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			err := client.DeleteBranch(context.Background(), "owner", "repo", tt.branch)

			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteBranch() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestListPullRequests_Pagination(t *testing.T) {
	// Build two pages: page 1 has 100 PRs, page 2 has 3 PRs.
	page1 := make([]*PullRequest, 100)
	for i := range page1 {
		n := i + 1
		page1[i] = &PullRequest{
			Number:  n,
			HTMLURL: fmt.Sprintf("https://github.com/owner/repo/pull/%d", n),
			Head:    PRRef{Ref: fmt.Sprintf("pilot/GH-%d", n), SHA: "sha"},
		}
	}
	page2 := []*PullRequest{
		{Number: 101, HTMLURL: "url101", Head: PRRef{Ref: "pilot/GH-101", SHA: "sha"}},
		{Number: 102, HTMLURL: "url102", Head: PRRef{Ref: "pilot/GH-102", SHA: "sha"}},
		{Number: 103, HTMLURL: "url103", Head: PRRef{Ref: "pilot/GH-103", SHA: "sha"}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/pulls" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		q := r.URL.Query()
		pageParam := q.Get("page")
		perPage := q.Get("per_page")
		if perPage != "100" {
			t.Errorf("per_page = %q, want 100", perPage)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if pageParam == "2" {
			_ = json.NewEncoder(w).Encode(page2)
		} else {
			_ = json.NewEncoder(w).Encode(page1)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	prs, err := client.ListPullRequests(context.Background(), "owner", "repo", "open")
	if err != nil {
		t.Fatalf("ListPullRequests() error = %v", err)
	}
	if len(prs) != 103 {
		t.Errorf("got %d PRs, want 103", len(prs))
	}
}

// TestListPullRequests_SinglePage verifies that a response shorter than
// per_page stops pagination after one request.
func TestListPullRequests_SinglePage(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		prs := []*PullRequest{
			{Number: 1, HTMLURL: "url1", Head: PRRef{Ref: "pilot/GH-1", SHA: "sha"}},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(prs)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	prs, err := client.ListPullRequests(context.Background(), "owner", "repo", "open")
	if err != nil {
		t.Fatalf("ListPullRequests() error = %v", err)
	}
	if len(prs) != 1 {
		t.Errorf("got %d PRs, want 1", len(prs))
	}
	if calls != 1 {
		t.Errorf("server called %d times, want 1 (single page should stop)", calls)
	}
}
