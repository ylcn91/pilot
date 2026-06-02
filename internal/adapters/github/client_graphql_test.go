package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestExecuteGraphQL(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		variables   map[string]interface{}
		statusCode  int
		response    string
		wantErr     bool
		errContains string
	}{
		{
			name:       "success with result",
			query:      `query { viewer { login } }`,
			statusCode: http.StatusOK,
			response:   `{"data":{"viewer":{"login":"octocat"}}}`,
		},
		{
			name:       "success with variables",
			query:      `mutation($id: ID!) { addItem(id: $id) { id } }`,
			variables:  map[string]interface{}{"id": "abc123"},
			statusCode: http.StatusOK,
			response:   `{"data":{"addItem":{"id":"abc123"}}}`,
		},
		{
			name:        "graphql error in response",
			query:       `query { bad }`,
			statusCode:  http.StatusOK,
			response:    `{"data":null,"errors":[{"message":"Field 'bad' not found"}]}`,
			wantErr:     true,
			errContains: "graphql error: Field 'bad' not found",
		},
		{
			name:        "http error",
			query:       `query { viewer { login } }`,
			statusCode:  http.StatusUnauthorized,
			response:    `{"message":"Bad credentials"}`,
			wantErr:     true,
			errContains: "graphql API error (status 401)",
		},
		{
			name:       "nil result ignores data",
			query:      `mutation { doSomething }`,
			statusCode: http.StatusOK,
			response:   `{"data":{"doSomething":true}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if r.URL.Path != "/graphql" {
					t.Errorf("unexpected path: %s, want /graphql", r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer "+testutil.FakeGitHubToken {
					t.Errorf("unexpected Authorization header: %s", r.Header.Get("Authorization"))
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("unexpected Content-Type: %s", r.Header.Get("Content-Type"))
				}

				var reqBody GraphQLRequest
				if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
					t.Fatalf("failed to decode request body: %v", err)
				}
				if reqBody.Query != tt.query {
					t.Errorf("query = %q, want %q", reqBody.Query, tt.query)
				}

				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

			if tt.name == "nil result ignores data" {
				err := client.ExecuteGraphQL(context.Background(), tt.query, tt.variables, nil)
				if (err != nil) != tt.wantErr {
					t.Errorf("ExecuteGraphQL() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			var result map[string]interface{}
			err := client.ExecuteGraphQL(context.Background(), tt.query, tt.variables, &result)

			if (err != nil) != tt.wantErr {
				t.Errorf("ExecuteGraphQL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.errContains)
				}
			}
			if !tt.wantErr && result == nil {
				t.Error("expected non-nil result")
			}
		})
	}
}

func TestGetIssueNodeID(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		response    string
		wantNodeID  string
		wantErr     bool
		errContains string
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			response:   `{"node_id":"I_kwDOTest123","number":42,"title":"Test"}`,
			wantNodeID: "I_kwDOTest123",
		},
		{
			name:        "empty node_id",
			statusCode:  http.StatusOK,
			response:    `{"node_id":"","number":42}`,
			wantErr:     true,
			errContains: "empty node_id",
		},
		{
			name:        "not found",
			statusCode:  http.StatusNotFound,
			response:    `{"message":"Not Found"}`,
			wantErr:     true,
			errContains: "get issue node ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/owner/repo/issues/42" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			nodeID, err := client.GetIssueNodeID(context.Background(), "owner", "repo", 42)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetIssueNodeID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.errContains)
				}
				return
			}
			if nodeID != tt.wantNodeID {
				t.Errorf("GetIssueNodeID() = %q, want %q", nodeID, tt.wantNodeID)
			}
		})
	}
}

func TestLinkSubIssue(t *testing.T) {
	tests := []struct {
		name            string
		parentResponse  string
		childResponse   string
		graphqlResponse string
		parentStatus    int
		childStatus     int
		graphqlStatus   int
		wantErr         bool
		errContains     string
	}{
		{
			name:            "success",
			parentResponse:  `{"node_id":"I_parent123","number":10}`,
			childResponse:   `{"node_id":"I_child456","number":20}`,
			graphqlResponse: `{"data":{"addSubIssue":{"issue":{"id":"I_parent123"},"subIssue":{"id":"I_child456"}}}}`,
			parentStatus:    http.StatusOK,
			childStatus:     http.StatusOK,
			graphqlStatus:   http.StatusOK,
		},
		{
			name:           "parent not found",
			parentStatus:   http.StatusNotFound,
			parentResponse: `{"message":"Not Found"}`,
			wantErr:        true,
			errContains:    "resolve parent node ID",
		},
		{
			name:           "child not found",
			parentResponse: `{"node_id":"I_parent123","number":10}`,
			parentStatus:   http.StatusOK,
			childStatus:    http.StatusNotFound,
			childResponse:  `{"message":"Not Found"}`,
			wantErr:        true,
			errContains:    "resolve child node ID",
		},
		{
			name:            "graphql mutation error",
			parentResponse:  `{"node_id":"I_parent123","number":10}`,
			childResponse:   `{"node_id":"I_child456","number":20}`,
			graphqlResponse: `{"data":null,"errors":[{"message":"addSubIssue not available"}]}`,
			parentStatus:    http.StatusOK,
			childStatus:     http.StatusOK,
			graphqlStatus:   http.StatusOK,
			wantErr:         true,
			errContains:     "graphql error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/repos/owner/repo/issues/10" && r.Method == http.MethodGet:
					w.WriteHeader(tt.parentStatus)
					_, _ = w.Write([]byte(tt.parentResponse))
				case r.URL.Path == "/repos/owner/repo/issues/20" && r.Method == http.MethodGet:
					w.WriteHeader(tt.childStatus)
					_, _ = w.Write([]byte(tt.childResponse))
				case r.URL.Path == "/graphql" && r.Method == http.MethodPost:
					var reqBody GraphQLRequest
					_ = json.NewDecoder(r.Body).Decode(&reqBody)
					if reqBody.Variables["parentID"] != "I_parent123" {
						t.Errorf("parentID = %v, want I_parent123", reqBody.Variables["parentID"])
					}
					if reqBody.Variables["childID"] != "I_child456" {
						t.Errorf("childID = %v, want I_child456", reqBody.Variables["childID"])
					}
					w.WriteHeader(tt.graphqlStatus)
					_, _ = w.Write([]byte(tt.graphqlResponse))
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			err := client.LinkSubIssue(context.Background(), "owner", "repo", 10, 20)

			if (err != nil) != tt.wantErr {
				t.Errorf("LinkSubIssue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.errContains)
				}
			}
		})
	}
}
