package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestCreatePullRequest(t *testing.T) {
	tests := []struct {
		name       string
		input      *PullRequestInput
		statusCode int
		wantErr    bool
	}{
		{
			name: "success",
			input: &PullRequestInput{
				Title: "Add new feature",
				Body:  "This PR adds a new feature",
				Head:  "feature/new-feature",
				Base:  "main",
			},
			statusCode: http.StatusCreated,
			wantErr:    false,
		},
		{
			name: "success - draft PR",
			input: &PullRequestInput{
				Title: "WIP: New feature",
				Head:  "feature/wip",
				Base:  "main",
				Draft: true,
			},
			statusCode: http.StatusCreated,
			wantErr:    false,
		},
		{
			name: "unprocessable entity - branch doesn't exist",
			input: &PullRequestInput{
				Title: "Add new feature",
				Head:  "nonexistent-branch",
				Base:  "main",
			},
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
				if r.URL.Path != "/repos/owner/repo/pulls" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				var body PullRequestInput
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}

				if body.Title != tt.input.Title {
					t.Errorf("unexpected title: %s", body.Title)
				}

				w.WriteHeader(tt.statusCode)
				if tt.statusCode < 300 {
					result := PullRequest{
						ID:      11111,
						Number:  42,
						Title:   body.Title,
						Head:    PRRef{Ref: body.Head, SHA: "abc123"},
						Base:    PRRef{Ref: body.Base, SHA: "def456"},
						State:   "open",
						HTMLURL: "https://github.com/owner/repo/pull/42",
					}
					_ = json.NewEncoder(w).Encode(result)
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			result, err := client.CreatePullRequest(context.Background(), "owner", "repo", tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("CreatePullRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result.Number != 42 {
				t.Errorf("result.Number = %d, want 42", result.Number)
			}
		})
	}
}

func TestGetPullRequest(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   interface{}
		wantErr    bool
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			response: PullRequest{
				ID:      11111,
				Number:  42,
				Title:   "Test PR",
				Head:    PRRef{Ref: "feature-branch", SHA: "abc123"},
				Base:    PRRef{Ref: "main", SHA: "def456"},
				State:   "open",
				HTMLURL: "https://github.com/owner/repo/pull/42",
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
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				if r.URL.Path != "/repos/owner/repo/pulls/42" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer "+testutil.FakeGitHubToken {
					t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
				}

				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			result, err := client.GetPullRequest(context.Background(), "owner", "repo", 42)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetPullRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result.Number != 42 {
				t.Errorf("result.Number = %d, want 42", result.Number)
			}
		})
	}
}

func TestClosePullRequest(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch {
					t.Errorf("expected PATCH, got %s", r.Method)
				}
				if r.URL.Path != "/repos/owner/repo/pulls/42" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			err := client.ClosePullRequest(context.Background(), "owner", "repo", 42)

			if (err != nil) != tt.wantErr {
				t.Errorf("ClosePullRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAddPRComment(t *testing.T) {
	tests := []struct {
		name        string
		commentBody string
		statusCode  int
		wantErr     bool
	}{
		{
			name:        "success",
			commentBody: "This is a PR comment",
			statusCode:  http.StatusCreated,
			wantErr:     false,
		},
		{
			name:        "not found",
			commentBody: "Comment on nonexistent PR",
			statusCode:  http.StatusNotFound,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				// PR comments go through the issues API
				if r.URL.Path != "/repos/owner/repo/issues/42/comments" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}

				if body["body"] != tt.commentBody {
					t.Errorf("unexpected comment body: %s", body["body"])
				}

				w.WriteHeader(tt.statusCode)
				if tt.statusCode < 300 {
					result := PRComment{
						ID:      22222,
						Body:    body["body"],
						HTMLURL: "https://github.com/owner/repo/issues/42#issuecomment-22222",
					}
					_ = json.NewEncoder(w).Encode(result)
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			result, err := client.AddPRComment(context.Background(), "owner", "repo", 42, tt.commentBody)

			if (err != nil) != tt.wantErr {
				t.Errorf("AddPRComment() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result.ID != 22222 {
				t.Errorf("result.ID = %d, want 22222", result.ID)
			}
		})
	}
}

func TestUpdatePullRequestBranch(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "success",
			statusCode: http.StatusAccepted,
			wantErr:    false,
		},
		{
			name:       "conflict cannot be resolved",
			statusCode: http.StatusUnprocessableEntity,
			wantErr:    true,
		},
		{
			name:       "forbidden",
			statusCode: http.StatusForbidden,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					t.Errorf("expected PUT, got %s", r.Method)
				}
				if r.URL.Path != "/repos/owner/repo/pulls/42/update-branch" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			err := client.UpdatePullRequestBranch(context.Background(), "owner", "repo", 42)

			if (err != nil) != tt.wantErr {
				t.Errorf("UpdatePullRequestBranch() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
