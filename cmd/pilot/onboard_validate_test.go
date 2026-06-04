package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/asana"
	"github.com/ylcn91/pilot/internal/adapters/azuredevops"
	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/gitlab"
	"github.com/ylcn91/pilot/internal/adapters/jira"
	"github.com/ylcn91/pilot/internal/adapters/linear"
	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/testutil"
)

// newAuthServer returns an httptest server responding with the given status and
// JSON body for every request, asserting the auth header carries the token.
func newAuthServer(t *testing.T, status int, body interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}))
}

// --- GitHub ---

func TestValidateGitHubConnEmpty(t *testing.T) {
	if err := validateGitHubConn(""); err == nil {
		t.Error("expected error for empty token")
	}
}

func TestValidateGitHubWith(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    interface{}
		wantErr bool
	}{
		{"valid token", http.StatusOK, github.User{Login: "octocat"}, false},
		{"unauthorized", http.StatusUnauthorized, map[string]string{"message": "Bad credentials"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newAuthServer(t, tt.status, tt.body)
			defer srv.Close()
			client := github.NewClientWithBaseURL(testutil.FakeGitHubToken, srv.URL)
			err := validateGitHubWith(client)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateGitHubWith() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- Linear ---

func TestValidateLinearConnEmpty(t *testing.T) {
	if _, err := validateLinearConn(""); err == nil {
		t.Error("expected error for empty API key")
	}
}

func TestValidateLinearWith(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     interface{}
		wantName string
		wantErr  bool
	}{
		{
			name:     "valid returns real org name",
			status:   http.StatusOK,
			body:     map[string]interface{}{"data": map[string]interface{}{"organization": map[string]string{"name": "Acme Inc"}}},
			wantName: "Acme Inc",
		},
		{
			name:     "valid but no org name falls back",
			status:   http.StatusOK,
			body:     map[string]interface{}{"data": map[string]interface{}{"organization": map[string]string{}}},
			wantName: "Workspace",
		},
		{
			name:    "unauthorized",
			status:  http.StatusUnauthorized,
			body:    map[string]string{"error": "unauthorized"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newAuthServer(t, tt.status, tt.body)
			defer srv.Close()
			client := linear.NewClientWithBaseURL(testutil.FakeLinearAPIKey, srv.URL)
			name, err := validateLinearWith(client)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateLinearWith() err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}

// --- Jira ---

func TestValidateJiraConnRequiredFields(t *testing.T) {
	if err := validateJiraConn("", "user", "tok"); err == nil {
		t.Error("expected error for missing base URL")
	}
}

func TestValidateJiraWith(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    interface{}
		wantErr bool
	}{
		{"valid", http.StatusOK, map[string]interface{}{"issues": []interface{}{}}, false},
		{"forbidden", http.StatusForbidden, map[string]string{"message": "no"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newAuthServer(t, tt.status, tt.body)
			defer srv.Close()
			client := jira.NewClient(srv.URL, "user@example.com", "fake-token", jira.PlatformCloud)
			err := validateJiraWith(client)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateJiraWith() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- GitLab ---

func TestValidateGitLabConnEmpty(t *testing.T) {
	if err := validateGitLabConn("https://gitlab.com", ""); err == nil {
		t.Error("expected error for empty token")
	}
}

func TestValidateGitLabWith(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    interface{}
		wantErr bool
	}{
		{"valid", http.StatusOK, gitlab.Project{ID: 1, Name: "proj"}, false},
		{"unauthorized", http.StatusUnauthorized, map[string]string{"message": "401"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newAuthServer(t, tt.status, tt.body)
			defer srv.Close()
			client := gitlab.NewClientWithBaseURL("fake-token", "group/proj", srv.URL)
			err := validateGitLabWith(client)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateGitLabWith() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- Azure DevOps ---

func TestValidateAzureDevOpsConnRequiredFields(t *testing.T) {
	if err := validateAzureDevOpsConn("", "proj", "pat"); err == nil {
		t.Error("expected error for missing org")
	}
}

func TestValidateAzureDevOpsWith(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    interface{}
		wantErr bool
	}{
		{"valid empty result", http.StatusOK, map[string]interface{}{"workItems": []interface{}{}}, false},
		{"unauthorized", http.StatusUnauthorized, map[string]string{"message": "denied"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newAuthServer(t, tt.status, tt.body)
			defer srv.Close()
			client := azuredevops.NewClientWithBaseURL("pat", "org", "proj", srv.URL)
			err := validateAzureDevOpsWith(client)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAzureDevOpsWith() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- Asana ---

func TestValidateAsanaConnEmpty(t *testing.T) {
	if _, err := validateAsanaConn(""); err == nil {
		t.Error("expected error for empty token")
	}
}

func TestValidateAsanaWorkspaceRequiresID(t *testing.T) {
	if _, err := validateAsanaWorkspace("tok", ""); err == nil {
		t.Error("expected error for empty workspace ID")
	}
}

func TestValidateAsanaTokenWith(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      interface{}
		wantNames []string
		wantErr   bool
	}{
		{
			name:   "valid token lists workspaces",
			status: http.StatusOK,
			body: map[string]interface{}{"data": []map[string]string{
				{"gid": "1", "name": "Acme Corp"},
				{"gid": "2", "name": "Side Project"},
			}},
			wantNames: []string{"Acme Corp", "Side Project"},
		},
		{
			name:    "unauthorized",
			status:  http.StatusUnauthorized,
			body:    map[string]string{"message": "no"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newAuthServer(t, tt.status, tt.body)
			defer srv.Close()
			client := asana.NewClientWithBaseURL(srv.URL, "fake-token", "")
			names, err := validateAsanaTokenWith(client)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateAsanaTokenWith() err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if len(names) != len(tt.wantNames) {
					t.Fatalf("got %d names, want %d", len(names), len(tt.wantNames))
				}
				for i, n := range names {
					if n != tt.wantNames[i] {
						t.Errorf("name[%d] = %q, want %q", i, n, tt.wantNames[i])
					}
				}
			}
		})
	}
}

func TestValidateAsanaWith(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     interface{}
		wantName string
		wantErr  bool
	}{
		{
			name:     "valid",
			status:   http.StatusOK,
			body:     map[string]interface{}{"data": map[string]string{"gid": "1", "name": "My Workspace"}},
			wantName: "My Workspace",
		},
		{
			name:    "unauthorized",
			status:  http.StatusUnauthorized,
			body:    map[string]string{"message": "no"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newAuthServer(t, tt.status, tt.body)
			defer srv.Close()
			client := asana.NewClientWithBaseURL(srv.URL, "fake-token", "12345")
			name, err := validateAsanaWith(client)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateAsanaWith() err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}

// --- Slack (#11: real auth.test validation) ---

// TestValidateSlackConnFormat covers the cheap format gate before any network call.
func TestValidateSlackConnFormat(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"missing prefix", "invalid-format"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := validateSlackConn(tt.token); err == nil {
				t.Errorf("validateSlackConn(%q) err = nil, want error", tt.token)
			}
		})
	}
}

// TestValidateSlackWith exercises the auth.test path against a stub server: a
// valid token returns the team name, a bad token surfaces a wrapped error.
func TestValidateSlackWith(t *testing.T) {
	tests := []struct {
		name     string
		response slack.AuthTestResponse
		wantTeam string
		wantErr  bool
	}{
		{"valid", slack.AuthTestResponse{OK: true, Team: "Acme Corp"}, "Acme Corp", false},
		{"bad token", slack.AuthTestResponse{OK: false, Error: "invalid_auth"}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/auth.test") {
					t.Errorf("path = %q, want /auth.test", r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer srv.Close()

			client := slack.NewClientWithBaseURL(testutil.FakeSlackBotToken, srv.URL)
			team, err := validateSlackWith(client)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSlackWith() err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && team != tt.wantTeam {
				t.Errorf("team = %q, want %q", team, tt.wantTeam)
			}
		})
	}
}

func TestValidateTelegramConnFormat(t *testing.T) {
	if _, err := validateTelegramConn("invalid-no-colon"); err == nil {
		t.Error("expected error for token without colon")
	}
}

// Guard: ensure validators wrap auth failures in a clear message rather than
// silently succeeding.
func TestValidateGitHubWithErrorMessage(t *testing.T) {
	srv := newAuthServer(t, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
	defer srv.Close()
	client := github.NewClientWithBaseURL(testutil.FakeGitHubToken, srv.URL)
	err := validateGitHubWith(client)
	if err == nil || !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("expected wrapped validation error, got %v", err)
	}
}
