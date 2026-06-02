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

// GH-457: Test that handleWaitingCI refreshes stale HeadSHA from GitHub.
// When self-review pushes new commits, OnPRCreated stores the pre-self-review SHA.
// The controller must fetch the actual HEAD from GitHub before checking CI.
func TestController_ProcessPR_RefreshesStaleHeadSHA(t *testing.T) {
	staleSHA := "stale1234567890"
	actualSHA := "actual1234567890"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/42":
			// Return PR with actual HEAD SHA (different from stale SHA)
			resp := github.PullRequest{
				Number: 42,
				Head:   github.PRRef{SHA: actualSHA},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)

		case "/repos/owner/repo/commits/" + actualSHA + "/check-runs":
			// CI passes for actual SHA
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

		case "/repos/owner/repo/commits/" + staleSHA + "/check-runs":
			// No CI for stale SHA (this is the bug scenario)
			resp := github.CheckRunsResponse{TotalCount: 0, CheckRuns: []github.CheckRun{}}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)

		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvStage
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.CIWaitTimeout = 1 * time.Second
	cfg.RequiredChecks = []string{"build", "test", "lint"}

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	// Register PR with stale SHA (simulates self-review changing HEAD)
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, staleSHA, "pilot/GH-10", "")

	ctx := context.Background()

	// Stage 1: PR created → waiting CI
	err := c.ProcessPR(ctx, 42, nil)
	if err != nil {
		t.Fatalf("ProcessPR stage 1 error: %v", err)
	}
	pr, _ := c.GetPRState(42)
	if pr.Stage != StageWaitingCI {
		t.Errorf("after stage 1: Stage = %s, want %s", pr.Stage, StageWaitingCI)
	}

	// Stage 2: waiting CI → should refresh SHA and find CI passed
	err = c.ProcessPR(ctx, 42, nil)
	if err != nil {
		t.Fatalf("ProcessPR stage 2 error: %v", err)
	}
	pr, _ = c.GetPRState(42)

	// Verify SHA was refreshed
	if pr.HeadSHA != actualSHA {
		t.Errorf("HeadSHA = %s, want %s (should have been refreshed from GitHub)", pr.HeadSHA, actualSHA)
	}

	// Verify CI passed with actual SHA
	if pr.CIStatus != CISuccess {
		t.Errorf("CIStatus = %s, want %s", pr.CIStatus, CISuccess)
	}
	if pr.Stage != StageCIPassed {
		t.Errorf("Stage = %s, want %s", pr.Stage, StageCIPassed)
	}
}

// GH-457: Test that without the fix, stale SHA would cause CI to stay pending.
// This validates the bug scenario explicitly.
func TestController_ProcessPR_StaleSHAWithoutRefreshWouldStayPending(t *testing.T) {
	staleSHA := "stale1234567890"
	actualSHA := "actual1234567890"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/42":
			resp := github.PullRequest{
				Number: 42,
				Head:   github.PRRef{SHA: actualSHA},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)

		case "/repos/owner/repo/commits/" + actualSHA + "/check-runs":
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

		case "/repos/owner/repo/commits/" + staleSHA + "/check-runs":
			// Stale SHA has no check runs — this is what caused the bug
			resp := github.CheckRunsResponse{TotalCount: 0, CheckRuns: []github.CheckRun{}}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)

		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvStage
	cfg.RequiredChecks = []string{"build", "test", "lint"}

	// Verify what happens when we check CI against stale SHA directly
	ciMonitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	// Stale SHA returns no checks → CIPending
	staleStatus, err := ciMonitor.CheckCI(context.Background(), staleSHA)
	if err != nil {
		t.Fatalf("CheckCI for stale SHA failed: %v", err)
	}
	if staleStatus != CIPending {
		t.Errorf("stale SHA status = %s, want %s (no checks = pending)", staleStatus, CIPending)
	}

	// Actual SHA returns passing checks → CISuccess
	actualStatus, err := ciMonitor.CheckCI(context.Background(), actualSHA)
	if err != nil {
		t.Fatalf("CheckCI for actual SHA failed: %v", err)
	}
	if actualStatus != CISuccess {
		t.Errorf("actual SHA status = %s, want %s", actualStatus, CISuccess)
	}
}
