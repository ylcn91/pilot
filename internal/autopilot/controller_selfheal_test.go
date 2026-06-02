package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// GH-2402: After a successful merge, the controller must call
// SelfHealExecutionAfterMerge so any prior failed execution row for the
// same task ID is promoted to "completed" with the PR URL stamped.
func TestController_HandleMerging_SelfHealsExecution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/commits/abc1234/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case "/repos/owner/repo/pulls/42/merge":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"sha":"merged123","merged":true,"message":"merged"}`))
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

	// TASK-352: scope self-heal to the project's filesystem path (the value the
	// executor stores in executions.project_path), NOT owner/repo.
	c := NewController(cfg, ghClient, nil, "owner", "repo", WithProjectPath("/proj/pilot"))
	healer := &mockExecutionHealer{}
	c.SetExecutionHealer(healer)

	c.mu.Lock()
	c.activePRs[42] = &PRState{
		PRNumber:    42,
		PRURL:       "https://github.com/owner/repo/pull/42",
		HeadSHA:     "abc1234",
		IssueNumber: 99,
		Stage:       StageMerging,
	}
	c.mu.Unlock()

	if err := c.ProcessPR(context.Background(), 42, nil); err != nil {
		t.Fatalf("ProcessPR returned unexpected error: %v", err)
	}

	// IssueNumber 99 has no "Parent: GH-N" body (default {} response), so exactly
	// one self-heal call (the issue itself, no parent).
	if len(healer.selfHealed) != 1 {
		t.Fatalf("expected 1 self-heal call, got %d", len(healer.selfHealed))
	}
	got := healer.selfHealed[0]
	if got.TaskID != "GH-99" {
		t.Errorf("self-heal task ID = %q, want GH-99", got.TaskID)
	}
	if got.ProjectPath != "/proj/pilot" {
		t.Errorf("self-heal project path = %q, want /proj/pilot (fs path, not owner/repo)", got.ProjectPath)
	}
	if got.PRURL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("self-heal PR URL = %q, want PR URL", got.PRURL)
	}
}

// TASK-352: An externally-merged Pilot PR (gh pr merge / GitHub UI) never passes
// through handleMerging, so ScanRecentlyMergedPRs must self-heal its execution
// record (Bug 1) AND, when the PR is for a sub-issue, its parent epic's record
// (Bug 2) — both scoped to the controller's filesystem project path.
func TestController_ScanRecentlyMergedPRs_SelfHeals(t *testing.T) {
	mergedAt := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)
	pr := github.PullRequest{
		Number:         55,
		Head:           github.PRRef{Ref: "pilot/GH-3353", SHA: "sha55"},
		Base:           github.PRRef{Ref: "main"},
		HTMLURL:        "https://github.com/owner/repo/pull/55",
		Title:          "fix(memory): integrity cluster",
		Merged:         true,
		MergedAt:       mergedAt,
		MergeCommitSHA: "merge-sha-55",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/pulls"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*github.PullRequest{&pr})
		case r.URL.Path == "/repos/owner/repo/issues/3353":
			// Sub-issue body carries the "Parent: GH-N" line epic.go writes.
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(github.Issue{
				Body: "<!--autopilot-meta\nparent: GH-3344\n-->\n\nParent: GH-3344\n\nwork",
			})
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("[]"))
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Release = &ReleaseConfig{Enabled: true, Trigger: "on_merge", TagPrefix: "v"}
	cfg.MergedPRScanWindow = 30 * time.Minute

	c := NewController(cfg, ghClient, nil, "owner", "repo", WithProjectPath("/proj/pilot"))
	healer := &mockExecutionHealer{}
	c.SetExecutionHealer(healer)

	if err := c.ScanRecentlyMergedPRs(context.Background()); err != nil {
		t.Fatalf("ScanRecentlyMergedPRs: %v", err)
	}

	healed := map[string]bool{}
	for _, h := range healer.selfHealed {
		healed[h.TaskID] = true
		if h.ProjectPath != "/proj/pilot" {
			t.Errorf("self-heal %s: ProjectPath = %q, want /proj/pilot", h.TaskID, h.ProjectPath)
		}
		if h.PRURL != pr.HTMLURL {
			t.Errorf("self-heal %s: PRURL = %q, want %q", h.TaskID, h.PRURL, pr.HTMLURL)
		}
	}
	if !healed["GH-3353"] {
		t.Errorf("Bug 1: expected self-heal for the merged sub-issue GH-3353; got %+v", healer.selfHealed)
	}
	if !healed["GH-3344"] {
		t.Errorf("Bug 2: expected self-heal for the parent epic GH-3344; got %+v", healer.selfHealed)
	}
}

// B3 (TASK-309): a PR persisted at stage='releasing' but absent from the in-memory
// activePRs map (e.g. after a daemon restart) must not be re-registered by the
// scanner while the release is fresh. A 'releasing' row stuck past
// releasingStaleThreshold is re-driven so a wedged release can recover.
func TestController_ScanRecentlyMergedPRs_SkipsPersistedReleasing(t *testing.T) {
	mergedAt := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)
	pr := github.PullRequest{
		Number:         77,
		Head:           github.PRRef{Ref: "pilot/GH-770", SHA: "sha77"},
		Base:           github.PRRef{Ref: "main"},
		HTMLURL:        "https://github.com/owner/repo/pull/77",
		Title:          "fix(api): retry",
		Merged:         true,
		MergedAt:       mergedAt,
		MergeCommitSHA: "merge-sha-77",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/pulls"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*github.PullRequest{&pr})
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("[]"))
		}
	}))
	defer server.Close()

	newScanController := func(t *testing.T) (*Controller, *StateStore) {
		t.Helper()
		ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
		cfg := DefaultConfig()
		cfg.Release = &ReleaseConfig{Enabled: true, Trigger: "on_merge", TagPrefix: "v"}
		cfg.MergedPRScanWindow = 30 * time.Minute
		c := NewController(cfg, ghClient, nil, "owner", "repo")
		store := newTestStateStore(t)
		c.SetStateStore(store)
		return c, store
	}

	isTracked := func(c *Controller, prNumber int) bool {
		for _, p := range c.GetActivePRs() {
			if p.PRNumber == prNumber {
				return true
			}
		}
		return false
	}

	t.Run("fresh persisted releasing row is skipped", func(t *testing.T) {
		c, store := newScanController(t)
		if err := store.SavePRState(&PRState{PRNumber: 77, BranchName: "pilot/GH-770", Stage: StageReleasing, CreatedAt: time.Now()}); err != nil {
			t.Fatalf("SavePRState: %v", err)
		}
		if err := c.ScanRecentlyMergedPRs(context.Background()); err != nil {
			t.Fatalf("ScanRecentlyMergedPRs: %v", err)
		}
		if isTracked(c, 77) {
			t.Error("PR 77 was re-registered despite a fresh persisted releasing row; B3 skip gate did not fire")
		}
	})

	t.Run("stale persisted releasing row is re-driven", func(t *testing.T) {
		c, store := newScanController(t)
		if err := store.SavePRState(&PRState{PRNumber: 77, BranchName: "pilot/GH-770", Stage: StageReleasing, CreatedAt: time.Now()}); err != nil {
			t.Fatalf("SavePRState: %v", err)
		}
		if _, err := store.db.Exec(
			`UPDATE autopilot_pr_state SET updated_at = datetime('now', '-2 hours') WHERE pr_number = ?`, 77,
		); err != nil {
			t.Fatalf("backdate: %v", err)
		}
		if err := c.ScanRecentlyMergedPRs(context.Background()); err != nil {
			t.Fatalf("ScanRecentlyMergedPRs: %v", err)
		}
		if !isTracked(c, 77) {
			t.Error("PR 77 was not re-driven despite a stale persisted releasing row; wedged release cannot recover")
		}
	})
}
