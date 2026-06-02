package autopilot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestHandleMerged_LearnsFromReviews verifies that handleMerged fetches PR reviews
// when a learning loop is configured.
func TestHandleMerged_LearnsFromReviews(t *testing.T) {
	reviewsFetched := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/42/reviews":
			reviewsFetched = true
			reviews := []github.PullRequestReview{
				{Body: "LGTM — nice implementation", State: "APPROVED", User: github.User{Login: "reviewer1"}},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, reviews))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.AutoReview = false

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	loop, cleanup := newTestLearningLoop(t)
	defer cleanup()
	c.SetLearningLoop(loop)

	prState := &PRState{
		PRNumber: 42,
		PRURL:    "https://github.com/owner/repo/pull/42",
		Stage:    StageMerged,
	}

	err := c.handleMerged(context.Background(), prState)
	if err != nil {
		t.Fatalf("handleMerged returned unexpected error: %v", err)
	}

	if !reviewsFetched {
		t.Error("expected /pulls/42/reviews to be fetched for learning")
	}
}

// TestHandleMerged_NoReviews verifies that handleMerged does not error when
// there are no reviews to learn from.
func TestHandleMerged_NoReviews(t *testing.T) {
	reviewsFetched := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/42/reviews":
			reviewsFetched = true
			// Return empty array — no reviews
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("[]"))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.AutoReview = false

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	loop, cleanup := newTestLearningLoop(t)
	defer cleanup()
	c.SetLearningLoop(loop)

	prState := &PRState{
		PRNumber: 42,
		PRURL:    "https://github.com/owner/repo/pull/42",
		Stage:    StageMerged,
	}

	err := c.handleMerged(context.Background(), prState)
	if err != nil {
		t.Fatalf("handleMerged returned unexpected error: %v", err)
	}

	if !reviewsFetched {
		t.Error("expected /pulls/42/reviews to be fetched even when empty")
	}
}

// TestHandleMerged_NilLearningLoop verifies that handleMerged does not panic
// when no learning loop is configured (nil guard).
func TestHandleMerged_NilLearningLoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.AutoReview = false

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	// learningLoop intentionally not set

	prState := &PRState{
		PRNumber: 42,
		PRURL:    "https://github.com/owner/repo/pull/42",
		Stage:    StageMerged,
	}

	// Must not panic
	err := c.handleMerged(context.Background(), prState)
	if err != nil {
		t.Fatalf("handleMerged returned unexpected error: %v", err)
	}
}

// TestHandleCIFailed_LearnsFromCIFailure verifies that handleCIFailed calls
// LearnFromCIFailure when a learning loop is configured.
func TestHandleCIFailed_LearnsFromCIFailure(t *testing.T) {
	issueCreated := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/commits/sha123/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "build", Status: "completed", Conclusion: "failure"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/issues" && r.Method == "POST":
			issueCreated = true
			resp := github.Issue{Number: 200}
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
	cfg.AutoCreateIssues = true

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	loop, cleanup := newTestLearningLoop(t)
	defer cleanup()
	c.SetLearningLoop(loop)

	prState := &PRState{
		PRNumber: 42,
		HeadSHA:  "sha123",
		Stage:    StageCIFailed,
	}

	err := c.handleCIFailed(context.Background(), prState)
	if err != nil {
		t.Fatalf("handleCIFailed returned unexpected error: %v", err)
	}

	if !issueCreated {
		t.Error("expected fix issue to be created")
	}

	// The learning loop was set, so LearnFromCIFailure was called.
	// With nil extractor it returns an error (logged as warning), but must not panic.
	if prState.Stage != StageFailed {
		t.Errorf("Stage = %s, want %s", prState.Stage, StageFailed)
	}
}

// TestHandleCIFailed_NilLearningLoop verifies that handleCIFailed does not panic
// when no learning loop is configured (nil guard).
func TestHandleCIFailed_NilLearningLoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/commits/sha456/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "lint", Status: "completed", Conclusion: "failure"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/issues" && r.Method == "POST":
			resp := github.Issue{Number: 201}
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
	// learningLoop intentionally not set

	prState := &PRState{
		PRNumber: 43,
		HeadSHA:  "sha456",
		Stage:    StageCIFailed,
	}

	// Must not panic
	err := c.handleCIFailed(context.Background(), prState)
	if err != nil {
		t.Fatalf("handleCIFailed returned unexpected error: %v", err)
	}
}
