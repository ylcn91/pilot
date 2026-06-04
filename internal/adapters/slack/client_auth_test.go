package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestClientAuthTest(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   AuthTestResponse
		wantErr    bool
		wantTeam   string
	}{
		{
			name:       "valid token",
			statusCode: http.StatusOK,
			response:   AuthTestResponse{OK: true, Team: "Acme Corp", User: "pilot-bot"},
			wantErr:    false,
			wantTeam:   "Acme Corp",
		},
		{
			name:       "invalid auth",
			statusCode: http.StatusOK,
			response:   AuthTestResponse{OK: false, Error: "invalid_auth"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %q, want POST", r.Method)
				}
				if !strings.HasSuffix(r.URL.Path, "/auth.test") {
					t.Errorf("path = %q, want to end with /auth.test", r.URL.Path)
				}
				if auth := r.Header.Get("Authorization"); auth != "Bearer "+testutil.FakeSlackBotToken {
					t.Errorf("Authorization = %q, want bearer fake token", auth)
				}
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeSlackBotToken, server.URL)
			got, err := client.AuthTest(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("AuthTest() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("AuthTest() unexpected error = %v", err)
			}
			if got.Team != tt.wantTeam {
				t.Errorf("Team = %q, want %q", got.Team, tt.wantTeam)
			}
		})
	}
}
