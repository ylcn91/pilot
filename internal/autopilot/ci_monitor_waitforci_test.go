package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

func TestCIMonitor_WaitForCI_Success(t *testing.T) {
	// Mock GitHub client returning success after 2 polls
	pollCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pollCount++
		resp := github.CheckRunsResponse{
			TotalCount: 3,
			CheckRuns: []github.CheckRun{
				{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
				{Name: "test", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
				{Name: "lint", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
			},
		}
		// First poll: pending, subsequent polls: success
		if pollCount == 1 {
			resp.CheckRuns[0].Status = github.CheckRunInProgress
			resp.CheckRuns[0].Conclusion = ""
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.CIWaitTimeout = 1 * time.Second
	cfg.RequiredChecks = []string{"build", "test", "lint"}

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	status, err := monitor.WaitForCI(context.Background(), "abc1234")
	if err != nil {
		t.Fatalf("WaitForCI() error = %v", err)
	}
	if status != CISuccess {
		t.Errorf("WaitForCI() status = %s, want %s", status, CISuccess)
	}
	if pollCount < 2 {
		t.Errorf("expected at least 2 polls, got %d", pollCount)
	}
}

func TestCIMonitor_WaitForCI_Failure(t *testing.T) {
	// Mock GitHub client returning failure
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := github.CheckRunsResponse{
			TotalCount: 3,
			CheckRuns: []github.CheckRun{
				{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionFailure},
				{Name: "test", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
				{Name: "lint", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.CIWaitTimeout = 1 * time.Second
	cfg.RequiredChecks = []string{"build", "test", "lint"}

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	status, err := monitor.WaitForCI(context.Background(), "abc1234")
	if err != nil {
		t.Fatalf("WaitForCI() error = %v", err)
	}
	if status != CIFailure {
		t.Errorf("WaitForCI() status = %s, want %s", status, CIFailure)
	}
}

func TestCIMonitor_WaitForCI_Timeout(t *testing.T) {
	// Mock GitHub client always returning pending
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := github.CheckRunsResponse{
			TotalCount: 1,
			CheckRuns: []github.CheckRun{
				{Name: "build", Status: github.CheckRunInProgress, Conclusion: ""},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.CIWaitTimeout = 50 * time.Millisecond
	cfg.RequiredChecks = []string{"build"}

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	status, err := monitor.WaitForCI(context.Background(), "abc1234")
	if err == nil {
		t.Fatal("WaitForCI() should return timeout error")
	}
	if status != CIPending {
		t.Errorf("WaitForCI() status = %s, want %s", status, CIPending)
	}
}

func TestCIMonitor_WaitForCI_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := github.CheckRunsResponse{
			TotalCount: 1,
			CheckRuns: []github.CheckRun{
				{Name: "build", Status: github.CheckRunInProgress, Conclusion: ""},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.CIPollInterval = 100 * time.Millisecond
	cfg.CIWaitTimeout = 10 * time.Second

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	status, err := monitor.WaitForCI(ctx, "abc1234")
	if err == nil {
		t.Fatal("WaitForCI() should return error on context cancellation")
	}
	if status != CIPending {
		t.Errorf("WaitForCI() status = %s, want %s", status, CIPending)
	}
}

func TestCIMonitor_RequiredChecksOnly(t *testing.T) {
	// Verify only configured checks are monitored
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := github.CheckRunsResponse{
			TotalCount: 4,
			CheckRuns: []github.CheckRun{
				{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
				{Name: "test", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
				{Name: "lint", Status: github.CheckRunCompleted, Conclusion: github.ConclusionFailure}, // Fails but not required
				{Name: "coverage", Status: github.CheckRunInProgress, Conclusion: ""},                  // Still running but not required
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.CIWaitTimeout = 1 * time.Second
	// Use manual mode with specific required checks
	cfg.CIChecks = &CIChecksConfig{
		Mode:     "manual",
		Required: []string{"build", "test"},
	}

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	status, err := monitor.WaitForCI(context.Background(), "abc1234")
	if err != nil {
		t.Fatalf("WaitForCI() error = %v", err)
	}
	if status != CISuccess {
		t.Errorf("WaitForCI() status = %s, want %s (unrequired checks should be ignored)", status, CISuccess)
	}
}

func TestCIMonitor_NoRequiredChecks(t *testing.T) {
	// When no required checks are configured, all checks are monitored
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := github.CheckRunsResponse{
			TotalCount: 2,
			CheckRuns: []github.CheckRun{
				{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
				{Name: "test", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.CIWaitTimeout = 1 * time.Second
	cfg.RequiredChecks = []string{} // No required checks

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	status, err := monitor.WaitForCI(context.Background(), "abc1234")
	if err != nil {
		t.Fatalf("WaitForCI() error = %v", err)
	}
	if status != CISuccess {
		t.Errorf("WaitForCI() status = %s, want %s", status, CISuccess)
	}
}

func TestCIMonitor_NoChecks(t *testing.T) {
	// When no checks exist at all, return pending
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := github.CheckRunsResponse{
			TotalCount: 0,
			CheckRuns:  []github.CheckRun{},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.CIWaitTimeout = 50 * time.Millisecond
	cfg.RequiredChecks = []string{} // No required checks, monitor all

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	_, err := monitor.WaitForCI(context.Background(), "abc1234")
	// Should timeout because no checks exist and status stays pending
	if err == nil {
		t.Fatal("WaitForCI() should timeout when no checks exist")
	}
}

func TestCIMonitor_WaitForCI_APIErrorContinues(t *testing.T) {
	// Test that API errors during polling are logged but don't fail the wait
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call fails
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Subsequent calls succeed
		resp := github.CheckRunsResponse{
			TotalCount: 1,
			CheckRuns: []github.CheckRun{
				{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.CIWaitTimeout = 1 * time.Second
	cfg.RequiredChecks = []string{"build"}

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	status, err := monitor.WaitForCI(context.Background(), "abc1234")
	if err != nil {
		t.Fatalf("WaitForCI() should recover from API errors: %v", err)
	}
	if status != CISuccess {
		t.Errorf("WaitForCI() status = %s, want %s", status, CISuccess)
	}
	if callCount < 2 {
		t.Errorf("expected at least 2 calls, got %d", callCount)
	}
}

func TestCIMonitor_WaitForCI_RequiredCheckNotFound(t *testing.T) {
	// Test behavior when a required check doesn't exist in the response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := github.CheckRunsResponse{
			TotalCount: 1,
			CheckRuns: []github.CheckRun{
				// Only 'build' exists, but 'test' is required
				{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.CIWaitTimeout = 50 * time.Millisecond
	// Use manual mode with specific required checks
	cfg.CIChecks = &CIChecksConfig{
		Mode:     "manual",
		Required: []string{"build", "test"}, // 'test' doesn't exist
	}

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	// Should timeout because 'test' is pending (not found)
	_, err := monitor.WaitForCI(context.Background(), "abc1234")
	if err == nil {
		t.Fatal("WaitForCI() should timeout when required check is missing")
	}
}
