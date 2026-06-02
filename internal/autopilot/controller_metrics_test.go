package autopilot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestController_IssuesProcessed_Success verifies that RecordIssueProcessed("success")
// is called when a PR merges successfully.
func TestController_IssuesProcessed_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/commits/mergesha1/check-runs":
			_, _ = w.Write(mustJSON(t, github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns:  []github.CheckRun{{Name: "build", Status: "completed", Conclusion: "success"}},
			}))
		case r.URL.Path == "/repos/owner/repo/pulls/42/merge" && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, map[string]interface{}{
				"sha": "merged1", "merged": true, "message": "Pull Request successfully merged",
			}))
		case r.URL.Path == "/repos/owner/repo/pulls/42" && r.Method == http.MethodGet:
			_, _ = w.Write(mustJSON(t, github.PullRequest{
				Number: 42, State: "open",
				Head: github.PRRef{Ref: "pilot/GH-10", SHA: "mergesha1"},
			}))
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
	cfg.RequiredChecks = []string{"build"}
	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "mergesha1", "pilot/GH-10", "")
	pr, _ := c.GetPRState(42)
	pr.Stage = StageMerging

	if err := c.ProcessPR(context.Background(), 42, nil); err != nil {
		t.Fatalf("ProcessPR returned error: %v", err)
	}

	snap := c.metrics.Snapshot()
	if snap.IssuesProcessed["success"] != 1 {
		t.Errorf("IssuesProcessed[success] = %d, want 1", snap.IssuesProcessed["success"])
	}
	if snap.IssuesProcessed["failed"] != 0 {
		t.Errorf("IssuesProcessed[failed] = %d, want 0", snap.IssuesProcessed["failed"])
	}
}

// TestController_IssuesProcessed_TerminalFailure verifies that RecordIssueProcessed("failed")
// is called when a PR hits the CI fix iteration limit (terminal failure path).
func TestController_IssuesProcessed_TerminalFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/commits/failsha1/check-runs":
			_, _ = w.Write(mustJSON(t, github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns:  []github.CheckRun{{Name: "build", Status: "completed", Conclusion: "failure"}},
			}))
		case r.URL.Path == "/repos/owner/repo/issues/20" && r.Method == http.MethodGet:
			// iteration:3 >= MaxCIFixIterations(3) → terminal stop
			_, _ = w.Write(mustJSON(t, github.Issue{
				Number: 20,
				Body:   "Fix CI failures.\n\n<!-- autopilot-meta branch:pilot/GH-20 pr:55 iteration:3 -->\n",
			}))
		case r.URL.Path == "/repos/owner/repo/pulls/55" && r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.MaxCIFixIterations = 3
	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.OnPRCreated(55, "https://github.com/owner/repo/pull/55", 20, "failsha1", "pilot/GH-20", "")
	pr, _ := c.GetPRState(55)
	pr.Stage = StageCIFailed

	if err := c.ProcessPR(context.Background(), 55, nil); err != nil {
		t.Fatalf("ProcessPR returned error: %v", err)
	}

	snap := c.metrics.Snapshot()
	if snap.IssuesProcessed["failed"] != 1 {
		t.Errorf("IssuesProcessed[failed] = %d, want 1", snap.IssuesProcessed["failed"])
	}
	if snap.IssuesProcessed["success"] != 0 {
		t.Errorf("IssuesProcessed[success] = %d, want 0", snap.IssuesProcessed["success"])
	}
}
