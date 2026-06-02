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

func TestGetIssue(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   interface{}
		wantErr    bool
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			response: Issue{
				Number:  42,
				Title:   "Test Issue",
				Body:    "Issue body",
				State:   "open",
				HTMLURL: "https://github.com/owner/repo/issues/42",
			},
			wantErr: false,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			response:   map[string]string{"message": "Not Found"},
			wantErr:    true,
		},
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			response:   map[string]string{"message": "Bad credentials"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/owner/repo/issues/42" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				if r.Header.Get("Authorization") != "Bearer "+testutil.FakeGitHubToken {
					t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
				}
				if r.Header.Get("Accept") != "application/vnd.github+json" {
					t.Errorf("unexpected Accept header: %s", r.Header.Get("Accept"))
				}

				w.WriteHeader(tt.statusCode)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			issue, err := client.GetIssue(context.Background(), "owner", "repo", 42)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetIssue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && issue.Number != 42 {
				t.Errorf("issue.Number = %d, want 42", issue.Number)
			}
		})
	}
}

func TestAddComment(t *testing.T) {
	tests := []struct {
		name        string
		commentBody string
		statusCode  int
		wantErr     bool
	}{
		{
			name:        "success",
			commentBody: "Test comment",
			statusCode:  http.StatusCreated,
			wantErr:     false,
		},
		{
			name:        "server error",
			commentBody: "Test comment",
			statusCode:  http.StatusInternalServerError,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
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
					comment := Comment{
						ID:   123,
						Body: tt.commentBody,
					}
					_ = json.NewEncoder(w).Encode(comment)
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			comment, err := client.AddComment(context.Background(), "owner", "repo", 42, tt.commentBody)

			if (err != nil) != tt.wantErr {
				t.Errorf("AddComment() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && comment.ID != 123 {
				t.Errorf("comment.ID = %d, want 123", comment.ID)
			}
		})
	}
}

func TestAddLabels(t *testing.T) {
	tests := []struct {
		name       string
		labels     []string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "success - single label",
			labels:     []string{"bug"},
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "success - multiple labels",
			labels:     []string{"bug", "pilot", "high-priority"},
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "not found",
			labels:     []string{"bug"},
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}

				var body map[string][]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}

				if len(body["labels"]) != len(tt.labels) {
					t.Errorf("expected %d labels, got %d", len(tt.labels), len(body["labels"]))
				}

				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			err := client.AddLabels(context.Background(), "owner", "repo", 42, tt.labels)

			if (err != nil) != tt.wantErr {
				t.Errorf("AddLabels() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRemoveLabel(t *testing.T) {
	tests := []struct {
		name       string
		label      string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "success",
			label:      "bug",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "not found (OK - label might not exist)",
			label:      "nonexistent",
			statusCode: http.StatusNotFound,
			wantErr:    false, // 404 is OK for RemoveLabel
		},
		{
			name:       "server error",
			label:      "bug",
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
				// Labels are normalized to lowercase in URL path
				expectedPath := "/repos/owner/repo/issues/42/labels/" + strings.ToLower(tt.label)
				if r.URL.Path != expectedPath {
					t.Errorf("unexpected path: %s, want %s", r.URL.Path, expectedPath)
				}
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			err := client.RemoveLabel(context.Background(), "owner", "repo", 42, tt.label)

			if (err != nil) != tt.wantErr {
				t.Errorf("RemoveLabel() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRemoveLabel_NormalizesToLowercase(t *testing.T) {
	var receivedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	_ = client.RemoveLabel(context.Background(), "owner", "repo", 42, "Pilot-Failed")

	expectedPath := "/repos/owner/repo/issues/42/labels/pilot-failed"
	if receivedPath != expectedPath {
		t.Errorf("expected label to be lowercased in path, got: %s, want: %s", receivedPath, expectedPath)
	}
}

func TestUpdateIssueState(t *testing.T) {
	tests := []struct {
		name       string
		state      string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "close issue",
			state:      "closed",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "reopen issue",
			state:      "open",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "not found",
			state:      "closed",
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

				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}

				if body["state"] != tt.state {
					t.Errorf("expected state %s, got %s", tt.state, body["state"])
				}

				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			err := client.UpdateIssueState(context.Background(), "owner", "repo", 42, tt.state)

			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateIssueState() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
