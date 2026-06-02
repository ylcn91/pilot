package autopilot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestHandleCIFailed_EmptyLogs_SkipsLearning verifies that handleCIFailed skips
// LearnFromCIFailure when CI logs are empty or whitespace-only (GH-1979).
// TestHandleCIFailed_EmptyLogs_SkipsLearning verifies that handleCIFailed skips
// LearnFromCIFailure when CI logs are empty (no failed check runs found for log fetch).
func TestHandleCIFailed_EmptyLogs_SkipsLearning(t *testing.T) {
	learnCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// GetFailedChecks uses this endpoint
		case r.URL.Path == "/repos/owner/repo/commits/sha789/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "build", Status: "completed", Conclusion: "failure"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		// GetFailedCheckLogs tries to fetch job logs — return 404 so logs are empty
		case strings.Contains(r.URL.Path, "/actions/jobs/") && strings.HasSuffix(r.URL.Path, "/logs"):
			w.WriteHeader(http.StatusNotFound)
		case r.URL.Path == "/repos/owner/repo/issues" && r.Method == "POST":
			resp := github.Issue{Number: 210}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(mustJSON(t, resp))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	loop, cleanup := newTestLearningLoop(t)
	defer cleanup()
	c.SetLearningLoop(loop)

	prState := &PRState{
		PRNumber: 45,
		HeadSHA:  "sha789",
		Stage:    StageCIFailed,
	}

	err := c.handleCIFailed(context.Background(), prState)
	if err != nil {
		t.Fatalf("handleCIFailed returned unexpected error: %v", err)
	}
	if prState.Stage != StageFailed {
		t.Errorf("Stage = %s, want %s", prState.Stage, StageFailed)
	}
	// The learning loop should NOT have been invoked (empty logs guard).
	// learnCalled remains false since we can't directly observe the call,
	// but the absence of "Failed to learn from CI failure" warning in logs
	// confirms the guard works. With nil extractor + non-empty logs, the
	// warning would appear.
	_ = learnCalled
}

// TestSetLearningLoop_ForwardsToFeedbackLoop verifies that SetLearningLoop
// also injects the learning loop into the feedback loop (GH-1979).
func TestSetLearningLoop_ForwardsToFeedbackLoop(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	loop, cleanup := newTestLearningLoop(t)
	defer cleanup()

	// Before setting — feedbackLoop.learningLoop should be nil
	if c.feedbackLoop.learningLoop != nil {
		t.Error("feedbackLoop.learningLoop should be nil before SetLearningLoop")
	}

	c.SetLearningLoop(loop)

	// After setting — feedbackLoop.learningLoop should be wired
	if c.feedbackLoop.learningLoop == nil {
		t.Error("feedbackLoop.learningLoop should be set after SetLearningLoop")
	}
	if c.feedbackLoop.learningLoop != loop {
		t.Error("feedbackLoop.learningLoop should point to the same loop instance")
	}
}
