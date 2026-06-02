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

// GH-724: Test that merge conflicts are detected during WaitingCI and PR is closed immediately.
func TestController_ProcessPR_MergeConflict_WaitingCI(t *testing.T) {
	prCommented := false
	prClosed := false
	labelRemoved := false

	mergeable := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/pulls/42" && r.Method == "GET":
			// Return PR with merge conflict
			resp := github.PullRequest{
				Number:         42,
				Head:           github.PRRef{SHA: "abc1234"},
				Mergeable:      &mergeable,
				MergeableState: "dirty",
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/repos/owner/repo/pulls/42/update-branch" && r.Method == "PUT":
			// GH-1796: auto-rebase fails (true conflict)
			w.WriteHeader(http.StatusUnprocessableEntity)
		case r.URL.Path == "/repos/owner/repo/issues/42/comments" && r.Method == "POST":
			prCommented = true
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(github.PRComment{ID: 1})
		case r.URL.Path == "/repos/owner/repo/pulls/42" && r.Method == "PATCH":
			prClosed = true
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/repos/owner/repo/issues/10/labels/pilot-in-progress" && r.Method == "DELETE":
			labelRemoved = true
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.DevCITimeout = 1 * time.Second
	cfg.RequiredChecks = []string{"build", "test", "lint"}

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	// Inject PR state directly at StageWaitingCI to test the handleWaitingCI path
	// (bypassing handlePRCreated which also checks conflicts now)
	c.mu.Lock()
	c.activePRs[42] = &PRState{
		PRNumber:        42,
		PRURL:           "https://github.com/owner/repo/pull/42",
		IssueNumber:     10,
		BranchName:      "pilot/GH-10",
		HeadSHA:         "abc1234",
		Stage:           StageWaitingCI,
		CIStatus:        CIPending,
		CIWaitStartedAt: time.Now(),
		CreatedAt:       time.Now(),
	}
	c.mu.Unlock()

	ctx := context.Background()

	// Process PR in WaitingCI stage → should detect conflict and fail immediately
	err := c.ProcessPR(ctx, 42, nil)
	if err != nil {
		t.Fatalf("ProcessPR error: %v", err)
	}

	pr, _ := c.GetPRState(42)
	if pr.Stage != StageFailed {
		t.Errorf("Stage = %s, want %s (conflict should immediately fail)", pr.Stage, StageFailed)
	}
	if pr.Error != "merge conflict with base branch" {
		t.Errorf("Error = %q, want %q", pr.Error, "merge conflict with base branch")
	}
	if !prCommented {
		t.Error("PR should have been commented with conflict explanation")
	}
	if !prClosed {
		t.Error("conflicting PR should have been closed")
	}
	if !labelRemoved {
		t.Error("pilot-in-progress label should have been removed from issue")
	}
}

// GH-724: Test that merge conflicts are detected immediately on PR creation.
func TestController_ProcessPR_MergeConflict_PRCreated(t *testing.T) {
	prClosed := false
	labelRemoved := false

	mergeable := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/pulls/42" && r.Method == "GET":
			resp := github.PullRequest{
				Number:         42,
				Head:           github.PRRef{SHA: "abc1234"},
				Mergeable:      &mergeable,
				MergeableState: "dirty",
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/repos/owner/repo/pulls/42/update-branch" && r.Method == "PUT":
			// GH-1796: auto-rebase fails (true conflict)
			w.WriteHeader(http.StatusUnprocessableEntity)
		case r.URL.Path == "/repos/owner/repo/pulls/42" && r.Method == "PATCH":
			prClosed = true
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/repos/owner/repo/issues/10/labels/pilot-in-progress" && r.Method == "DELETE":
			labelRemoved = true
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.CIPollInterval = 10 * time.Millisecond

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc1234", "pilot/GH-10", "")

	ctx := context.Background()

	// Stage 1: PR created → should detect conflict immediately and skip CI wait
	err := c.ProcessPR(ctx, 42, nil)
	if err != nil {
		t.Fatalf("ProcessPR error: %v", err)
	}

	pr, _ := c.GetPRState(42)
	if pr.Stage != StageFailed {
		t.Errorf("Stage = %s, want %s (conflict on creation should fail immediately)", pr.Stage, StageFailed)
	}
	if !prClosed {
		t.Error("conflicting PR should have been closed")
	}
	if !labelRemoved {
		t.Error("pilot-in-progress label should have been removed from issue")
	}
}

// GH-724: Test that unknown mergeable state (GitHub still computing) proceeds to CI check normally.
func TestController_ProcessPR_MergeableUnknown_ProceedsToCICheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/pulls/42" && r.Method == "GET":
			// Mergeable is nil (GitHub hasn't computed yet)
			resp := github.PullRequest{
				Number:         42,
				Head:           github.PRRef{SHA: "abc1234"},
				MergeableState: "unknown",
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/repos/owner/repo/commits/abc1234/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 3,
				CheckRuns: []github.CheckRun{
					{Name: "build", Status: "completed", Conclusion: "success"},
					{Name: "test", Status: "completed", Conclusion: "success"},
					{Name: "lint", Status: "completed", Conclusion: "success"},
				},
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
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.DevCITimeout = 1 * time.Second
	cfg.RequiredChecks = []string{"build", "test", "lint"}

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc1234", "pilot/GH-10", "")

	ctx := context.Background()

	// Stage 1: PR created → waiting CI (unknown mergeable should NOT block)
	err := c.ProcessPR(ctx, 42, nil)
	if err != nil {
		t.Fatalf("ProcessPR stage 1 error: %v", err)
	}
	pr, _ := c.GetPRState(42)
	if pr.Stage != StageWaitingCI {
		t.Errorf("Stage = %s, want %s (unknown mergeable should proceed to CI)", pr.Stage, StageWaitingCI)
	}

	// Stage 2: waiting CI → CI passed (should check CI normally despite unknown mergeable)
	err = c.ProcessPR(ctx, 42, nil)
	if err != nil {
		t.Fatalf("ProcessPR stage 2 error: %v", err)
	}
	pr, _ = c.GetPRState(42)
	if pr.Stage != StageCIPassed {
		t.Errorf("Stage = %s, want %s", pr.Stage, StageCIPassed)
	}
}

// GH-724: Test that mergeable=false (without dirty state) also triggers conflict detection.
func TestController_ProcessPR_MergeableFalse_DetectsConflict(t *testing.T) {
	prClosed := false

	mergeable := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/pulls/42" && r.Method == "GET":
			// mergeable=false but mergeable_state not set (older API responses)
			resp := github.PullRequest{
				Number:    42,
				Head:      github.PRRef{SHA: "abc1234"},
				Mergeable: &mergeable,
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/repos/owner/repo/pulls/42/update-branch" && r.Method == "PUT":
			// GH-1796: auto-rebase fails (true conflict)
			w.WriteHeader(http.StatusUnprocessableEntity)
		case r.URL.Path == "/repos/owner/repo/pulls/42" && r.Method == "PATCH":
			prClosed = true
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.DevCITimeout = 1 * time.Second

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc1234", "pilot/GH-10", "")

	ctx := context.Background()

	// Stage 1: PR created → waiting CI
	err := c.ProcessPR(ctx, 42, nil)
	if err != nil {
		t.Fatalf("ProcessPR stage 1 error: %v", err)
	}

	// Stage 2: waiting CI → conflict detected via mergeable=false
	err = c.ProcessPR(ctx, 42, nil)
	if err != nil {
		t.Fatalf("ProcessPR stage 2 error: %v", err)
	}

	pr, _ := c.GetPRState(42)
	if pr.Stage != StageFailed {
		t.Errorf("Stage = %s, want %s", pr.Stage, StageFailed)
	}
	if !prClosed {
		t.Error("conflicting PR should have been closed")
	}
}
