package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestApprovePullRequest(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "success - with comment",
			body:       "LGTM! Great work.",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "success - without comment",
			body:       "",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "not found",
			body:       "",
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
		{
			name:       "forbidden - no permission",
			body:       "",
			statusCode: http.StatusForbidden,
			wantErr:    true,
		},
		{
			name:       "conflict - already reviewed",
			body:       "",
			statusCode: http.StatusUnprocessableEntity,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if r.URL.Path != "/repos/owner/repo/pulls/42/reviews" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}

				if body["event"] != ReviewEventApprove {
					t.Errorf("unexpected event: %s, want %s", body["event"], ReviewEventApprove)
				}
				if tt.body != "" && body["body"] != tt.body {
					t.Errorf("unexpected body: %s, want %s", body["body"], tt.body)
				}

				w.WriteHeader(tt.statusCode)
				if tt.statusCode == http.StatusOK {
					_, _ = w.Write([]byte(`{"id": 123, "state": "APPROVED"}`))
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			err := client.ApprovePullRequest(context.Background(), "owner", "repo", 42, tt.body)

			if (err != nil) != tt.wantErr {
				t.Errorf("ApprovePullRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestListPullRequestReviews(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   interface{}
		wantErr    bool
		wantCount  int
	}{
		{
			name:       "success - multiple reviews",
			statusCode: http.StatusOK,
			response: []*PullRequestReview{
				{ID: 1, User: User{Login: "alice"}, State: ReviewStateApproved},
				{ID: 2, User: User{Login: "bob"}, State: ReviewStateCommented},
			},
			wantErr:   false,
			wantCount: 2,
		},
		{
			name:       "success - no reviews",
			statusCode: http.StatusOK,
			response:   []*PullRequestReview{},
			wantErr:    false,
			wantCount:  0,
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
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				if r.URL.Path != "/repos/owner/repo/pulls/42/reviews" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			reviews, err := client.ListPullRequestReviews(context.Background(), "owner", "repo", 42)

			if (err != nil) != tt.wantErr {
				t.Errorf("ListPullRequestReviews() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(reviews) != tt.wantCount {
				t.Errorf("ListPullRequestReviews() returned %d reviews, want %d", len(reviews), tt.wantCount)
			}
		})
	}
}

func TestHasApprovalReview(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		response     interface{}
		wantApproved bool
		wantApprover string
		wantErr      bool
	}{
		{
			name:       "approved - single review",
			statusCode: http.StatusOK,
			response: []*PullRequestReview{
				{ID: 1, User: User{Login: "alice"}, State: ReviewStateApproved},
			},
			wantApproved: true,
			wantApprover: "alice",
			wantErr:      false,
		},
		{
			name:       "approved - multiple reviews from same user, latest is approval",
			statusCode: http.StatusOK,
			response: []*PullRequestReview{
				{ID: 1, User: User{Login: "alice"}, State: ReviewStateChangesRequested},
				{ID: 2, User: User{Login: "alice"}, State: ReviewStateApproved},
			},
			wantApproved: true,
			wantApprover: "alice",
			wantErr:      false,
		},
		{
			name:       "not approved - changes requested after approval",
			statusCode: http.StatusOK,
			response: []*PullRequestReview{
				{ID: 1, User: User{Login: "alice"}, State: ReviewStateApproved},
				{ID: 2, User: User{Login: "alice"}, State: ReviewStateChangesRequested},
			},
			wantApproved: false,
			wantApprover: "",
			wantErr:      false,
		},
		{
			name:       "approved - one approves, one requests changes",
			statusCode: http.StatusOK,
			response: []*PullRequestReview{
				{ID: 1, User: User{Login: "alice"}, State: ReviewStateApproved},
				{ID: 2, User: User{Login: "bob"}, State: ReviewStateChangesRequested},
			},
			wantApproved: true,
			wantApprover: "alice",
			wantErr:      false,
		},
		{
			name:       "not approved - only comments",
			statusCode: http.StatusOK,
			response: []*PullRequestReview{
				{ID: 1, User: User{Login: "alice"}, State: ReviewStateCommented},
			},
			wantApproved: false,
			wantApprover: "",
			wantErr:      false,
		},
		{
			name:         "not approved - no reviews",
			statusCode:   http.StatusOK,
			response:     []*PullRequestReview{},
			wantApproved: false,
			wantApprover: "",
			wantErr:      false,
		},
		{
			name:       "error - not found",
			statusCode: http.StatusNotFound,
			response:   map[string]string{"message": "Not Found"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			approved, approver, err := client.HasApprovalReview(context.Background(), "owner", "repo", 42)

			if (err != nil) != tt.wantErr {
				t.Errorf("HasApprovalReview() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if approved != tt.wantApproved {
					t.Errorf("HasApprovalReview() approved = %v, want %v", approved, tt.wantApproved)
				}
				if approved && approver != tt.wantApprover {
					t.Errorf("HasApprovalReview() approver = %s, want %s", approver, tt.wantApprover)
				}
			}
		})
	}
}

func TestRequestReviewers(t *testing.T) {
	tests := []struct {
		name          string
		reviewers     []string
		teamReviewers []string
		statusCode    int
		wantErr       bool
		wantSkipped   bool // expect no HTTP call
	}{
		{
			name:       "success - individual reviewers",
			reviewers:  []string{"alice", "bob"},
			statusCode: http.StatusCreated,
		},
		{
			name:          "success - team reviewers",
			teamReviewers: []string{"backend-team"},
			statusCode:    http.StatusCreated,
		},
		{
			name:          "success - both individual and team",
			reviewers:     []string{"alice"},
			teamReviewers: []string{"frontend-team"},
			statusCode:    http.StatusCreated,
		},
		{
			name:        "skip - no reviewers",
			wantSkipped: true,
		},
		{
			name:       "error - not found",
			reviewers:  []string{"nonexistent"},
			statusCode: http.StatusUnprocessableEntity,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if !strings.Contains(r.URL.Path, "/requested_reviewers") {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				var body map[string][]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode request body: %v", err)
				}
				if len(tt.reviewers) > 0 {
					if len(body["reviewers"]) != len(tt.reviewers) {
						t.Errorf("reviewers = %v, want %v", body["reviewers"], tt.reviewers)
					}
				}
				if len(tt.teamReviewers) > 0 {
					if len(body["team_reviewers"]) != len(tt.teamReviewers) {
						t.Errorf("team_reviewers = %v, want %v", body["team_reviewers"], tt.teamReviewers)
					}
				}

				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			err := client.RequestReviewers(context.Background(), "owner", "repo", 42, tt.reviewers, tt.teamReviewers)

			if (err != nil) != tt.wantErr {
				t.Errorf("RequestReviewers() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantSkipped && called {
				t.Error("RequestReviewers() should not make HTTP call when no reviewers specified")
			}
			if !tt.wantSkipped && !tt.wantErr && !called {
				t.Error("RequestReviewers() should have made HTTP call")
			}
		})
	}
}
