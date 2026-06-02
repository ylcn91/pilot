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

func TestGetOpenSubIssueCount(t *testing.T) {
	tests := []struct {
		name            string
		restResponse    string
		restStatus      int
		graphqlResponse string
		graphqlStatus   int
		wantCount       int
		wantNativeLinks bool
		wantErr         bool
		errContains     string
	}{
		{
			name:            "some open sub-issues",
			restResponse:    `{"node_id":"I_parent123","number":50}`,
			restStatus:      http.StatusOK,
			graphqlResponse: `{"data":{"node":{"subIssues":{"totalCount":3,"nodes":[{"state":"OPEN"},{"state":"CLOSED"},{"state":"OPEN"}]}}}}`,
			graphqlStatus:   http.StatusOK,
			wantCount:       2,
			wantNativeLinks: true,
		},
		{
			name:            "all closed",
			restResponse:    `{"node_id":"I_parent123","number":50}`,
			restStatus:      http.StatusOK,
			graphqlResponse: `{"data":{"node":{"subIssues":{"totalCount":2,"nodes":[{"state":"CLOSED"},{"state":"CLOSED"}]}}}}`,
			graphqlStatus:   http.StatusOK,
			wantCount:       0,
			wantNativeLinks: true,
		},
		{
			name:            "no native links",
			restResponse:    `{"node_id":"I_parent123","number":50}`,
			restStatus:      http.StatusOK,
			graphqlResponse: `{"data":{"node":{"subIssues":{"totalCount":0,"nodes":[]}}}}`,
			graphqlStatus:   http.StatusOK,
			wantCount:       0,
			wantNativeLinks: false,
		},
		{
			name:         "parent REST error",
			restResponse: `{"message":"Not Found"}`,
			restStatus:   http.StatusNotFound,
			wantErr:      true,
			errContains:  "resolve parent node ID",
		},
		{
			name:            "graphql error",
			restResponse:    `{"node_id":"I_parent123","number":50}`,
			restStatus:      http.StatusOK,
			graphqlResponse: `{"data":null,"errors":[{"message":"something broke"}]}`,
			graphqlStatus:   http.StatusOK,
			wantErr:         true,
			errContains:     "query sub-issues",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/repos/owner/repo/issues/50" && r.Method == http.MethodGet:
					w.WriteHeader(tt.restStatus)
					_, _ = w.Write([]byte(tt.restResponse))
				case r.URL.Path == "/graphql" && r.Method == http.MethodPost:
					w.WriteHeader(tt.graphqlStatus)
					_, _ = w.Write([]byte(tt.graphqlResponse))
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			count, hasNative, err := client.GetOpenSubIssueCount(context.Background(), "owner", "repo", 50)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetOpenSubIssueCount() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.errContains)
				}
				return
			}
			if count != tt.wantCount {
				t.Errorf("GetOpenSubIssueCount() count = %d, want %d", count, tt.wantCount)
			}
			if hasNative != tt.wantNativeLinks {
				t.Errorf("GetOpenSubIssueCount() hasNativeLinks = %v, want %v", hasNative, tt.wantNativeLinks)
			}
		})
	}
}

func TestSearchOpenPilotIssuesWithSubIssues(t *testing.T) {
	tests := []struct {
		name            string
		graphqlResponse string
		graphqlStatus   int
		limit           int
		wantNumbers     []int
		wantErr         bool
		errContains     string
	}{
		{
			name:  "returns issues with sub-issues",
			limit: 10,
			graphqlResponse: `{"data":{"repository":{"issues":{"nodes":[
				{"number":1,"subIssuesSummary":{"total":3,"completed":1}},
				{"number":2,"subIssuesSummary":{"total":0,"completed":0}},
				{"number":3,"subIssuesSummary":{"total":2,"completed":2}}
			]}}}}`,
			graphqlStatus: http.StatusOK,
			wantNumbers:   []int{1, 3},
		},
		{
			name:  "empty results when no sub-issues",
			limit: 10,
			graphqlResponse: `{"data":{"repository":{"issues":{"nodes":[
				{"number":5,"subIssuesSummary":{"total":0,"completed":0}},
				{"number":6,"subIssuesSummary":{"total":0,"completed":0}}
			]}}}}`,
			graphqlStatus: http.StatusOK,
			wantNumbers:   nil,
		},
		{
			name:            "empty results when no issues",
			limit:           10,
			graphqlResponse: `{"data":{"repository":{"issues":{"nodes":[]}}}}`,
			graphqlStatus:   http.StatusOK,
			wantNumbers:     nil,
		},
		{
			name:  "limit truncation applied to API request",
			limit: 2,
			graphqlResponse: `{"data":{"repository":{"issues":{"nodes":[
				{"number":10,"subIssuesSummary":{"total":1,"completed":0}},
				{"number":11,"subIssuesSummary":{"total":4,"completed":2}}
			]}}}}`,
			graphqlStatus: http.StatusOK,
			wantNumbers:   []int{10, 11},
		},
		{
			name:            "API error propagation",
			limit:           10,
			graphqlResponse: `{"data":null,"errors":[{"message":"could not resolve repository"}]}`,
			graphqlStatus:   http.StatusOK,
			wantErr:         true,
			errContains:     "search pilot issues with sub-issues",
		},
		{
			name:            "HTTP error propagation",
			limit:           10,
			graphqlStatus:   http.StatusInternalServerError,
			graphqlResponse: `internal error`,
			wantErr:         true,
			errContains:     "search pilot issues with sub-issues",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedFirst int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/graphql" && r.Method == http.MethodPost {
					var req GraphQLRequest
					if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
						if v, ok := req.Variables["first"]; ok {
							switch n := v.(type) {
							case float64:
								capturedFirst = int(n)
							}
						}
					}
					w.WriteHeader(tt.graphqlStatus)
					_, _ = w.Write([]byte(tt.graphqlResponse))
					return
				}
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			numbers, err := client.SearchOpenPilotIssuesWithSubIssues(context.Background(), "owner", "repo", tt.limit)

			if (err != nil) != tt.wantErr {
				t.Errorf("SearchOpenPilotIssuesWithSubIssues() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.errContains)
				}
				return
			}
			if !tt.wantErr && capturedFirst != tt.limit {
				t.Errorf("GraphQL first = %d, want %d (limit not forwarded)", capturedFirst, tt.limit)
			}
			if len(numbers) != len(tt.wantNumbers) {
				t.Errorf("SearchOpenPilotIssuesWithSubIssues() = %v, want %v", numbers, tt.wantNumbers)
				return
			}
			for i, n := range numbers {
				if n != tt.wantNumbers[i] {
					t.Errorf("numbers[%d] = %d, want %d", i, n, tt.wantNumbers[i])
				}
			}
		})
	}
}
