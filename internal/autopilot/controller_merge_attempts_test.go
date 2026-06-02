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

// TestController_HandleMerging_MergeAttemptCapEscalates verifies that when a PR
// reaches MaxMergeAttempts on a non-conflict merge failure it transitions to
// StageFailed (never retried) and posts an escalation comment on the issue.
// TASK-336 / B5.
func TestController_HandleMerging_MergeAttemptCapEscalates(t *testing.T) {
	var (
		escalationCommentPosted bool
		prFetched               bool
	)

	mergeable := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/pulls/88/merge" && r.Method == http.MethodPost:
			// Simulate a persistent non-conflict merge failure (e.g. branch-protection).
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`{"message":"405 Method Not Allowed"}`))

		case r.URL.Path == "/repos/owner/repo/pulls/88" && r.Method == http.MethodGet:
			prFetched = true
			resp := github.PullRequest{
				Number:         88,
				Head:           github.PRRef{SHA: "sha88"},
				Mergeable:      &mergeable,
				MergeableState: "clean",
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)

		case r.URL.Path == "/repos/owner/repo/issues/40/comments" && r.Method == http.MethodPost:
			escalationCommentPosted = true
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(github.PRComment{ID: 1})

		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.MaxMergeAttempts = 3

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.mu.Lock()
	// MergeAttempts is 2; handleMerging will increment to 3 == MaxMergeAttempts → cap fires.
	c.activePRs[88] = &PRState{
		PRNumber:      88,
		PRURL:         "https://github.com/owner/repo/pull/88",
		IssueNumber:   40,
		BranchName:    "pilot/GH-40",
		HeadSHA:       "sha88",
		Stage:         StageMerging,
		MergeAttempts: 2,
		CreatedAt:     time.Now(),
	}
	c.mu.Unlock()

	err := c.ProcessPR(context.Background(), 88, nil)
	if err != nil {
		t.Fatalf("ProcessPR returned unexpected error: %v", err)
	}

	pr, ok := c.GetPRState(88)
	if !ok {
		t.Fatal("PR 88 not found in activePRs")
	}
	if pr.Stage != StageFailed {
		t.Errorf("Stage = %s, want %s", pr.Stage, StageFailed)
	}
	if pr.MergeAttempts != 3 {
		t.Errorf("MergeAttempts = %d, want 3", pr.MergeAttempts)
	}
	if !prFetched {
		t.Error("GetPullRequest should have been called to check for conflict")
	}
	if !escalationCommentPosted {
		t.Error("escalation comment should have been posted on the issue")
	}
}

// TestController_HandleMerging_BelowCapReturnsRetryableError verifies that a merge
// failure when MergeAttempts is still below MaxMergeAttempts returns a retryable
// error (Stage stays StageMerging) rather than transitioning to StageFailed.
// TASK-336 / B5.
func TestController_HandleMerging_BelowCapReturnsRetryableError(t *testing.T) {
	mergeable := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/pulls/66/merge" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`{"message":"405 Method Not Allowed"}`))

		case r.URL.Path == "/repos/owner/repo/pulls/66" && r.Method == http.MethodGet:
			resp := github.PullRequest{
				Number:         66,
				Head:           github.PRRef{SHA: "sha66"},
				Mergeable:      &mergeable,
				MergeableState: "clean",
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)

		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.MaxMergeAttempts = 5
	cfg.MaxFailures = 100 // disable circuit breaker for this test

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.mu.Lock()
	// MergeAttempts is 1; handleMerging will increment to 2 < MaxMergeAttempts(5) → retryable.
	c.activePRs[66] = &PRState{
		PRNumber:      66,
		PRURL:         "https://github.com/owner/repo/pull/66",
		IssueNumber:   0,
		BranchName:    "pilot/GH-66",
		HeadSHA:       "sha66",
		Stage:         StageMerging,
		MergeAttempts: 1,
		CreatedAt:     time.Now(),
	}
	c.mu.Unlock()

	err := c.ProcessPR(context.Background(), 66, nil)
	if err == nil {
		t.Fatal("ProcessPR should have returned an error for a below-cap merge failure")
	}

	pr, ok := c.GetPRState(66)
	if !ok {
		t.Fatal("PR 66 not found in activePRs")
	}
	if pr.Stage != StageMerging {
		t.Errorf("Stage = %s, want %s (below-cap failure must stay retryable)", pr.Stage, StageMerging)
	}
	if pr.MergeAttempts != 2 {
		t.Errorf("MergeAttempts = %d, want 2", pr.MergeAttempts)
	}
}
