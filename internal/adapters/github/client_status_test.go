package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestGetCombinedStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   interface{}
		wantErr    bool
		wantState  string
	}{
		{
			name:       "success - all passing",
			statusCode: http.StatusOK,
			response: CombinedStatus{
				State:      StatusSuccess,
				SHA:        "abc123def456",
				TotalCount: 2,
				Statuses: []CommitStatus{
					{Context: "ci/build", State: StatusSuccess},
					{Context: "ci/test", State: StatusSuccess},
				},
			},
			wantErr:   false,
			wantState: StatusSuccess,
		},
		{
			name:       "success - pending",
			statusCode: http.StatusOK,
			response: CombinedStatus{
				State:      StatusPending,
				SHA:        "abc123def456",
				TotalCount: 1,
				Statuses: []CommitStatus{
					{Context: "ci/build", State: StatusPending},
				},
			},
			wantErr:   false,
			wantState: StatusPending,
		},
		{
			name:       "success - failure",
			statusCode: http.StatusOK,
			response: CombinedStatus{
				State:      StatusFailure,
				SHA:        "abc123def456",
				TotalCount: 2,
				Statuses: []CommitStatus{
					{Context: "ci/build", State: StatusSuccess},
					{Context: "ci/test", State: StatusFailure},
				},
			},
			wantErr:   false,
			wantState: StatusFailure,
		},
		{
			name:       "success - no statuses",
			statusCode: http.StatusOK,
			response: CombinedStatus{
				State:      StatusPending,
				SHA:        "abc123def456",
				TotalCount: 0,
				Statuses:   []CommitStatus{},
			},
			wantErr:   false,
			wantState: StatusPending,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			response:   map[string]string{"message": "Not Found"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				if r.URL.Path != "/repos/owner/repo/commits/abc123def456/status" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			status, err := client.GetCombinedStatus(context.Background(), "owner", "repo", "abc123def456")

			if (err != nil) != tt.wantErr {
				t.Errorf("GetCombinedStatus() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && status.State != tt.wantState {
				t.Errorf("status.State = %s, want %s", status.State, tt.wantState)
			}
		})
	}
}

func TestListCheckRuns(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   interface{}
		wantErr    bool
		wantCount  int
	}{
		{
			name:       "success - multiple check runs",
			statusCode: http.StatusOK,
			response: CheckRunsResponse{
				TotalCount: 3,
				CheckRuns: []CheckRun{
					{ID: 1, Name: "build", Status: CheckRunCompleted, Conclusion: ConclusionSuccess},
					{ID: 2, Name: "test", Status: CheckRunCompleted, Conclusion: ConclusionSuccess},
					{ID: 3, Name: "lint", Status: CheckRunCompleted, Conclusion: ConclusionSuccess},
				},
			},
			wantErr:   false,
			wantCount: 3,
		},
		{
			name:       "success - in progress",
			statusCode: http.StatusOK,
			response: CheckRunsResponse{
				TotalCount: 2,
				CheckRuns: []CheckRun{
					{ID: 1, Name: "build", Status: CheckRunCompleted, Conclusion: ConclusionSuccess},
					{ID: 2, Name: "test", Status: CheckRunInProgress},
				},
			},
			wantErr:   false,
			wantCount: 2,
		},
		{
			name:       "success - no check runs",
			statusCode: http.StatusOK,
			response: CheckRunsResponse{
				TotalCount: 0,
				CheckRuns:  []CheckRun{},
			},
			wantErr:   false,
			wantCount: 0,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			response:   map[string]string{"message": "Not Found"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				if r.URL.Path != "/repos/owner/repo/commits/abc123def456/check-runs" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Header.Get("Accept") != "application/vnd.github+json" {
					t.Errorf("unexpected Accept header: %s", r.Header.Get("Accept"))
				}

				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			result, err := client.ListCheckRuns(context.Background(), "owner", "repo", "abc123def456")

			if (err != nil) != tt.wantErr {
				t.Errorf("ListCheckRuns() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result.TotalCount != tt.wantCount {
				t.Errorf("result.TotalCount = %d, want %d", result.TotalCount, tt.wantCount)
			}
		})
	}
}
