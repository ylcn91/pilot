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

// TestController_handleMerging_ConflictClearsLabel tests GH-880:
// When merge fails due to conflict, handleMergeConflict should be called
// which removes pilot-in-progress label so the issue can be retried.
func TestController_handleMerging_ConflictClearsLabel(t *testing.T) {
	labelRemoved := false
	prClosed := false
	commentAdded := false
	mergeAttempted := false

	mergeable := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/commits/abc1234/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "build", Status: "completed", Conclusion: "success"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/repos/owner/repo/pulls/42/merge":
			mergeAttempted = true
			// Return 405 to simulate conflict error
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"message": "Pull Request is not mergeable",
			})
		case r.URL.Path == "/repos/owner/repo/pulls/42/update-branch" && r.Method == http.MethodPut:
			// GH-1796: auto-rebase fails (true conflict), fall through to close-and-retry
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"message": "merge conflict between base and head",
			})
		case r.URL.Path == "/repos/owner/repo/pulls/42":
			if r.Method == http.MethodPatch {
				// Close PR request
				prClosed = true
				w.WriteHeader(http.StatusOK)
				return
			}
			// GET PR - return with conflict state
			pr := github.PullRequest{
				Number:         42,
				State:          "open",
				Mergeable:      &mergeable,
				MergeableState: "dirty",
				Head: github.PRRef{
					Ref: "pilot/GH-10",
					SHA: "abc1234",
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(pr)
		case r.URL.Path == "/repos/owner/repo/issues/10/labels/pilot-in-progress" && r.Method == http.MethodDelete:
			labelRemoved = true
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/repos/owner/repo/issues/42/comments" && r.Method == http.MethodPost:
			commentAdded = true
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]int{"id": 1})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.AutoReview = false
	cfg.RequiredChecks = []string{"build"}

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	// Set up PR in StageMerging state
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc1234", "pilot/GH-10", "")
	prState, _ := c.GetPRState(42)
	prState.Stage = StageMerging

	ctx := context.Background()

	// Process PR - merge should fail and trigger conflict handling
	err := c.ProcessPR(ctx, 42, nil)

	// No error returned because handleMergeConflict handles it gracefully
	if err != nil {
		t.Fatalf("ProcessPR returned error: %v", err)
	}

	if !mergeAttempted {
		t.Error("merge should have been attempted")
	}

	if !labelRemoved {
		t.Error("pilot-in-progress label should have been removed from issue")
	}

	if !prClosed {
		t.Error("PR should have been closed")
	}

	if !commentAdded {
		t.Error("comment should have been added to PR")
	}

	// PR should be in Failed state
	prState, ok := c.GetPRState(42)
	if !ok {
		t.Fatal("PR should still be tracked")
	}
	if prState.Stage != StageFailed {
		t.Errorf("Stage = %s, want %s", prState.Stage, StageFailed)
	}
	if prState.Error != "merge conflict with base branch" {
		t.Errorf("Error = %q, want 'merge conflict with base branch'", prState.Error)
	}
}

// TestController_handleMerging_Success_RemovesFailedLabel tests GH-1302:
// When a retry succeeds and PR merges, pilot-failed label should be removed.
func TestController_handleMerging_Success_RemovesFailedLabel(t *testing.T) {
	pilotDoneAdded := false
	pilotInProgressRemoved := false
	pilotFailedRemoved := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/commits/abc1234/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "build", Status: "completed", Conclusion: "success"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/repos/owner/repo/pulls/42/merge" && r.Method == http.MethodPut:
			// Successful merge
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"sha":     "merged123",
				"merged":  true,
				"message": "Pull Request successfully merged",
			})
		case r.URL.Path == "/repos/owner/repo/pulls/42" && r.Method == http.MethodGet:
			pr := github.PullRequest{
				Number: 42,
				State:  "open",
				Head: github.PRRef{
					Ref: "pilot/GH-10",
					SHA: "abc1234",
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(pr)
		case r.URL.Path == "/repos/owner/repo/issues/10/labels" && r.Method == http.MethodPost:
			pilotDoneAdded = true
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]github.Label{{Name: github.LabelDone}})
		case r.URL.Path == "/repos/owner/repo/issues/10/labels/pilot-in-progress" && r.Method == http.MethodDelete:
			pilotInProgressRemoved = true
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/repos/owner/repo/issues/10/labels/pilot-failed" && r.Method == http.MethodDelete:
			pilotFailedRemoved = true
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.AutoReview = false
	cfg.RequiredChecks = []string{"build"}

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	// Set up PR in StageMerging state
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc1234", "pilot/GH-10", "")
	prState, _ := c.GetPRState(42)
	prState.Stage = StageMerging

	ctx := context.Background()

	err := c.ProcessPR(ctx, 42, nil)
	if err != nil {
		t.Fatalf("ProcessPR returned error: %v", err)
	}

	if !pilotDoneAdded {
		t.Error("pilot-done label should have been added")
	}

	if !pilotInProgressRemoved {
		t.Error("pilot-in-progress label should have been removed")
	}

	if !pilotFailedRemoved {
		t.Error("pilot-failed label should have been removed (GH-1302)")
	}

	// PR should be in Merged state
	prState, ok := c.GetPRState(42)
	if !ok {
		t.Fatal("PR should still be tracked")
	}
	if prState.Stage != StageMerged {
		t.Errorf("Stage = %s, want %s", prState.Stage, StageMerged)
	}
}

// Test consecutive API failure counter logic
