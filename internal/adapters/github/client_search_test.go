package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestSearchMergedPRsForIssue(t *testing.T) {
	tests := []struct {
		name        string
		issueNumber int
		statusCode  int
		response    string
		wantFound   bool
		wantErr     bool
	}{
		{
			name:        "merged PRs exist",
			issueNumber: 42,
			statusCode:  http.StatusOK,
			response:    `{"total_count": 2}`,
			wantFound:   true,
		},
		{
			name:        "no merged PRs",
			issueNumber: 99,
			statusCode:  http.StatusOK,
			response:    `{"total_count": 0}`,
			wantFound:   false,
		},
		{
			name:        "open PRs only - not counted",
			issueNumber: 55,
			statusCode:  http.StatusOK,
			response:    `{"total_count": 0}`,
			wantFound:   false,
		},
		{
			name:        "API error",
			issueNumber: 10,
			statusCode:  http.StatusForbidden,
			response:    `{"message": "rate limit"}`,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.URL.Path, "/search/issues") {
					t.Errorf("unexpected path: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				q := r.URL.Query().Get("q")
				expectedQ := fmt.Sprintf("repo:owner/repo GH-%d in:title is:pr is:merged", tt.issueNumber)
				if q != expectedQ {
					t.Errorf("query = %q, want %q", q, expectedQ)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			found, err := client.SearchMergedPRsForIssue(context.Background(), "owner", "repo", tt.issueNumber)

			if (err != nil) != tt.wantErr {
				t.Errorf("SearchMergedPRsForIssue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && found != tt.wantFound {
				t.Errorf("SearchMergedPRsForIssue() = %v, want %v", found, tt.wantFound)
			}
		})
	}
}

func TestFindMergedPRByBranch(t *testing.T) {
	tests := []struct {
		name       string
		branch     string
		response   string
		wantFound  bool
		wantErr    bool
		statusCode int
	}{
		{
			name:       "merged PR on branch",
			branch:     "pilot/GH-42",
			statusCode: http.StatusOK,
			response:   `[{"number": 100, "merged_at": "2026-04-17T14:01:40Z"}]`,
			wantFound:  true,
		},
		{
			name:       "closed but not merged",
			branch:     "pilot/GH-43",
			statusCode: http.StatusOK,
			response:   `[{"number": 101, "merged_at": ""}]`,
			wantFound:  false,
		},
		{
			name:       "no PRs on branch",
			branch:     "pilot/GH-44",
			statusCode: http.StatusOK,
			response:   `[]`,
			wantFound:  false,
		},
		{
			name:       "API error",
			branch:     "pilot/GH-45",
			statusCode: http.StatusForbidden,
			response:   `{"message":"rate limit"}`,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/owner/repo/pulls" {
					t.Errorf("unexpected path: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if got := r.URL.Query().Get("head"); got != "owner:"+tt.branch {
					t.Errorf("head filter = %q, want %q", got, "owner:"+tt.branch)
				}
				if got := r.URL.Query().Get("state"); got != "closed" {
					t.Errorf("state = %q, want closed", got)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			found, err := client.FindMergedPRByBranch(context.Background(), "owner", "repo", tt.branch)
			if (err != nil) != tt.wantErr {
				t.Errorf("FindMergedPRByBranch() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && found != tt.wantFound {
				t.Errorf("FindMergedPRByBranch() = %v, want %v", found, tt.wantFound)
			}
		})
	}
}

func TestSearchOpenSubIssues(t *testing.T) {
	tests := []struct {
		name       string
		parentNum  int
		statusCode int
		response   string
		wantCount  int
		wantErr    bool
	}{
		{
			name:       "parent with open siblings",
			parentNum:  100,
			statusCode: http.StatusOK,
			response:   `{"total_count": 3}`,
			wantCount:  3,
		},
		{
			name:       "all siblings closed",
			parentNum:  200,
			statusCode: http.StatusOK,
			response:   `{"total_count": 0}`,
			wantCount:  0,
		},
		{
			name:       "API error",
			parentNum:  300,
			statusCode: http.StatusForbidden,
			response:   `{"message": "rate limit"}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.URL.Path, "/search/issues") {
					t.Errorf("unexpected path: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				q := r.URL.Query().Get("q")
				expectedQ := fmt.Sprintf(`repo:owner/repo "Parent: GH-%d" is:issue is:open`, tt.parentNum)
				if q != expectedQ {
					t.Errorf("query = %q, want %q", q, expectedQ)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			count, err := client.SearchOpenSubIssues(context.Background(), "owner", "repo", tt.parentNum)

			if (err != nil) != tt.wantErr {
				t.Errorf("SearchOpenSubIssues() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && count != tt.wantCount {
				t.Errorf("SearchOpenSubIssues() = %d, want %d", count, tt.wantCount)
			}
		})
	}
}

func TestSearchPRsForIssue(t *testing.T) {
	tests := []struct {
		name        string
		issueNumber int
		statusCode  int
		response    string
		wantPRNums  []int
		wantErr     bool
	}{
		{
			name:        "two PRs found",
			issueNumber: 42,
			statusCode:  http.StatusOK,
			response: `{"total_count":2,"items":[
				{"id":101,"number":10,"title":"fix: closes #42","state":"open","html_url":"https://github.com/owner/repo/pull/10","pull_request":{"merged_at":""}},
				{"id":102,"number":11,"title":"feat: implements #42","state":"closed","html_url":"https://github.com/owner/repo/pull/11","pull_request":{"merged_at":"2026-04-01T12:00:00Z"}}
			]}`,
			wantPRNums: []int{10, 11},
		},
		{
			name:        "no PRs",
			issueNumber: 99,
			statusCode:  http.StatusOK,
			response:    `{"total_count":0,"items":[]}`,
			wantPRNums:  []int{},
		},
		{
			name:        "API error",
			issueNumber: 1,
			statusCode:  http.StatusForbidden,
			response:    `{"message":"rate limit exceeded"}`,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.URL.Path, "/search/issues") {
					t.Errorf("unexpected path: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				q := r.URL.Query().Get("q")
				expectedQ := fmt.Sprintf("repo:owner/repo is:pr #%d", tt.issueNumber)
				if q != expectedQ {
					t.Errorf("query = %q, want %q", q, expectedQ)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			prs, err := client.SearchPRsForIssue(context.Background(), "owner", "repo", tt.issueNumber)

			if (err != nil) != tt.wantErr {
				t.Errorf("SearchPRsForIssue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if len(prs) != len(tt.wantPRNums) {
				t.Fatalf("SearchPRsForIssue() returned %d PRs, want %d", len(prs), len(tt.wantPRNums))
			}
			for i, pr := range prs {
				if pr.Number != tt.wantPRNums[i] {
					t.Errorf("prs[%d].Number = %d, want %d", i, pr.Number, tt.wantPRNums[i])
				}
			}
			// Verify merged flag is set correctly for second PR in "two PRs found" case
			if tt.name == "two PRs found" {
				if prs[0].Merged {
					t.Error("prs[0].Merged should be false (merged_at empty)")
				}
				if !prs[1].Merged {
					t.Error("prs[1].Merged should be true (merged_at set)")
				}
				if prs[1].MergedAt != "2026-04-01T12:00:00Z" {
					t.Errorf("prs[1].MergedAt = %q, want 2026-04-01T12:00:00Z", prs[1].MergedAt)
				}
			}
		})
	}
}

// TestListPullRequests_Pagination verifies that ListPullRequests fetches
// multiple pages when a page returns the full per_page count.
