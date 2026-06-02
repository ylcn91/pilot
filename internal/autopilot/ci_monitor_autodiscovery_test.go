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

func TestCIMonitor_AutoDiscovery(t *testing.T) {
	// Test auto mode discovers checks from API and returns correct status
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := github.CheckRunsResponse{
			TotalCount: 3,
			CheckRuns: []github.CheckRun{
				{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
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
	// Use auto mode (default when no RequiredChecks set)
	cfg.RequiredChecks = nil
	cfg.CIChecks = &CIChecksConfig{
		Mode:                 "auto",
		DiscoveryGracePeriod: 10 * time.Millisecond,
	}

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	status, err := monitor.CheckCI(context.Background(), "abc1234")
	if err != nil {
		t.Fatalf("CheckCI() error = %v", err)
	}
	if status != CISuccess {
		t.Errorf("CheckCI() status = %s, want %s", status, CISuccess)
	}

	// Verify discovered checks are stored
	discovered := monitor.GetDiscoveredChecks("abc1234")
	if len(discovered) != 3 {
		t.Errorf("GetDiscoveredChecks() = %v, want 3 checks", discovered)
	}
}

func TestCIMonitor_AutoDiscovery_WithExclusions(t *testing.T) {
	// Test auto mode excludes checks matching patterns
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := github.CheckRunsResponse{
			TotalCount: 4,
			CheckRuns: []github.CheckRun{
				{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
				{Name: "test", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
				{Name: "codecov/patch", Status: github.CheckRunCompleted, Conclusion: github.ConclusionFailure},     // Should be excluded
				{Name: "coverage-optional", Status: github.CheckRunCompleted, Conclusion: github.ConclusionFailure}, // Should be excluded
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
	cfg.RequiredChecks = nil
	cfg.CIChecks = &CIChecksConfig{
		Mode:                 "auto",
		Exclude:              []string{"codecov/*", "*-optional"},
		DiscoveryGracePeriod: 10 * time.Millisecond,
	}

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	// Should succeed because the failing checks are excluded
	status, err := monitor.CheckCI(context.Background(), "abc1234")
	if err != nil {
		t.Fatalf("CheckCI() error = %v", err)
	}
	if status != CISuccess {
		t.Errorf("CheckCI() status = %s, want %s (excluded checks should be ignored)", status, CISuccess)
	}

	// Verify only non-excluded checks are discovered
	discovered := monitor.GetDiscoveredChecks("abc1234")
	if len(discovered) != 2 {
		t.Errorf("GetDiscoveredChecks() = %v, want 2 checks (build, test)", discovered)
	}
}

func TestCIMonitor_matchesExclude_GlobPatterns(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	cfg.CIChecks = &CIChecksConfig{
		Mode: "auto",
		Exclude: []string{
			"codecov/*",       // Glob pattern
			"*-optional",      // Glob pattern
			"skip-me",         // Exact match
			"prefix-*-suffix", // Complex glob
		},
	}

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	tests := []struct {
		name string
		want bool
	}{
		{"codecov/patch", true},
		{"codecov/project", true},
		{"codecov", false}, // No glob match
		{"test-optional", true},
		{"optional", false}, // Doesn't match *-optional
		{"skip-me", true},   // Exact match
		{"skip-me-too", false},
		{"prefix-x-suffix", true},
		{"prefix--suffix", true},
		{"build", false},
		{"test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := monitor.matchesExclude(tt.name)
			if got != tt.want {
				t.Errorf("matchesExclude(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestCIMonitor_AutoDiscovery_GracePeriod(t *testing.T) {
	// Test that auto mode waits during grace period when no checks exist
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := github.CheckRunsResponse{
			TotalCount: 0,
			CheckRuns:  []github.CheckRun{},
		}
		// After grace period (3rd call), return checks
		if callCount >= 3 {
			resp = github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
				},
			}
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.CIWaitTimeout = 1 * time.Second
	cfg.RequiredChecks = nil
	cfg.CIChecks = &CIChecksConfig{
		Mode:                 "auto",
		DiscoveryGracePeriod: 50 * time.Millisecond,
	}

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	// Wait for CI - should eventually succeed when checks appear
	status, err := monitor.WaitForCI(context.Background(), "abc1234")
	if err != nil {
		t.Fatalf("WaitForCI() error = %v", err)
	}
	if status != CISuccess {
		t.Errorf("WaitForCI() status = %s, want %s", status, CISuccess)
	}
	if callCount < 3 {
		t.Errorf("expected at least 3 calls (waiting for checks), got %d", callCount)
	}
}

func TestCIMonitor_AutoDiscovery_GracePeriodExpired(t *testing.T) {
	// Test that auto mode returns success if grace period expires with no checks
	// and no commit statuses (genuine no-CI repo).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/commits/abc1234/check-runs":
			resp := github.CheckRunsResponse{TotalCount: 0, CheckRuns: []github.CheckRun{}}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case "/repos/owner/repo/commits/abc1234/status":
			// No commit statuses → genuine no-CI repo
			resp := github.CombinedStatus{State: github.StatusPending, TotalCount: 0, Statuses: []github.CommitStatus{}}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.CIWaitTimeout = 1 * time.Second
	cfg.RequiredChecks = nil
	cfg.CIChecks = &CIChecksConfig{
		Mode:                 "auto",
		DiscoveryGracePeriod: 20 * time.Millisecond, // Very short grace period
	}

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	// First call starts grace period
	status, _ := monitor.CheckCI(context.Background(), "abc1234")
	if status != CIPending {
		t.Errorf("First CheckCI() status = %s, want %s", status, CIPending)
	}

	// Wait for grace period to expire
	time.Sleep(30 * time.Millisecond)

	// Second call should return success after grace period expired
	status, err := monitor.CheckCI(context.Background(), "abc1234")
	if err != nil {
		t.Fatalf("CheckCI() error = %v", err)
	}
	if status != CISuccess {
		t.Errorf("CheckCI() after grace period status = %s, want %s", status, CISuccess)
	}
}

// TestCIMonitor_CommitStatusFallback tests the fallback to the GitHub commit-status
// API when check-runs returns empty. Providers like CircleCI, Jenkins, and Travis
// use the statuses API exclusively, so empty check-runs must not auto-approve.
func TestCIMonitor_CommitStatusFallback(t *testing.T) {
	tests := []struct {
		name          string
		combinedState string
		totalCount    int
		wantStatus    CIStatus
	}{
		{
			name:          "failing combined status → CIFailure",
			combinedState: github.StatusFailure,
			totalCount:    1,
			wantStatus:    CIFailure,
		},
		{
			name:          "error combined status → CIFailure",
			combinedState: github.StatusError,
			totalCount:    1,
			wantStatus:    CIFailure,
		},
		{
			name:          "pending combined status → CIPending",
			combinedState: github.StatusPending,
			totalCount:    1,
			wantStatus:    CIPending,
		},
		{
			name:          "empty combined statuses (TotalCount=0) → CISuccess",
			combinedState: github.StatusPending, // GitHub returns "pending" with no statuses
			totalCount:    0,
			wantStatus:    CISuccess,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/owner/repo/commits/abc1234/check-runs":
					resp := github.CheckRunsResponse{TotalCount: 0, CheckRuns: []github.CheckRun{}}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(resp)
				case "/repos/owner/repo/commits/abc1234/status":
					resp := github.CombinedStatus{
						State:      tt.combinedState,
						TotalCount: tt.totalCount,
					}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(resp)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cfg := DefaultConfig()
			cfg.RequiredChecks = nil
			cfg.CIChecks = &CIChecksConfig{
				Mode:                 "auto",
				DiscoveryGracePeriod: 20 * time.Millisecond,
			}
			monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

			// First call starts grace period
			firstStatus, _ := monitor.CheckCI(context.Background(), "abc1234")
			if firstStatus != CIPending {
				t.Errorf("first CheckCI() = %s, want %s", firstStatus, CIPending)
			}

			// Wait for grace period to expire
			time.Sleep(30 * time.Millisecond)

			// Second call: grace period expired, fallback to commit-status API
			status, err := monitor.CheckCI(context.Background(), "abc1234")
			if err != nil {
				t.Fatalf("CheckCI() error = %v", err)
			}
			if status != tt.wantStatus {
				t.Errorf("CheckCI() after grace period = %s, want %s", status, tt.wantStatus)
			}
		})
	}
}

func TestCIMonitor_CommitStatusFallback_APIError(t *testing.T) {
	// When GetCombinedStatus fails, treat as no CI configured (success).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/commits/abc1234/check-runs":
			resp := github.CheckRunsResponse{TotalCount: 0, CheckRuns: []github.CheckRun{}}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case "/repos/owner/repo/commits/abc1234/status":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.RequiredChecks = nil
	cfg.CIChecks = &CIChecksConfig{
		Mode:                 "auto",
		DiscoveryGracePeriod: 20 * time.Millisecond,
	}
	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	// Start grace period
	_, _ = monitor.CheckCI(context.Background(), "abc1234")
	time.Sleep(30 * time.Millisecond)

	// After grace period, status API fails → treat as no CI (success)
	status, err := monitor.CheckCI(context.Background(), "abc1234")
	if err != nil {
		t.Fatalf("CheckCI() error = %v", err)
	}
	if status != CISuccess {
		t.Errorf("CheckCI() on status API error = %s, want %s (degrade gracefully)", status, CISuccess)
	}
}
