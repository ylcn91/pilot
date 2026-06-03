package bitbucket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeToken is an obviously-fake test token. The shared testutil package has no
// Bitbucket-specific constant yet and is outside this package's ownership, so a
// local fake is used (never a realistic token shape — push protection blocks those).
const (
	fakeToken         = "test-bitbucket-token"
	fakeWebhookSecret = "test-bitbucket-webhook-secret"
)

func TestNewClient(t *testing.T) {
	client := NewClient(fakeToken, "myworkspace", "myrepo")
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.token != fakeToken {
		t.Errorf("client.token = %s, want %s", client.token, fakeToken)
	}
	if client.baseURL != defaultBaseURL {
		t.Errorf("client.baseURL = %s, want %s", client.baseURL, defaultBaseURL)
	}
	if client.workspace != "myworkspace" {
		t.Errorf("client.workspace = %s, want myworkspace", client.workspace)
	}
	if client.repo != "myrepo" {
		t.Errorf("client.repo = %s, want myrepo", client.repo)
	}
}

func TestNewClientWithConfig(t *testing.T) {
	cfg := &Config{
		Token:     fakeToken,
		Username:  "alice",
		Workspace: "ws",
		Repo:      "repo",
		BaseURL:   "https://bitbucket.example.com/2.0",
	}
	client := NewClientWithConfig(cfg)
	if client.username != "alice" {
		t.Errorf("client.username = %s, want alice", client.username)
	}
	if client.baseURL != "https://bitbucket.example.com/2.0" {
		t.Errorf("client.baseURL = %s, want override", client.baseURL)
	}
}

func TestNewClientWithBaseURL(t *testing.T) {
	customURL := "https://custom.bitbucket.example.com"
	client := NewClientWithBaseURL(fakeToken, "ws", "repo", customURL)
	if client == nil {
		t.Fatal("NewClientWithBaseURL returned nil")
	}
	if client.baseURL != customURL {
		t.Errorf("client.baseURL = %s, want %s", client.baseURL, customURL)
	}
}

func TestDoRequestBearerAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(Repository{Name: "repo"})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(fakeToken, "ws", "repo", server.URL)
	if _, err := client.GetRepository(context.Background()); err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}
	if gotAuth != "Bearer "+fakeToken {
		t.Errorf("Authorization = %q, want Bearer header", gotAuth)
	}
}

func TestDoRequestBasicAuth(t *testing.T) {
	var gotUser, gotPass string
	var ok bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, ok = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(Repository{Name: "repo"})
	}))
	defer server.Close()

	client := NewClientWithConfig(&Config{
		Token:     fakeToken,
		Username:  "alice",
		Workspace: "ws",
		Repo:      "repo",
		BaseURL:   server.URL,
	})
	if _, err := client.GetRepository(context.Background()); err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}
	if !ok || gotUser != "alice" || gotPass != fakeToken {
		t.Errorf("BasicAuth = (%q, %q, %v), want (alice, token, true)", gotUser, gotPass, ok)
	}
}

func TestGetIssue(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		response     interface{}
		wantErr      bool
		wantDisabled bool
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			response: Issue{
				ID:    42,
				Title: "Test Issue",
				State: StateNew,
			},
			wantErr: false,
		},
		{
			name:         "issue tracker disabled (404)",
			statusCode:   http.StatusNotFound,
			response:     map[string]string{"type": "error"},
			wantErr:      true,
			wantDisabled: true,
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

			client := NewClientWithBaseURL(fakeToken, "ws", "repo", server.URL)
			issue, err := client.GetIssue(context.Background(), 42)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetIssue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantDisabled && err != ErrIssueTrackerDisabled {
				t.Errorf("GetIssue() error = %v, want ErrIssueTrackerDisabled", err)
			}
			if !tt.wantErr && issue.ID != 42 {
				t.Errorf("issue.ID = %d, want 42", issue.ID)
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
			response: IssueList{
				Values: []*Issue{
					{ID: 1, Title: "Issue 1", Kind: "bug"},
					{ID: 2, Title: "Issue 2", Kind: "task"},
				},
			},
			wantErr:   false,
			wantCount: 2,
		},
		{
			name: "success - with state",
			opts: &ListIssuesOptions{
				State: StateNew,
			},
			statusCode: http.StatusOK,
			response: IssueList{
				Values: []*Issue{
					{ID: 1, Title: "Issue 1"},
				},
			},
			wantErr:   false,
			wantCount: 1,
		},
		{
			name:       "issue tracker disabled (404) returns empty",
			opts:       nil,
			statusCode: http.StatusNotFound,
			response:   map[string]string{"type": "error"},
			wantErr:    false,
			wantCount:  0,
		},
		{
			name:       "unauthorized",
			opts:       nil,
			statusCode: http.StatusUnauthorized,
			response:   map[string]string{"type": "error"},
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

			client := NewClientWithBaseURL(fakeToken, "ws", "repo", server.URL)
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

func TestAddIssueComment(t *testing.T) {
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

				var body struct {
					Content struct {
						Raw string `json:"raw"`
					} `json:"content"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}
				if body.Content.Raw != tt.body {
					t.Errorf("unexpected comment body: %s", body.Content.Raw)
				}

				w.WriteHeader(tt.statusCode)
				if tt.statusCode < 300 {
					_ = json.NewEncoder(w).Encode(Comment{ID: 123})
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(fakeToken, "ws", "repo", server.URL)
			comment, err := client.AddIssueComment(context.Background(), 42, tt.body)

			if (err != nil) != tt.wantErr {
				t.Errorf("AddIssueComment() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && comment.ID != 123 {
				t.Errorf("comment.ID = %d, want 123", comment.ID)
			}
		})
	}
}

func TestCreatePRAndExtractURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/pullrequests") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var input PullRequestInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if input.Source == nil || input.Source.Branch == nil || input.Source.Branch.Name != "feature" {
			t.Errorf("unexpected source branch: %+v", input.Source)
		}
		if input.Destination == nil || input.Destination.Branch == nil || input.Destination.Branch.Name != "main" {
			t.Errorf("unexpected destination branch: %+v", input.Destination)
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(PullRequest{
			ID:    7,
			State: PRStateOpen,
			Links: &Links{HTML: &Link{Href: "https://bitbucket.org/ws/repo/pull-requests/7"}},
		})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(fakeToken, "ws", "repo", server.URL)
	url, err := client.CreatePR(context.Background(), "feature", "main", "Title", "Body")
	if err != nil {
		t.Fatalf("CreatePR() error = %v", err)
	}
	if url != "https://bitbucket.org/ws/repo/pull-requests/7" {
		t.Errorf("CreatePR() url = %s, want PR html href", url)
	}
}

func TestMergePullRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/pullrequests/7/merge") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(PullRequest{ID: 7, State: PRStateMerged})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(fakeToken, "ws", "repo", server.URL)
	pr, err := client.MergePullRequest(context.Background(), 7, true)
	if err != nil {
		t.Fatalf("MergePullRequest() error = %v", err)
	}
	if pr.State != PRStateMerged {
		t.Errorf("pr.State = %s, want %s", pr.State, PRStateMerged)
	}
}

func TestGetCommitStatuses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/commit/abc123/statuses") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(CommitStatusList{
			Values: []*CommitStatus{
				{Key: "pipeline", State: BuildSuccessful},
			},
		})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(fakeToken, "ws", "repo", server.URL)
	statuses, err := client.GetCommitStatuses(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("GetCommitStatuses() error = %v", err)
	}
	if len(statuses) != 1 || statuses[0].State != BuildSuccessful {
		t.Errorf("unexpected statuses: %+v", statuses)
	}
}
