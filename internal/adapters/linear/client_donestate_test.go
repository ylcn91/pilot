package linear

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestGetTeamDoneStateID(t *testing.T) {
	tests := []struct {
		name        string
		teamKey     string
		response    GraphQLResponse
		wantID      string
		wantErr     bool
		errContains string
	}{
		{
			name:    "returns completed state ID",
			teamKey: "ENG",
			response: GraphQLResponse{
				Data: json.RawMessage(`{
					"workflowStates": {
						"nodes": [
							{"id": "state-done-123", "name": "Done", "type": "completed"}
						]
					}
				}`),
			},
			wantID:  "state-done-123",
			wantErr: false,
		},
		{
			name:    "returns error when no completed state found",
			teamKey: "EMPTY",
			response: GraphQLResponse{
				Data: json.RawMessage(`{
					"workflowStates": {
						"nodes": []
					}
				}`),
			},
			wantID:      "",
			wantErr:     true,
			errContains: "no completed state found",
		},
		{
			name:    "returns GraphQL error",
			teamKey: "BAD",
			response: GraphQLResponse{
				Errors: []GraphQLError{
					{Message: "Team not found"},
				},
			},
			wantID:      "",
			wantErr:     true,
			errContains: "Team not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := newTestableClient(server.URL, testutil.FakeLinearAPIKey)

			gotID, err := client.getTeamDoneStateID(context.Background(), tt.teamKey)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want to contain %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotID != tt.wantID {
				t.Errorf("GetTeamDoneStateID() = %s, want %s", gotID, tt.wantID)
			}
		})
	}
}

func TestGetTeamDoneStateID_Cache(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := GraphQLResponse{
			Data: json.RawMessage(`{
				"workflowStates": {
					"nodes": [
						{"id": "state-done-456", "name": "Done", "type": "completed"}
					]
				}
			}`),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestableClient(server.URL, testutil.FakeLinearAPIKey)

	// First call should hit the server
	id1, err := client.getTeamDoneStateID(context.Background(), "TEAM")
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if id1 != "state-done-456" {
		t.Errorf("first call returned %s, want state-done-456", id1)
	}
	if callCount != 1 {
		t.Errorf("expected 1 HTTP call after first request, got %d", callCount)
	}

	// Second call should hit the cache, not the server
	id2, err := client.getTeamDoneStateID(context.Background(), "TEAM")
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if id2 != "state-done-456" {
		t.Errorf("second call returned %s, want state-done-456", id2)
	}
	if callCount != 1 {
		t.Errorf("expected still 1 HTTP call after second request (cache hit), got %d", callCount)
	}

	// Third call with different team should hit the server
	_, err = client.getTeamDoneStateID(context.Background(), "OTHER")
	if err != nil {
		t.Fatalf("third call failed: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls after third request (different team), got %d", callCount)
	}
}
