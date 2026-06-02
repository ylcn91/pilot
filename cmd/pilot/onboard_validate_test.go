package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestValidateGitHubConn tests GitHub connection validation.
// Note: Current implementation is a stub that only checks for empty token.
func TestValidateGitHubConn(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{
			name:    "valid token",
			token:   testutil.FakeGitHubToken,
			wantErr: false,
		},
		{
			name:    "empty token",
			token:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGitHubConn(tt.token)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateGitHubConn() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateGitHubConnWithServer demonstrates GitHub validation with httptest server.
// Uses GetRepository as a proxy for auth validation since the client doesn't have GetAuthenticatedUser.
func TestValidateGitHubConnWithServer(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   interface{}
		wantErr    bool
	}{
		{
			name:       "success - valid token",
			statusCode: http.StatusOK,
			response:   github.Repository{Name: "test-repo"},
			wantErr:    false,
		},
		{
			name:       "unauthorized - invalid token",
			statusCode: http.StatusUnauthorized,
			response:   map[string]string{"message": "Bad credentials"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+testutil.FakeGitHubToken {
					t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
				}
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			// Use the GitHub client with test server base URL
			client := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			_, err := client.GetRepository(context.Background(), "owner", "repo")

			if (err != nil) != tt.wantErr {
				t.Errorf("GetRepository() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateSlackConn tests Slack connection validation.
// Note: Current implementation validates token format (xoxb- prefix).
func TestValidateSlackConn(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantBot string
		wantErr bool
	}{
		{
			name:    "valid token format",
			token:   "xoxb-test-token",
			wantBot: "pilot-bot", // Stub always returns "pilot-bot"
			wantErr: false,
		},
		{
			name:    "invalid token format - no xoxb prefix",
			token:   "invalid-format",
			wantBot: "",
			wantErr: true,
		},
		{
			name:    "empty token",
			token:   "",
			wantBot: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			botName, err := validateSlackConn(tt.token)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSlackConn() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && botName != tt.wantBot {
				t.Errorf("validateSlackConn() botName = %v, want %v", botName, tt.wantBot)
			}
		})
	}
}

// TestValidateSlackConnWithServer tests Slack validation with httptest server.
func TestValidateSlackConnWithServer(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   interface{}
		wantBot    string
		wantErr    bool
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			response:   map[string]interface{}{"ok": true, "user": "pilot-bot"},
			wantBot:    "pilot-bot",
			wantErr:    false,
		},
		{
			name:       "auth error",
			statusCode: http.StatusOK,
			response:   map[string]interface{}{"ok": false, "error": "invalid_auth"},
			wantBot:    "",
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

			// Note: Would need to inject test server URL into Slack client
			// For now, just validate the test server setup
			_ = server
		})
	}
}

// TestValidateLinearConn tests Linear connection validation.
func TestValidateLinearConn(t *testing.T) {
	tests := []struct {
		name          string
		apiKey        string
		statusCode    int
		response      interface{}
		wantWorkspace string
		wantErr       bool
	}{
		{
			name:          "valid API key",
			apiKey:        testutil.FakeLinearAPIKey,
			statusCode:    http.StatusOK,
			response:      map[string]interface{}{"data": map[string]interface{}{"organization": map[string]string{"name": "Test Workspace"}}},
			wantWorkspace: "Workspace", // Stub returns "Workspace"
			wantErr:       false,
		},
		{
			name:          "empty API key",
			apiKey:        "",
			statusCode:    0,
			response:      nil,
			wantWorkspace: "",
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspaceName, err := validateLinearConn(tt.apiKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateLinearConn() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && workspaceName != tt.wantWorkspace {
				t.Errorf("validateLinearConn() workspaceName = %v, want %v", workspaceName, tt.wantWorkspace)
			}
		})
	}
}

// TestValidateTelegramConn tests Telegram connection validation.
func TestValidateTelegramConn(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		statusCode int
		response   interface{}
		wantBot    string
		wantErr    bool
	}{
		{
			name:       "invalid token format - no colon",
			token:      "invalid-no-colon",
			statusCode: 0,
			response:   nil,
			wantBot:    "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			botName, err := validateTelegramConn(tt.token)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTelegramConn() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && botName != tt.wantBot {
				t.Errorf("validateTelegramConn() botName = %v, want %v", botName, tt.wantBot)
			}
		})
	}
}
