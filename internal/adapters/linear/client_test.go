package linear

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewClient(t *testing.T) {
	client := NewClient(testutil.FakeLinearAPIKey)
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.apiKey != testutil.FakeLinearAPIKey {
		t.Errorf("client.apiKey = %s, want %s", client.apiKey, testutil.FakeLinearAPIKey)
	}
	if client.httpClient == nil {
		t.Error("client.httpClient is nil")
	}
	if client.httpClient.Timeout != 30*time.Second {
		t.Errorf("client.httpClient.Timeout = %v, want 30s", client.httpClient.Timeout)
	}
	if client.doneStateCache == nil {
		t.Error("client.doneStateCache is nil")
	}
}

func TestExecute_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Verify headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %s, want application/json", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != testutil.FakeLinearAPIKey {
			t.Errorf("Authorization = %s, want "+testutil.FakeLinearAPIKey, r.Header.Get("Authorization"))
		}

		// Verify request body
		var reqBody GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if reqBody.Query != "query { viewer { id } }" {
			t.Errorf("query = %s, want query { viewer { id } }", reqBody.Query)
		}

		// Send response
		resp := GraphQLResponse{
			Data: json.RawMessage(`{"viewer": {"id": "user-123"}}`),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestableClient(server.URL, testutil.FakeLinearAPIKey)

	var result struct {
		Viewer struct {
			ID string `json:"id"`
		} `json:"viewer"`
	}

	err := client.execute(context.Background(), "query { viewer { id } }", nil, &result)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Viewer.ID != "user-123" {
		t.Errorf("result.Viewer.ID = %s, want user-123", result.Viewer.ID)
	}
}

func TestExecute_WithVariables(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		// Verify variables
		if reqBody.Variables["id"] != "issue-123" {
			t.Errorf("variables[id] = %v, want issue-123", reqBody.Variables["id"])
		}

		resp := GraphQLResponse{
			Data: json.RawMessage(`{"issue": {"id": "issue-123"}}`),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestableClient(server.URL, testutil.FakeLinearAPIKey)

	var result struct {
		Issue struct {
			ID string `json:"id"`
		} `json:"issue"`
	}

	variables := map[string]interface{}{"id": "issue-123"}
	err := client.execute(context.Background(), "query GetIssue($id: String!) { issue(id: $id) { id } }", variables, &result)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}

func TestExecute_GraphQLError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := GraphQLResponse{
			Errors: []GraphQLError{
				{Message: "Issue not found"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestableClient(server.URL, testutil.FakeLinearAPIKey)

	err := client.execute(context.Background(), "query { issue(id: \"invalid\") { id } }", nil, nil)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if err.Error() != "GraphQL error: Issue not found" {
		t.Errorf("error = %v, want 'GraphQL error: Issue not found'", err)
	}
}

func TestExecute_HTTPError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		wantErr    string
	}{
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			response:   `{"error": "Invalid API key"}`,
			wantErr:    "API error:",
		},
		{
			name:       "internal server error",
			statusCode: http.StatusInternalServerError,
			response:   `{"error": "Internal error"}`,
			wantErr:    "API error:",
		},
		{
			name:       "rate limited",
			statusCode: http.StatusTooManyRequests,
			response:   `{"error": "Rate limit exceeded"}`,
			wantErr:    "API error:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := newTestableClient(server.URL, testutil.FakeLinearAPIKey)

			err := client.execute(context.Background(), "query { viewer { id } }", nil, nil)
			if err == nil {
				t.Fatal("expected error but got nil")
			}
			if !contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestExecute_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{invalid json}`))
	}))
	defer server.Close()

	client := newTestableClient(server.URL, testutil.FakeLinearAPIKey)

	err := client.execute(context.Background(), "query { viewer { id } }", nil, nil)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !contains(err.Error(), "failed to parse response") {
		t.Errorf("error = %v, want to contain 'failed to parse response'", err)
	}
}

func TestExecute_NilResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := GraphQLResponse{
			Data: json.RawMessage(`{"success": true}`),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestableClient(server.URL, testutil.FakeLinearAPIKey)

	// Should not error when result is nil
	err := client.execute(context.Background(), "mutation { doSomething { success } }", nil, nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}

func TestExecute_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		resp := GraphQLResponse{
			Data: json.RawMessage(`{}`),
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestableClient(server.URL, testutil.FakeLinearAPIKey)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := client.execute(ctx, "query { viewer { id } }", nil, nil)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}
