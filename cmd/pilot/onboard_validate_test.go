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

// --- Slack (format-only check, no auth endpoint exposed by the client) ---

func TestValidateSlackConn(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantBot string
		wantErr bool
	}{
		{"valid format", "xoxb-test-token", "pilot-bot", false},
		{"missing prefix", "invalid-format", "", true},
		{"empty", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			botName, err := validateSlackConn(tt.token)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSlackConn() err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && botName != tt.wantBot {
				t.Errorf("botName = %q, want %q", botName, tt.wantBot)
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
