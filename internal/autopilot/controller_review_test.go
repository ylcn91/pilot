package autopilot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

func TestController_HandleReviewRequested_CreatesIssue(t *testing.T) {
	issueCreated := false
	prClosed := false
	branchDeleted := false
	notified := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/pulls/42/reviews":
			resp := []*github.PullRequestReview{
				{ID: 1, User: github.User{Login: "alice"}, Body: "Fix the nil check", State: "CHANGES_REQUESTED", SubmittedAt: "2026-03-05T10:00:00Z"},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/pulls/42/comments":
			resp := []*github.PRReviewComment{
				{ID: 10, Body: "Add error handling", Path: "foo.go", Line: 5, User: github.User{Login: "alice"}},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/issues" && r.Method == http.MethodPost:
			issueCreated = true
			resp := github.Issue{Number: 100}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/pulls/42" && r.Method == http.MethodPatch:
			prClosed = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, github.PullRequest{Number: 42, State: "closed"}))
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/git/refs/heads/") && r.Method == http.MethodDelete:
			branchDeleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.ReviewFeedback = &ReviewFeedbackConfig{Enabled: true, MaxIterations: 3}
	cfg.AutoCreateIssues = true

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.SetNotifier(&mockNotifier{
		notifyFixIssueCreatedFunc: func(ctx context.Context, prState *PRState, issueNumber int) error {
			notified = true
			return nil
		},
	})

	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "")

	// Transition to review_requested
	c.mu.Lock()
	c.activePRs[42].Stage = StageReviewRequested
	c.mu.Unlock()

	err := c.ProcessPR(context.Background(), 42, nil)
	if err != nil {
		t.Fatalf("ProcessPR error: %v", err)
	}

	if !issueCreated {
		t.Error("expected review issue to be created")
	}
	if !prClosed {
		t.Error("expected PR to be closed")
	}
	if !branchDeleted {
		t.Error("expected branch to be deleted")
	}
	if !notified {
		t.Error("expected notification to be sent")
	}

	pr, ok := c.GetPRState(42)
	if !ok {
		t.Fatal("PR should still be tracked")
	}
	if pr.Stage != StageFailed {
		t.Errorf("stage = %s, want %s", pr.Stage, StageFailed)
	}
}

func TestController_HandleReviewRequested_IterationLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/pulls/42/reviews":
			resp := []*github.PullRequestReview{
				{ID: 1, User: github.User{Login: "alice"}, Body: "Still broken", State: "CHANGES_REQUESTED"},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/pulls/42/comments":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("[]"))
		case r.URL.Path == "/repos/owner/repo/issues/10":
			// Return issue with iteration=3 metadata (at limit)
			resp := github.Issue{
				Number: 10,
				Body:   "some body\n<!-- autopilot-meta branch:pilot/GH-10 pr:42 iteration:3 -->",
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/pulls/42" && r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, github.PullRequest{Number: 42, State: "closed"}))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.ReviewFeedback = &ReviewFeedbackConfig{Enabled: true, MaxIterations: 3}

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "")

	c.mu.Lock()
	c.activePRs[42].Stage = StageReviewRequested
	c.mu.Unlock()

	err := c.ProcessPR(context.Background(), 42, nil)
	if err != nil {
		t.Fatalf("ProcessPR error: %v", err)
	}

	pr, ok := c.GetPRState(42)
	if !ok {
		t.Fatal("PR should still be tracked")
	}
	if pr.Stage != StageFailed {
		t.Errorf("stage = %s, want %s", pr.Stage, StageFailed)
	}
	if !strings.Contains(pr.Error, "iteration limit") {
		t.Errorf("error should mention iteration limit: %s", pr.Error)
	}
}

func TestController_HandleReviewRequested_IgnoresSelfReview(t *testing.T) {
	// hasChangesRequested should skip bot reviews
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/42/reviews":
			resp := []*github.PullRequestReview{
				{ID: 1, User: github.User{Login: "pilot[bot]"}, Body: "Self-review", State: "CHANGES_REQUESTED", SubmittedAt: "2026-03-05T10:00:00Z"},
				{ID: 2, User: github.User{Login: "ci-bot"}, Body: "Bot review", State: "CHANGES_REQUESTED", SubmittedAt: "2026-03-05T10:00:00Z"},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.ReviewFeedback = &ReviewFeedbackConfig{Enabled: true, MaxIterations: 3}

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "")

	prState, _ := c.GetPRState(42)
	// Set CreatedAt to before the reviews so time filter doesn't block them
	c.mu.Lock()
	c.activePRs[42].CreatedAt = time.Date(2026, 3, 5, 9, 0, 0, 0, time.UTC)
	c.mu.Unlock()

	result := c.hasChangesRequested(context.Background(), prState)
	if result {
		t.Error("hasChangesRequested should return false for bot-only reviews")
	}
}

func TestController_HandleReviewRequested_DisabledByConfig(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	cfg.ReviewFeedback = &ReviewFeedbackConfig{Enabled: false, MaxIterations: 3}

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "")

	// OnReviewRequested should not transition when disabled
	c.OnReviewRequested(42, "submitted", "changes_requested", "alice")

	pr, ok := c.GetPRState(42)
	if !ok {
		t.Fatal("PR should be tracked")
	}
	if pr.Stage == StageReviewRequested {
		t.Error("stage should NOT be review_requested when feature is disabled")
	}
}

func TestController_OnReviewRequested_UntrackedPR(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	cfg.ReviewFeedback = &ReviewFeedbackConfig{Enabled: true, MaxIterations: 3}

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	// Should not panic on untracked PR
	c.OnReviewRequested(99, "submitted", "changes_requested", "alice")

	_, ok := c.GetPRState(99)
	if ok {
		t.Error("untracked PR should not be added")
	}
}

func TestController_HasChangesRequested_FilterByTime(t *testing.T) {
	// Reviews submitted before PR tracking should be ignored
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/42/reviews":
			resp := []*github.PullRequestReview{
				{
					ID:          1,
					User:        github.User{Login: "alice"},
					Body:        "Old review",
					State:       "CHANGES_REQUESTED",
					SubmittedAt: "2026-03-01T10:00:00Z", // Before PR creation
				},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.ReviewFeedback = &ReviewFeedbackConfig{Enabled: true, MaxIterations: 3}

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "")

	// Set PR creation after the review
	c.mu.Lock()
	c.activePRs[42].CreatedAt = time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	c.mu.Unlock()

	prState, _ := c.GetPRState(42)
	result := c.hasChangesRequested(context.Background(), prState)
	if result {
		t.Error("hasChangesRequested should return false for reviews submitted before PR creation")
	}
}
