package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestCreateCommitStatus(t *testing.T) {
	tests := []struct {
		name       string
		status     *CommitStatus
		statusCode int
		wantErr    bool
	}{
		{
			name: "success - pending",
			status: &CommitStatus{
				State:       StatusPending,
				Context:     "pilot/execution",
				Description: "Running...",
			},
			statusCode: http.StatusCreated,
			wantErr:    false,
		},
		{
			name: "success - success with URL",
			status: &CommitStatus{
				State:       StatusSuccess,
				Context:     "pilot/execution",
				Description: "Completed",
				TargetURL:   "https://example.com/logs/123",
			},
			statusCode: http.StatusCreated,
			wantErr:    false,
		},
		{
			name: "not found",
			status: &CommitStatus{
				State:   StatusPending,
				Context: "pilot/execution",
			},
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if r.URL.Path != "/repos/owner/repo/statuses/abc123def" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				var body CommitStatus
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}

				if body.State != tt.status.State {
					t.Errorf("unexpected state: %s", body.State)
				}
				if body.Context != tt.status.Context {
					t.Errorf("unexpected context: %s", body.Context)
				}

				w.WriteHeader(tt.statusCode)
				if tt.statusCode < 300 {
					result := CommitStatus{
						ID:          12345,
						State:       body.State,
						Context:     body.Context,
						Description: body.Description,
						TargetURL:   body.TargetURL,
					}
					_ = json.NewEncoder(w).Encode(result)
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			result, err := client.CreateCommitStatus(context.Background(), "owner", "repo", "abc123def", tt.status)

			if (err != nil) != tt.wantErr {
				t.Errorf("CreateCommitStatus() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result.ID != 12345 {
				t.Errorf("result.ID = %d, want 12345", result.ID)
			}
		})
	}
}

func TestCreateCheckRun(t *testing.T) {
	tests := []struct {
		name       string
		checkRun   *CheckRun
		statusCode int
		wantErr    bool
	}{
		{
			name: "success - queued",
			checkRun: &CheckRun{
				HeadSHA: "abc123def456",
				Name:    "Pilot Execution",
				Status:  CheckRunQueued,
			},
			statusCode: http.StatusCreated,
			wantErr:    false,
		},
		{
			name: "success - in progress with output",
			checkRun: &CheckRun{
				HeadSHA: "abc123def456",
				Name:    "Pilot Execution",
				Status:  CheckRunInProgress,
				Output: &CheckOutput{
					Title:   "Running tests",
					Summary: "Currently executing test suite",
				},
			},
			statusCode: http.StatusCreated,
			wantErr:    false,
		},
		{
			name: "error - bad request",
			checkRun: &CheckRun{
				HeadSHA: "",
				Name:    "Pilot Execution",
			},
			statusCode: http.StatusUnprocessableEntity,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if r.URL.Path != "/repos/owner/repo/check-runs" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				w.WriteHeader(tt.statusCode)
				if tt.statusCode < 300 {
					result := CheckRun{
						ID:      67890,
						HeadSHA: tt.checkRun.HeadSHA,
						Name:    tt.checkRun.Name,
						Status:  tt.checkRun.Status,
					}
					_ = json.NewEncoder(w).Encode(result)
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			result, err := client.CreateCheckRun(context.Background(), "owner", "repo", tt.checkRun)

			if (err != nil) != tt.wantErr {
				t.Errorf("CreateCheckRun() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result.ID != 67890 {
				t.Errorf("result.ID = %d, want 67890", result.ID)
			}
		})
	}
}

func TestUpdateCheckRun(t *testing.T) {
	tests := []struct {
		name       string
		checkRunID int64
		checkRun   *CheckRun
		statusCode int
		wantErr    bool
	}{
		{
			name:       "success - complete with success",
			checkRunID: 67890,
			checkRun: &CheckRun{
				Status:     CheckRunCompleted,
				Conclusion: ConclusionSuccess,
			},
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "success - complete with failure",
			checkRunID: 67890,
			checkRun: &CheckRun{
				Status:     CheckRunCompleted,
				Conclusion: ConclusionFailure,
				Output: &CheckOutput{
					Title:   "Tests failed",
					Summary: "3 tests failed",
				},
			},
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "not found",
			checkRunID: 99999,
			checkRun: &CheckRun{
				Status:     CheckRunCompleted,
				Conclusion: ConclusionSuccess,
			},
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch {
					t.Errorf("expected PATCH, got %s", r.Method)
				}

				w.WriteHeader(tt.statusCode)
				if tt.statusCode < 300 {
					result := CheckRun{
						ID:         tt.checkRunID,
						Status:     tt.checkRun.Status,
						Conclusion: tt.checkRun.Conclusion,
					}
					_ = json.NewEncoder(w).Encode(result)
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			result, err := client.UpdateCheckRun(context.Background(), "owner", "repo", tt.checkRunID, tt.checkRun)

			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateCheckRun() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result.Status != tt.checkRun.Status {
				t.Errorf("result.Status = %s, want %s", result.Status, tt.checkRun.Status)
			}
		})
	}
}

func TestGetJobLogs(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
		wantLogs   string
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			body:       "2024-01-01T00:00:00Z Error: lint failed\nSA5011: possible nil pointer",
			wantErr:    false,
			wantLogs:   "2024-01-01T00:00:00Z Error: lint failed\nSA5011: possible nil pointer",
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			body:       "Not Found",
			wantErr:    true,
		},
		{
			name:       "empty logs",
			statusCode: http.StatusOK,
			body:       "",
			wantErr:    false,
			wantLogs:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/owner/repo/actions/jobs/123/logs" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method != http.MethodGet {
					t.Errorf("unexpected method: %s", r.Method)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			logs, err := client.GetJobLogs(context.Background(), "owner", "repo", 123)

			if tt.wantErr {
				if err == nil {
					t.Error("GetJobLogs() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("GetJobLogs() unexpected error: %v", err)
			}
			if logs != tt.wantLogs {
				t.Errorf("GetJobLogs() = %q, want %q", logs, tt.wantLogs)
			}
		})
	}
}
