package asana

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestListWorkspaces(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       interface{}
		wantErr    bool
		wantNames  []string
	}{
		{
			name:       "valid token returns workspaces",
			statusCode: http.StatusOK,
			body: PagedResponse[Workspace]{
				Data: []Workspace{
					{GID: "1", Name: "Acme Corp"},
					{GID: "2", Name: "Side Project"},
				},
			},
			wantErr:   false,
			wantNames: []string{"Acme Corp", "Side Project"},
		},
		{
			name:       "unauthorized token fails",
			statusCode: http.StatusUnauthorized,
			body: APIResponse[interface{}]{
				Errors: []APIError{{Message: "Not Authorized"}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/workspaces" {
					t.Errorf("path = %q, want /workspaces", r.URL.Path)
				}
				if r.Method != http.MethodGet {
					t.Errorf("method = %q, want GET", r.Method)
				}
				if r.Header.Get("Authorization") != "Bearer "+testutil.FakeAsanaAccessToken {
					t.Error("missing or incorrect Authorization header")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.body)
			}))
			defer server.Close()

			// Empty workspace ID: ListWorkspaces must not need one.
			client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, "")
			got, err := client.ListWorkspaces(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ListWorkspaces() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ListWorkspaces() unexpected error = %v", err)
			}
			if len(got) != len(tt.wantNames) {
				t.Fatalf("got %d workspaces, want %d", len(got), len(tt.wantNames))
			}
			for i, ws := range got {
				if ws.Name != tt.wantNames[i] {
					t.Errorf("workspace[%d].Name = %q, want %q", i, ws.Name, tt.wantNames[i])
				}
			}
		})
	}
}
