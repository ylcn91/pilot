package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestIsUnprocessableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "422 error",
			err:  fmt.Errorf("API error (status 422): Reference does not exist"),
			want: true,
		},
		{
			name: "404 error",
			err:  fmt.Errorf("API error (status 404): Not Found"),
			want: false,
		},
		{
			name: "500 error",
			err:  fmt.Errorf("API error (status 500): Internal Server Error"),
			want: false,
		},
		{
			name: "other error",
			err:  fmt.Errorf("connection refused"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isUnprocessableError(tt.err)
			if got != tt.want {
				t.Errorf("isUnprocessableError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPullRequestComments(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   interface{}
		wantErr    bool
		wantCount  int
	}{
		{
			name:       "success - multiple comments",
			statusCode: http.StatusOK,
			response: []*PRReviewComment{
				{
					ID:        1,
					Body:      "Consider using a constant here",
					Path:      "internal/executor/runner.go",
					Line:      42,
					Side:      "RIGHT",
					User:      User{Login: "reviewer1"},
					CreatedAt: "2026-01-15T10:00:00Z",
					HTMLURL:   "https://github.com/owner/repo/pull/10#discussion_r1",
				},
				{
					ID:        2,
					Body:      "This nil check is redundant",
					Path:      "internal/gateway/server.go",
					Line:      100,
					Side:      "RIGHT",
					User:      User{Login: "reviewer2"},
					CreatedAt: "2026-01-15T11:00:00Z",
					HTMLURL:   "https://github.com/owner/repo/pull/10#discussion_r2",
				},
			},
			wantErr:   false,
			wantCount: 2,
		},
		{
			name:       "success - no comments",
			statusCode: http.StatusOK,
			response:   []*PRReviewComment{},
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
				if r.URL.Path != "/repos/owner/repo/pulls/10/comments" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			comments, err := client.GetPullRequestComments(context.Background(), "owner", "repo", 10)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetPullRequestComments() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(comments) != tt.wantCount {
					t.Errorf("GetPullRequestComments() returned %d comments, want %d", len(comments), tt.wantCount)
				}
				// Verify fields are parsed correctly for success case with comments
				if tt.wantCount > 0 {
					c := comments[0]
					if c.Body != "Consider using a constant here" {
						t.Errorf("comment.Body = %q, want %q", c.Body, "Consider using a constant here")
					}
					if c.Path != "internal/executor/runner.go" {
						t.Errorf("comment.Path = %q, want %q", c.Path, "internal/executor/runner.go")
					}
					if c.Line != 42 {
						t.Errorf("comment.Line = %d, want %d", c.Line, 42)
					}
					if c.Side != "RIGHT" {
						t.Errorf("comment.Side = %q, want %q", c.Side, "RIGHT")
					}
					if c.User.Login != "reviewer1" {
						t.Errorf("comment.User.Login = %q, want %q", c.User.Login, "reviewer1")
					}
				}
			}
		})
	}
}
