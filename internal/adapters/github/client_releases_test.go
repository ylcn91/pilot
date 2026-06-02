package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestGetTagForSHA(t *testing.T) {
	tests := []struct {
		name       string
		sha        string
		tags       []*Tag
		wantTag    string
		statusCode int
		wantErr    bool
	}{
		{
			name: "found tag",
			sha:  "abc123",
			tags: []*Tag{
				{Name: "v1.0.0", Commit: struct {
					SHA string `json:"sha"`
				}{SHA: "def456"}},
				{Name: "v1.0.1", Commit: struct {
					SHA string `json:"sha"`
				}{SHA: "abc123"}},
			},
			wantTag:    "v1.0.1",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name: "no matching tag",
			sha:  "abc123",
			tags: []*Tag{
				{Name: "v1.0.0", Commit: struct {
					SHA string `json:"sha"`
				}{SHA: "def456"}},
			},
			wantTag:    "",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "empty tags",
			sha:        "abc123",
			tags:       []*Tag{},
			wantTag:    "",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "api error",
			sha:        "abc123",
			tags:       nil,
			wantTag:    "",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				if !strings.HasPrefix(r.URL.Path, "/repos/owner/repo/tags") {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				w.WriteHeader(tt.statusCode)
				if tt.statusCode == http.StatusOK {
					_ = json.NewEncoder(w).Encode(tt.tags)
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			tagName, err := client.GetTagForSHA(context.Background(), "owner", "repo", tt.sha)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetTagForSHA() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tagName != tt.wantTag {
				t.Errorf("GetTagForSHA() = %q, want %q", tagName, tt.wantTag)
			}
		})
	}
}

func TestGetReleaseByTag(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   interface{}
		wantNil    bool
		wantErr    bool
	}{
		{
			name:       "found",
			statusCode: http.StatusOK,
			response: Release{
				ID:      123,
				TagName: "v1.0.0",
				Body:    "changelog here",
			},
			wantNil: false,
			wantErr: false,
		},
		{
			name:       "not found returns nil",
			statusCode: http.StatusNotFound,
			response:   map[string]string{"message": "Not Found"},
			wantNil:    true,
			wantErr:    false,
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			response:   map[string]string{"message": "Internal Server Error"},
			wantNil:    true,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/owner/repo/releases/tags/v1.0.0" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method != http.MethodGet {
					t.Errorf("unexpected method: %s", r.Method)
				}
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			release, err := client.GetReleaseByTag(context.Background(), "owner", "repo", "v1.0.0")

			if (err != nil) != tt.wantErr {
				t.Errorf("GetReleaseByTag() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if (release == nil) != tt.wantNil {
				t.Errorf("GetReleaseByTag() release nil = %v, wantNil %v", release == nil, tt.wantNil)
			}
			if release != nil && release.ID != 123 {
				t.Errorf("GetReleaseByTag() release.ID = %d, want 123", release.ID)
			}
		})
	}
}

func TestUpdateRelease(t *testing.T) {
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
			var receivedBody map[string]interface{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/owner/repo/releases/123" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method != http.MethodPatch {
					t.Errorf("unexpected method: %s, want PATCH", r.Method)
				}
				_ = json.NewDecoder(r.Body).Decode(&receivedBody)
				w.WriteHeader(tt.statusCode)
				_, _ = fmt.Fprintf(w, `{"id":123,"tag_name":"v1.0.0","body":"updated body"}`)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			release, err := client.UpdateRelease(context.Background(), "owner", "repo", 123, &ReleaseInput{
				Body: "enriched changelog",
			})

			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateRelease() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if release == nil {
					t.Fatal("UpdateRelease() returned nil release")
				}
				if receivedBody["body"] != "enriched changelog" {
					t.Errorf("UpdateRelease() sent body = %v, want 'enriched changelog'", receivedBody["body"])
				}
			}
		})
	}
}
