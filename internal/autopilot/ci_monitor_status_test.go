package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

func TestCIMonitor_GetFailedChecks(t *testing.T) {
	tests := []struct {
		name       string
		checkRuns  []github.CheckRun
		wantFailed []string
		wantErr    bool
	}{
		{
			name: "multiple failures",
			checkRuns: []github.CheckRun{
				{Name: "build", Conclusion: github.ConclusionFailure},
				{Name: "test", Conclusion: github.ConclusionSuccess},
				{Name: "lint", Conclusion: github.ConclusionFailure},
			},
			wantFailed: []string{"build", "lint"},
			wantErr:    false,
		},
		{
			name: "no failures",
			checkRuns: []github.CheckRun{
				{Name: "build", Conclusion: github.ConclusionSuccess},
				{Name: "test", Conclusion: github.ConclusionSuccess},
			},
			wantFailed: nil,
			wantErr:    false,
		},
		{
			name: "all failures",
			checkRuns: []github.CheckRun{
				{Name: "build", Conclusion: github.ConclusionFailure},
				{Name: "test", Conclusion: github.ConclusionFailure},
			},
			wantFailed: []string{"build", "test"},
			wantErr:    false,
		},
		{
			name:       "empty check runs",
			checkRuns:  []github.CheckRun{},
			wantFailed: nil,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := github.CheckRunsResponse{
					TotalCount: len(tt.checkRuns),
					CheckRuns:  tt.checkRuns,
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cfg := DefaultConfig()

			monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

			failed, err := monitor.GetFailedChecks(context.Background(), "abc1234")
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetFailedChecks() error = %v, wantErr %v", err, tt.wantErr)
			}

			if len(failed) != len(tt.wantFailed) {
				t.Errorf("GetFailedChecks() = %v, want %v", failed, tt.wantFailed)
			}

			for i, name := range failed {
				if i < len(tt.wantFailed) && name != tt.wantFailed[i] {
					t.Errorf("GetFailedChecks()[%d] = %s, want %s", i, name, tt.wantFailed[i])
				}
			}
		})
	}
}

func TestCIMonitor_GetFailedChecks_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	_, err := monitor.GetFailedChecks(context.Background(), "abc1234")
	if err == nil {
		t.Error("GetFailedChecks() should return error on API failure")
	}
}

func TestCIMonitor_GetCheckStatus(t *testing.T) {
	tests := []struct {
		name       string
		checkName  string
		checkRuns  []github.CheckRun
		wantStatus CIStatus
		wantErr    bool
	}{
		{
			name:      "check found - success",
			checkName: "build",
			checkRuns: []github.CheckRun{
				{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
			},
			wantStatus: CISuccess,
			wantErr:    false,
		},
		{
			name:      "check found - failure",
			checkName: "build",
			checkRuns: []github.CheckRun{
				{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionFailure},
			},
			wantStatus: CIFailure,
			wantErr:    false,
		},
		{
			name:      "check found - in progress",
			checkName: "build",
			checkRuns: []github.CheckRun{
				{Name: "build", Status: github.CheckRunInProgress, Conclusion: ""},
			},
			wantStatus: CIRunning,
			wantErr:    false,
		},
		{
			name:      "check not found",
			checkName: "nonexistent",
			checkRuns: []github.CheckRun{
				{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
			},
			wantStatus: CIPending,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := github.CheckRunsResponse{
					TotalCount: len(tt.checkRuns),
					CheckRuns:  tt.checkRuns,
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cfg := DefaultConfig()

			monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

			status, err := monitor.GetCheckStatus(context.Background(), "abc1234", tt.checkName)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetCheckStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
			if status != tt.wantStatus {
				t.Errorf("GetCheckStatus() = %s, want %s", status, tt.wantStatus)
			}
		})
	}
}

func TestCIMonitor_GetCIStatus(t *testing.T) {
	// Test GetCIStatus returns point-in-time status
	tests := []struct {
		name       string
		checkRuns  []github.CheckRun
		wantStatus CIStatus
	}{
		{
			name: "all checks success",
			checkRuns: []github.CheckRun{
				{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
				{Name: "test", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
			},
			wantStatus: CISuccess,
		},
		{
			name: "one check failing",
			checkRuns: []github.CheckRun{
				{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
				{Name: "test", Status: github.CheckRunCompleted, Conclusion: github.ConclusionFailure},
			},
			wantStatus: CIFailure,
		},
		{
			name: "one check pending",
			checkRuns: []github.CheckRun{
				{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
				{Name: "test", Status: github.CheckRunQueued, Conclusion: ""},
			},
			wantStatus: CIPending,
		},
		{
			name: "one check running",
			checkRuns: []github.CheckRun{
				{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
				{Name: "test", Status: github.CheckRunInProgress, Conclusion: ""},
			},
			wantStatus: CIPending, // Running maps to pending in aggregate
		},
		{
			name:       "no checks",
			checkRuns:  []github.CheckRun{},
			wantStatus: CIPending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := github.CheckRunsResponse{
					TotalCount: len(tt.checkRuns),
					CheckRuns:  tt.checkRuns,
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cfg := DefaultConfig()
			cfg.RequiredChecks = []string{} // Check all runs

			monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

			status, err := monitor.GetCIStatus(context.Background(), "abc1234")
			if err != nil {
				t.Fatalf("GetCIStatus() error = %v", err)
			}
			if status != tt.wantStatus {
				t.Errorf("GetCIStatus() status = %s, want %s", status, tt.wantStatus)
			}
		})
	}
}

func TestCIMonitor_GetFailedCheckLogs(t *testing.T) {
	t.Run("fetches logs for failed checks", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/repos/owner/repo/commits/abc123/check-runs":
				resp := github.CheckRunsResponse{
					TotalCount: 2,
					CheckRuns: []github.CheckRun{
						{ID: 100, Name: "lint", Status: "completed", Conclusion: "failure"},
						{ID: 101, Name: "test", Status: "completed", Conclusion: "success"},
					},
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(resp)
			case "/repos/owner/repo/actions/jobs/100/logs":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("SA5011: possible nil pointer dereference"))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
		cfg := DefaultConfig()
		monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

		logs := monitor.GetFailedCheckLogs(context.Background(), "abc123", 2000)

		if logs == "" {
			t.Fatal("expected non-empty logs")
		}
		if !contains(logs, "=== lint ===") {
			t.Error("logs should contain check name header")
		}
		if !contains(logs, "SA5011") {
			t.Error("logs should contain actual error output")
		}
	})

	t.Run("truncates to maxLen", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/repos/owner/repo/commits/abc123/check-runs":
				resp := github.CheckRunsResponse{
					TotalCount: 1,
					CheckRuns: []github.CheckRun{
						{ID: 100, Name: "lint", Status: "completed", Conclusion: "failure"},
					},
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(resp)
			case "/repos/owner/repo/actions/jobs/100/logs":
				w.WriteHeader(http.StatusOK)
				// Write a long log
				longLog := make([]byte, 5000)
				for i := range longLog {
					longLog[i] = 'x'
				}
				_, _ = w.Write(longLog)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
		cfg := DefaultConfig()
		monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

		logs := monitor.GetFailedCheckLogs(context.Background(), "abc123", 100)

		if len(logs) > 100 {
			t.Errorf("logs length = %d, want <= 100", len(logs))
		}
	})

	t.Run("graceful on log fetch failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/repos/owner/repo/commits/abc123/check-runs":
				resp := github.CheckRunsResponse{
					TotalCount: 1,
					CheckRuns: []github.CheckRun{
						{ID: 100, Name: "lint", Status: "completed", Conclusion: "failure"},
					},
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(resp)
			case "/repos/owner/repo/actions/jobs/100/logs":
				w.WriteHeader(http.StatusInternalServerError)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
		cfg := DefaultConfig()
		monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

		logs := monitor.GetFailedCheckLogs(context.Background(), "abc123", 2000)

		// Should return empty string on failure, not panic or error
		if logs != "" {
			t.Errorf("expected empty logs on fetch failure, got %q", logs)
		}
	})

	t.Run("returns empty when no failed checks", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{ID: 100, Name: "lint", Status: "completed", Conclusion: "success"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
		cfg := DefaultConfig()
		monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

		logs := monitor.GetFailedCheckLogs(context.Background(), "abc123", 2000)

		if logs != "" {
			t.Errorf("expected empty logs when no failed checks, got %q", logs)
		}
	})
}
