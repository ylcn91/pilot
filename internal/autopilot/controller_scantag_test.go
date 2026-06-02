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

// TestController_ScanRecentlyMergedPRs_SkipsTaggedMergeCommit reproduces GH-3218:
// releases have target_commitish="main" (branch ref, not SHA), so the former
// releasedCommits map lookup was never matching. The scanner must skip PRs whose
// merge commit has a release tag regardless of target_commitish.
func TestController_ScanRecentlyMergedPRs_SkipsTaggedMergeCommit(t *testing.T) {
	recentMergedAt := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)
	mergeSHA := "abc123def456"

	pilotPR := github.PullRequest{
		Number:         55,
		Head:           github.PRRef{Ref: "pilot/GH-200", SHA: "headsha55"},
		Base:           github.PRRef{Ref: "main"},
		HTMLURL:        "https://github.com/owner/repo/pull/55",
		Title:          "feat(api): endpoint",
		Merged:         true,
		MergedAt:       recentMergedAt,
		MergeCommitSHA: mergeSHA,
	}

	// Tag exists at the merge SHA. target_commitish is "main" (branch ref, not SHA),
	// which was the broken key in the old releasedCommits map.
	tagAtMergeSHA := github.Tag{Name: "v1.2.3", Commit: struct {
		SHA string `json:"sha"`
	}{SHA: mergeSHA}}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/pulls"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*github.PullRequest{&pilotPR})
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/tags"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*github.Tag{&tagAtMergeSHA})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Release = &ReleaseConfig{
		Enabled:   true,
		Trigger:   "on_merge",
		TagPrefix: "v",
	}
	cfg.MergedPRScanWindow = 30 * time.Minute

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	store := newTestStateStore(t)
	c.SetStateStore(store)

	if err := c.ScanRecentlyMergedPRs(context.Background()); err != nil {
		t.Fatalf("ScanRecentlyMergedPRs() error = %v", err)
	}

	c.mu.RLock()
	_, tracked := c.activePRs[55]
	c.mu.RUnlock()
	if tracked {
		t.Error("PR 55 was added to activePRs; scanner must skip PRs whose merge commit is already tagged")
	}
}

// TestController_ScanRecentlyMergedPRs_TracksOrphanMerge verifies that a PR
// merged externally with NO release tag is still picked up by the scanner
// (orphan-merge recovery must keep working after the GH-3218 fix).
func TestController_ScanRecentlyMergedPRs_TracksOrphanMerge(t *testing.T) {
	recentMergedAt := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)
	mergeSHA := "deadbeef1234"

	pilotPR := github.PullRequest{
		Number:         66,
		Head:           github.PRRef{Ref: "pilot/GH-300", SHA: "headsha66"},
		Base:           github.PRRef{Ref: "main"},
		HTMLURL:        "https://github.com/owner/repo/pull/66",
		Title:          "fix(db): leak",
		Merged:         true,
		MergedAt:       recentMergedAt,
		MergeCommitSHA: mergeSHA,
	}

	// Tags endpoint returns a tag for a DIFFERENT SHA — merge commit has no tag yet.
	unrelatedTag := github.Tag{Name: "v0.0.1", Commit: struct {
		SHA string `json:"sha"`
	}{SHA: "ffffffffffffffffffffffffffffffffffffffff"}}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/pulls"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*github.PullRequest{&pilotPR})
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/tags"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*github.Tag{&unrelatedTag})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Release = &ReleaseConfig{
		Enabled:   true,
		Trigger:   "on_merge",
		TagPrefix: "v",
	}
	cfg.MergedPRScanWindow = 30 * time.Minute

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	store := newTestStateStore(t)
	c.SetStateStore(store)

	if err := c.ScanRecentlyMergedPRs(context.Background()); err != nil {
		t.Fatalf("ScanRecentlyMergedPRs() error = %v", err)
	}

	c.mu.RLock()
	pr, tracked := c.activePRs[66]
	c.mu.RUnlock()
	if !tracked {
		t.Fatal("PR 66 was not added to activePRs; orphan-merge recovery must still pick up untagged PRs")
	}
	if pr.Stage != StageReleasing {
		t.Errorf("PR 66 stage = %v, want StageReleasing", pr.Stage)
	}
}

// TestController_RecordMergeSuccess_Idempotency verifies recordMergeSuccess
// fires exactly once per PR number even when called multiple times from
// different code paths (e.g. handleMerging + ScanRecentlyMergedPRs both
// observing the same PR).
func TestController_RecordMergeSuccess_Idempotency(t *testing.T) {
	c := NewController(DefaultConfig(), nil, nil, "owner", "repo")

	createdAt := time.Now().Add(-10 * time.Minute)
	prState := &PRState{PRNumber: 42, CreatedAt: createdAt}

	c.recordMergeSuccess(prState)
	c.recordMergeSuccess(prState)
	c.recordMergeSuccess(prState)

	snap := c.metrics.Snapshot()
	if snap.PRsMerged != 1 {
		t.Errorf("PRsMerged = %d after 3 calls, want 1", snap.PRsMerged)
	}
	if snap.IssuesProcessed["success"] != 1 {
		t.Errorf("IssuesProcessed[success] = %d after 3 calls, want 1", snap.IssuesProcessed["success"])
	}
	hist := c.metrics.HistogramSnapshot()
	if len(hist.PRTimeToMerge) != 1 {
		t.Errorf("PRTimeToMerge samples = %d after 3 calls, want 1", len(hist.PRTimeToMerge))
	}

	// Different PR number → fires independently.
	c.recordMergeSuccess(&PRState{PRNumber: 43, CreatedAt: createdAt})
	snap2 := c.metrics.Snapshot()
	if snap2.PRsMerged != 2 {
		t.Errorf("PRsMerged = %d after second PR, want 2", snap2.PRsMerged)
	}
}
