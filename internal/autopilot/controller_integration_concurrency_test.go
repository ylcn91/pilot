//go:build integration

package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
)

// TestController_Integration_CircuitBreaker tests per-PR circuit breaker
func TestController_Integration_CircuitBreaker(t *testing.T) {
	mergeAttempts := 0

	server := setupMockGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/test/repo/pulls/4" && r.Method == "GET":
			// Return valid PR data to progress through states
			json.NewEncoder(w).Encode(github.PullRequest{
				Number:    4,
				Head:      github.PRRef{SHA: "cbsha123"},
				Mergeable: integrationBoolPtr(true),
			})

		case r.URL.Path == "/repos/test/repo/commits/cbsha123/check-runs" && r.Method == "GET":
			// CI passes
			json.NewEncoder(w).Encode(map[string]interface{}{
				"total_count": 1,
				"check_runs": []map[string]interface{}{
					{"name": "CI", "status": "completed", "conclusion": "success"},
				},
			})

		case r.URL.Path == "/repos/test/repo/pulls/4/merge" && r.Method == "PUT":
			// Merge always fails - this triggers circuit breaker
			mergeAttempts++
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message": "Pull Request is not mergeable",
			})

		default:
			w.WriteHeader(http.StatusOK)
		}
	})
	defer server.Close()

	ghClient := github.NewClientWithBaseURL("test-token", server.URL)
	cfg := &Config{
		Enabled:             true,
		Environment:         EnvDev,
		AutoMerge:           true,
		CIWaitTimeout:       1 * time.Minute,
		CIPollInterval:      50 * time.Millisecond,
		MaxFailures:         3, // Circuit breaker threshold
		FailureResetTimeout: 1 * time.Hour,
		CIChecks: &CIChecksConfig{
			Mode:                 "auto",
			DiscoveryGracePeriod: 50 * time.Millisecond,
		},
	}

	controller := NewController(cfg, ghClient, nil, "test", "repo")

	controller.OnPRCreated(4, "https://github.com/test/repo/pull/4", 400, "cbsha123", "pilot/GH-400")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Process through state machine until we hit merge failures
	// PRCreated -> WaitingCI -> CIPassed -> Merging (fails repeatedly)
	for i := 0; i < 10; i++ {
		_ = controller.ProcessPR(ctx, 4, nil)

		// Once circuit is open, stop
		if controller.isPRCircuitOpen(4) {
			break
		}
	}

	// Check if circuit is open after merge failures
	if !controller.isPRCircuitOpen(4) {
		t.Errorf("Expected per-PR circuit breaker to be open after merge failures (attempts: %d)", mergeAttempts)
	}
}

// TestController_Integration_MultiplePRs tests concurrent PR handling
func TestController_Integration_MultiplePRs(t *testing.T) {
	server := setupMockGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/test/repo/pulls/10" && r.Method == "GET":
			json.NewEncoder(w).Encode(github.PullRequest{
				Number: 10,
				Head:   github.PRRef{SHA: "sha10"},
			})
		case r.URL.Path == "/repos/test/repo/pulls/11" && r.Method == "GET":
			json.NewEncoder(w).Encode(github.PullRequest{
				Number: 11,
				Head:   github.PRRef{SHA: "sha11"},
			})
		case r.URL.Path == "/repos/test/repo/pulls/12" && r.Method == "GET":
			json.NewEncoder(w).Encode(github.PullRequest{
				Number: 12,
				Head:   github.PRRef{SHA: "sha12"},
			})
		default:
			w.WriteHeader(http.StatusOK)
		}
	})
	defer server.Close()

	ghClient := github.NewClientWithBaseURL("test-token", server.URL)
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.CIPollInterval = 50 * time.Millisecond

	controller := NewController(cfg, ghClient, nil, "test", "repo")

	// Register multiple PRs
	controller.OnPRCreated(10, "https://github.com/test/repo/pull/10", 1000, "sha10", "pilot/GH-1000")
	controller.OnPRCreated(11, "https://github.com/test/repo/pull/11", 1001, "sha11", "pilot/GH-1001")
	controller.OnPRCreated(12, "https://github.com/test/repo/pull/12", 1002, "sha12", "pilot/GH-1002")

	// Verify all PRs are tracked
	if len(controller.activePRs) != 3 {
		t.Errorf("Expected 3 active PRs, got %d", len(controller.activePRs))
	}

	// Each PR should be independent
	for prNum, prState := range controller.activePRs {
		if prState.Stage != StagePRCreated {
			t.Errorf("PR %d: expected initial stage PRCreated, got %s", prNum, prState.Stage)
		}
	}
}
