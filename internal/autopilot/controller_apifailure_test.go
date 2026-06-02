package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

func TestController_ConsecutiveAPIFailures(t *testing.T) {
	// Mock HTTP server that always returns error for check runs API
	var apiCallCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCallCount++
		if strings.Contains(r.URL.Path, "check-runs") {
			// Return error for CI checks
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"API Error"}`))
			return
		}
		// Default PR response for GetPullRequest calls
		if strings.Contains(r.URL.Path, "/pulls/") {
			pr := map[string]interface{}{
				"number":    42,
				"state":     "open",
				"merged":    false,
				"mergeable": true,
				"head": map[string]interface{}{
					"sha": "abc1234",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(pr)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	cfg := DefaultConfig()
	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc1234", "pilot/GH-10", "")

	// Set PR to waiting CI stage
	prState, _ := c.GetPRState(42)
	prState.Stage = StageWaitingCI

	ctx := context.Background()

	// Call ProcessPR 4 times - failures should increment but not transition to StageFailed yet
	for i := 1; i <= 4; i++ {
		err := c.ProcessPR(ctx, 42, nil)
		if err != nil {
			t.Fatalf("ProcessPR iteration %d error: %v", i, err)
		}

		prState, _ = c.GetPRState(42)
		if prState.ConsecutiveAPIFailures != i {
			t.Errorf("after %d failures: ConsecutiveAPIFailures = %d, want %d", i, prState.ConsecutiveAPIFailures, i)
		}
		if prState.Stage != StageWaitingCI {
			t.Errorf("after %d failures: Stage = %s, want %s", i, prState.Stage, StageWaitingCI)
		}
	}

	// 5th failure should transition to StageFailed
	err := c.ProcessPR(ctx, 42, nil)
	if err != nil {
		t.Fatalf("ProcessPR 5th iteration error: %v", err)
	}

	prState, _ = c.GetPRState(42)
	if prState.ConsecutiveAPIFailures != 5 {
		t.Errorf("after 5 failures: ConsecutiveAPIFailures = %d, want 5", prState.ConsecutiveAPIFailures)
	}
	if prState.Stage != StageFailed {
		t.Errorf("after 5 failures: Stage = %s, want %s", prState.Stage, StageFailed)
	}
	if !strings.Contains(prState.Error, "CI check API failed 5 consecutive times") {
		t.Errorf("Error = %q, should contain consecutive API failure message", prState.Error)
	}
}

// Test that consecutive failure counter resets on successful API call
func TestController_ConsecutiveAPIFailures_Reset(t *testing.T) {
	// Mock HTTP server that fails 3 times then succeeds
	var apiCallCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "check-runs") {
			apiCallCount++
			if apiCallCount <= 3 {
				// Return error for first 3 CI checks
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"API Error"}`))
				return
			}
			// Success on 4th call - return successful CI
			response := map[string]interface{}{
				"total_count": 1,
				"check_runs": []map[string]interface{}{
					{
						"name":       "build",
						"status":     "completed",
						"conclusion": "success",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
			return
		}
		// Default PR response for GetPullRequest calls
		if strings.Contains(r.URL.Path, "/pulls/") {
			pr := map[string]interface{}{
				"number":    42,
				"state":     "open",
				"merged":    false,
				"mergeable": true,
				"head": map[string]interface{}{
					"sha": "abc1234",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(pr)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	cfg := DefaultConfig()
	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc1234", "pilot/GH-10", "")

	// Set PR to waiting CI stage
	prState, _ := c.GetPRState(42)
	prState.Stage = StageWaitingCI

	ctx := context.Background()

	// Call ProcessPR 3 times with failures
	for i := 1; i <= 3; i++ {
		err := c.ProcessPR(ctx, 42, nil)
		if err != nil {
			t.Fatalf("ProcessPR iteration %d error: %v", i, err)
		}
		prState, _ = c.GetPRState(42)
		if prState.ConsecutiveAPIFailures != i {
			t.Errorf("after %d failures: ConsecutiveAPIFailures = %d, want %d", i, prState.ConsecutiveAPIFailures, i)
		}
	}

	// 4th call succeeds - counter should reset and transition to StageCIPassed
	err := c.ProcessPR(ctx, 42, nil)
	if err != nil {
		t.Fatalf("ProcessPR 4th iteration (success) error: %v", err)
	}

	prState, _ = c.GetPRState(42)
	if prState.ConsecutiveAPIFailures != 0 {
		t.Errorf("after success: ConsecutiveAPIFailures = %d, want 0", prState.ConsecutiveAPIFailures)
	}
	if prState.Stage != StageCIPassed {
		t.Errorf("after success: Stage = %s, want %s", prState.Stage, StageCIPassed)
	}
}
