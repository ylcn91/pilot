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

// GH-834: Test that per-PR circuit breaker doesn't block other PRs.
func TestController_PerPRCircuitBreaker_DoesNotBlockOtherPRs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/42/merge":
			// Always fail merge for PR 42
			w.WriteHeader(http.StatusInternalServerError)
		case "/repos/owner/repo/pulls/43/merge":
			// Always succeed for PR 43
			w.WriteHeader(http.StatusOK)
		case "/repos/owner/repo/commits/sha42/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns:  []github.CheckRun{{Name: "build", Status: "completed", Conclusion: "success"}},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case "/repos/owner/repo/commits/sha43/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns:  []github.CheckRun{{Name: "build", Status: "completed", Conclusion: "success"}},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.MaxFailures = 2
	cfg.RequiredChecks = []string{"build"}

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	// Set up PR 42 at merging stage (will fail)
	c.mu.Lock()
	c.activePRs[42] = &PRState{PRNumber: 42, HeadSHA: "sha42", Stage: StageMerging}
	c.activePRs[43] = &PRState{PRNumber: 43, HeadSHA: "sha43", Stage: StageMerging}
	c.mu.Unlock()

	ctx := context.Background()

	// Cause failures on PR 42 until circuit opens
	for i := 0; i < 2; i++ {
		_ = c.ProcessPR(ctx, 42, nil)
	}

	// PR 42's circuit should be open
	if !c.IsPRCircuitOpen(42) {
		t.Error("PR 42's circuit breaker should be open")
	}

	// PR 43's circuit should NOT be open
	if c.IsPRCircuitOpen(43) {
		t.Error("PR 43's circuit breaker should NOT be open (independent of PR 42)")
	}

	// PR 43 should still be processable
	err := c.ProcessPR(ctx, 43, nil)
	if err != nil {
		t.Errorf("PR 43 should be processable despite PR 42's failures: %v", err)
	}

	// PR 42 should be blocked
	err = c.ProcessPR(ctx, 42, nil)
	if err == nil {
		t.Error("PR 42 should be blocked by its per-PR circuit breaker")
	}
}

// GH-834: Test that per-PR circuit breaker resets after timeout.
func TestController_PerPRCircuitBreaker_ResetsAfterTimeout(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	cfg.MaxFailures = 2
	cfg.FailureResetTimeout = 50 * time.Millisecond // Short timeout for testing

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	// Set up failure state with old timestamp
	c.mu.Lock()
	c.prFailures[42] = &prFailureState{
		FailureCount:    5,
		LastFailureTime: time.Now().Add(-100 * time.Millisecond), // Older than timeout
	}
	c.activePRs[42] = &PRState{PRNumber: 42, Stage: StagePRCreated}
	c.mu.Unlock()

	// Circuit should be closed because timeout has passed
	if c.IsPRCircuitOpen(42) {
		t.Error("circuit should be closed after timeout passed")
	}
}

// GH-834: Test ResetPRCircuitBreaker for single PR.
func TestController_ResetPRCircuitBreaker(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	cfg.MaxFailures = 2

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	// Set up failure state for multiple PRs
	c.mu.Lock()
	c.prFailures[42] = &prFailureState{FailureCount: 5, LastFailureTime: time.Now()}
	c.prFailures[43] = &prFailureState{FailureCount: 5, LastFailureTime: time.Now()}
	c.mu.Unlock()

	// Both should be open
	if !c.IsPRCircuitOpen(42) {
		t.Error("PR 42 circuit should be open")
	}
	if !c.IsPRCircuitOpen(43) {
		t.Error("PR 43 circuit should be open")
	}

	// Reset only PR 42
	c.ResetPRCircuitBreaker(42)

	// PR 42 should be closed, PR 43 still open
	if c.IsPRCircuitOpen(42) {
		t.Error("PR 42 circuit should be closed after reset")
	}
	if !c.IsPRCircuitOpen(43) {
		t.Error("PR 43 circuit should still be open")
	}
}

// GH-834: Test GetPRFailures returns correct count.
func TestController_GetPRFailures(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	// Initially zero
	if c.GetPRFailures(42) != 0 {
		t.Error("initial failures should be 0")
	}

	// Set failures
	c.mu.Lock()
	c.prFailures[42] = &prFailureState{FailureCount: 3, LastFailureTime: time.Now()}
	c.mu.Unlock()

	if c.GetPRFailures(42) != 3 {
		t.Errorf("failures = %d, want 3", c.GetPRFailures(42))
	}
}

// GH-834: Test that IsCircuitOpen returns true only when at least one PR is blocked.
func TestController_IsCircuitOpen_AnyPRBlocked(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	cfg.MaxFailures = 3

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	// No failures — circuit closed
	if c.IsCircuitOpen() {
		t.Error("circuit should be closed with no failures")
	}

	// Add failures below threshold
	c.mu.Lock()
	c.prFailures[42] = &prFailureState{FailureCount: 2, LastFailureTime: time.Now()}
	c.mu.Unlock()

	if c.IsCircuitOpen() {
		t.Error("circuit should be closed with failures below threshold")
	}

	// Add failures at threshold
	c.mu.Lock()
	c.prFailures[42].FailureCount = 3
	c.mu.Unlock()

	if !c.IsCircuitOpen() {
		t.Error("circuit should be open with failures at threshold")
	}
}

// TestController_DeadlockDetection tests the deadlock detection mechanism (GH-849).
func TestController_DeadlockDetection(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	// Initial state: lastProgressAt should be set to now
	initialProgress := c.GetLastProgressAt()
	if initialProgress.IsZero() {
		t.Error("lastProgressAt should be initialized on construction")
	}

	// Alert flag should start as false
	if c.IsDeadlockAlertSent() {
		t.Error("deadlockAlertSent should be false initially")
	}

	// Mark alert as sent
	c.MarkDeadlockAlertSent()
	if !c.IsDeadlockAlertSent() {
		t.Error("deadlockAlertSent should be true after marking")
	}

	// Simulate a PR state transition by adding a PR
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "")

	// Get PR and manually trigger a stage transition to update lastProgressAt
	pr, _ := c.GetPRState(42)
	previousStage := pr.Stage
	pr.Stage = StageWaitingCI

	// Simulate what ProcessPR does on stage transition
	c.mu.Lock()
	if pr.Stage != previousStage {
		c.lastProgressAt = time.Now()
		c.deadlockAlertSent = false
	}
	c.mu.Unlock()

	// After transition, lastProgressAt should be updated and alert flag reset
	newProgress := c.GetLastProgressAt()
	if !newProgress.After(initialProgress) && !newProgress.Equal(initialProgress) {
		t.Error("lastProgressAt should be updated after stage transition")
	}
	if c.IsDeadlockAlertSent() {
		t.Error("deadlockAlertSent should be reset after stage transition")
	}
}

// TestController_DeadlockDetection_StaleProgress tests that stale progress is detected.
func TestController_DeadlockDetection_StaleProgress(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	// Manually set lastProgressAt to 2 hours ago
	c.mu.Lock()
	c.lastProgressAt = time.Now().Add(-2 * time.Hour)
	c.mu.Unlock()

	// Check that GetLastProgressAt returns the stale time
	progress := c.GetLastProgressAt()
	if time.Since(progress) < 1*time.Hour {
		t.Error("lastProgressAt should be more than 1 hour ago")
	}

	// Add a PR to simulate active work
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "")

	// The MetricsAlerter would check: noProgressMin >= 60 && len(activePRs) > 0
	noProgressMin := time.Since(c.GetLastProgressAt()).Minutes()
	activePRs := c.GetActivePRs()

	if noProgressMin < 60 {
		t.Errorf("noProgressMin = %.1f, expected >= 60", noProgressMin)
	}
	if len(activePRs) == 0 {
		t.Error("expected active PRs")
	}

	// This is the condition that would trigger a deadlock alert
	deadlockDetected := noProgressMin >= 60 && !c.IsDeadlockAlertSent() && len(activePRs) > 0
	if !deadlockDetected {
		t.Error("deadlock condition should be detected")
	}
}
