package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewClient(t *testing.T) {
	client := NewClient(testutil.FakeGitLabToken, "namespace/project")
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.token != testutil.FakeGitLabToken {
		t.Errorf("client.token = %s, want %s", client.token, testutil.FakeGitLabToken)
	}
	if client.baseURL != gitlabAPIURL {
		t.Errorf("client.baseURL = %s, want %s", client.baseURL, gitlabAPIURL)
	}
	// Project path should be URL-encoded
	if client.projectID != "namespace%2Fproject" {
		t.Errorf("client.projectID = %s, want namespace%%2Fproject", client.projectID)
	}
}

func TestNewClientWithBaseURL(t *testing.T) {
	customURL := "https://custom.gitlab.example.com"
	client := NewClientWithBaseURL(testutil.FakeGitLabToken, "namespace/project", customURL)
	if client == nil {
		t.Fatal("NewClientWithBaseURL returned nil")
	}
	if client.token != testutil.FakeGitLabToken {
		t.Errorf("client.token = %s, want %s", client.token, testutil.FakeGitLabToken)
	}
	if client.baseURL != customURL {
		t.Errorf("client.baseURL = %s, want %s", client.baseURL, customURL)
	}
}

func TestGetProject(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   interface{}
		wantErr    bool
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			response: Project{
				ID:                12345,
				Name:              "project",
				PathWithNamespace: "namespace/project",
				WebURL:            "https://gitlab.com/namespace/project",
				DefaultBranch:     "main",
			},
			wantErr: false,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			response:   map[string]string{"message": "404 Project Not Found"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				if r.Header.Get("PRIVATE-TOKEN") != testutil.FakeGitLabToken {
					t.Errorf("unexpected auth header: %s", r.Header.Get("PRIVATE-TOKEN"))
				}

				w.WriteHeader(tt.statusCode)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitLabToken, "namespace/project", server.URL)
			project, err := client.GetProject(context.Background())

			if (err != nil) != tt.wantErr {
				t.Errorf("GetProject() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && project.Name != "project" {
				t.Errorf("project.Name = %s, want project", project.Name)
			}
		})
	}
}

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
				ID:          1001,
				IID:         42,
				Title:       "Test Issue",
				Description: "Issue description",
				State:       StateOpened,
				WebURL:      "https://gitlab.com/namespace/project/-/issues/42",
			},
			wantErr: false,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			response:   map[string]string{"message": "404 Issue Not Found"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.Path, "/issues/42") {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}

				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitLabToken, "namespace/project", server.URL)
			issue, err := client.GetIssue(context.Background(), 42)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetIssue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && issue.IID != 42 {
				t.Errorf("issue.IID = %d, want 42", issue.IID)
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
				{IID: 1, Title: "Issue 1"},
				{IID: 2, Title: "Issue 2"},
			},
			wantErr:   false,
			wantCount: 2,
		},
		{
			name: "success - with labels",
			opts: &ListIssuesOptions{
				Labels: []string{"pilot", "bug"},
				State:  StateOpened,
			},
			statusCode: http.StatusOK,
			response: []*Issue{
				{IID: 1, Title: "Issue 1"},
			},
			wantErr:   false,
			wantCount: 1,
		},
		{
			name:       "unauthorized",
			opts:       nil,
			statusCode: http.StatusUnauthorized,
			response:   map[string]string{"message": "401 Unauthorized"},
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
				if !strings.Contains(r.URL.Path, "/issues") {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitLabToken, "namespace/project", server.URL)
			issues, err := client.ListIssues(context.Background(), tt.opts)

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

func TestAddIssueNote(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "success",
			body:       "Test comment",
			statusCode: http.StatusCreated,
			wantErr:    false,
		},
		{
			name:       "server error",
			body:       "Test comment",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}

				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}

				if body["body"] != tt.body {
					t.Errorf("unexpected comment body: %s", body["body"])
				}

				w.WriteHeader(tt.statusCode)
				if tt.statusCode < 300 {
					note := Note{
						ID:   123,
						Body: tt.body,
					}
					_ = json.NewEncoder(w).Encode(note)
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitLabToken, "namespace/project", server.URL)
			note, err := client.AddIssueNote(context.Background(), 42, tt.body)

			if (err != nil) != tt.wantErr {
				t.Errorf("AddIssueNote() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && note.ID != 123 {
				t.Errorf("note.ID = %d, want 123", note.ID)
			}
		})
	}
}
