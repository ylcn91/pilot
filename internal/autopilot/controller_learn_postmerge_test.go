package autopilot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestHandlePostMergeCI_LearnsFromCIFailure verifies that handlePostMergeCI calls
// LearnFromCIFailure when post-merge CI fails and a learning loop is configured.
func TestHandlePostMergeCI_LearnsFromCIFailure(t *testing.T) {
	issueCreated := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/branches/main":
			resp := map[string]interface{}{
				"commit": map[string]string{"sha": "mainsha1"},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/commits/mainsha1/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "e2e", Status: "completed", Conclusion: "failure"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/issues" && r.Method == "POST":
			issueCreated = true
			resp := github.Issue{Number: 300}
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
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.CIWaitTimeout = 1 * time.Second
	cfg.RequiredChecks = []string{"e2e"}
	cfg.AutoCreateIssues = true

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	loop, cleanup := newTestLearningLoop(t)
	defer cleanup()
	c.SetLearningLoop(loop)

	prState := &PRState{
		PRNumber: 44,
		Stage:    StagePostMergeCI,
	}

	err := c.handlePostMergeCI(context.Background(), prState)
	if err != nil {
		t.Fatalf("handlePostMergeCI returned unexpected error: %v", err)
	}

	if !issueCreated {
		t.Error("expected post-merge fix issue to be created")
	}
}

// TestHandlePostMergeCI_NonBlocking verifies that handlePostMergeCI does not block
// the tick loop: pending CI returns nil and stays in StagePostMergeCI, success
// advances to StageReleasing (when release is configured) or removes the PR,
// and a daemon restart resumes from the persisted PostMergeSHA. (GH-2717)
func TestHandlePostMergeCI_NonBlocking(t *testing.T) {
	tests := []struct {
		name           string
		checkStatus    string
		checkConc      string
		releaseEnabled bool
		wantStage      PRStage
		wantRemoved    bool
	}{
		{
			name:        "pending stays in stage",
			checkStatus: "in_progress",
			wantStage:   StagePostMergeCI,
			wantRemoved: false,
		},
		{
			name:           "success without release removes PR",
			checkStatus:    "completed",
			checkConc:      "success",
			releaseEnabled: false,
			wantRemoved:    true,
		},
		{
			name:           "success with release advances to StageReleasing",
			checkStatus:    "completed",
			checkConc:      "success",
			releaseEnabled: true,
			wantStage:      StageReleasing,
			wantRemoved:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/owner/repo/branches/main":
					resp := map[string]interface{}{
						"commit": map[string]string{"sha": "mainsha42"},
					}
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(mustJSON(t, resp))
				case "/repos/owner/repo/commits/mainsha42/check-runs":
					resp := github.CheckRunsResponse{
						TotalCount: 1,
						CheckRuns: []github.CheckRun{
							{Name: "ci", Status: tt.checkStatus, Conclusion: tt.checkConc},
						},
					}
					w.WriteHeader(http.StatusOK)
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
			cfg.CIPollInterval = 10 * time.Millisecond
			cfg.CIWaitTimeout = 5 * time.Second
			if tt.releaseEnabled {
				cfg.Release = &ReleaseConfig{Enabled: true, Trigger: "on_merge"}
			}

			c := NewController(cfg, ghClient, nil, "owner", "repo")

			prState := &PRState{
				PRNumber: 77,
				Stage:    StagePostMergeCI,
			}

			err := c.handlePostMergeCI(context.Background(), prState)
			if err != nil {
				t.Fatalf("handlePostMergeCI returned error: %v", err)
			}

			if tt.wantRemoved {
				// PR should have been removed from tracking.
				c.mu.RLock()
				_, stillTracked := c.activePRs[77]
				c.mu.RUnlock()
				if stillTracked {
					t.Error("expected PR to be removed but it is still tracked")
				}
			} else {
				if prState.Stage != tt.wantStage {
					t.Errorf("stage = %s, want %s", prState.Stage, tt.wantStage)
				}
			}

			// PostMergeSHA must be populated after first call.
			if prState.PostMergeSHA == "" {
				t.Error("PostMergeSHA should be set after first tick")
			}
		})
	}
}

// TestHandlePostMergeCI_RestartResumesSHA verifies that if PostMergeSHA is already
// set (simulating a daemon restart with persisted state), handlePostMergeCI does not
// fetch a new SHA and continues monitoring the original commit. (GH-2717)
func TestHandlePostMergeCI_RestartResumesSHA(t *testing.T) {
	branchFetched := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/branches/main":
			branchFetched = true
			resp := map[string]interface{}{
				"commit": map[string]string{"sha": "newsha99"},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		case "/repos/owner/repo/commits/originalsha/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "ci", Status: "in_progress"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.CIWaitTimeout = 5 * time.Minute

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	// Simulate persisted state from before restart (started 30s ago, well within timeout).
	prState := &PRState{
		PRNumber:             88,
		Stage:                StagePostMergeCI,
		PostMergeSHA:         "originalsha",
		PostMergeCIStartedAt: time.Now().Add(-30 * time.Second),
	}

	err := c.handlePostMergeCI(context.Background(), prState)
	if err != nil {
		t.Fatalf("handlePostMergeCI returned error: %v", err)
	}

	if branchFetched {
		t.Error("branch SHA should NOT be fetched when PostMergeSHA is already set")
	}
	if prState.PostMergeSHA != "originalsha" {
		t.Errorf("PostMergeSHA changed to %q, want %q", prState.PostMergeSHA, "originalsha")
	}
	if prState.Stage != StagePostMergeCI {
		t.Errorf("stage = %s, want %s (CI still running)", prState.Stage, StagePostMergeCI)
	}
}

// TestHandlePostMergeCI_Timeout verifies that a long-running post-merge CI
// eventually times out and transitions to StageFailed. (GH-2717)
func TestHandlePostMergeCI_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/commits/tsha/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "ci", Status: "in_progress"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.CIWaitTimeout = 1 * time.Millisecond

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	// Started well in the past to trigger immediate timeout.
	prState := &PRState{
		PRNumber:             99,
		Stage:                StagePostMergeCI,
		PostMergeSHA:         "tsha",
		PostMergeCIStartedAt: time.Now().Add(-10 * time.Minute),
	}

	err := c.handlePostMergeCI(context.Background(), prState)
	if err != nil {
		t.Fatalf("handlePostMergeCI returned error: %v", err)
	}
	if prState.Stage != StageFailed {
		t.Errorf("stage = %s, want %s after timeout", prState.Stage, StageFailed)
	}
	if prState.Error == "" {
		t.Error("expected Error to be set on timeout")
	}
}
