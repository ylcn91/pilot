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

// TestController_ScanRecentlyMergedPRs_RecordsMetrics verifies that
// ScanRecentlyMergedPRs fires merge metrics on first discovery (GH-2981)
// and is idempotent on subsequent scans.
func TestController_ScanRecentlyMergedPRs_RecordsMetrics(t *testing.T) {
	recentMergedAt := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)

	pilotPR := github.PullRequest{
		Number:         77,
		Head:           github.PRRef{Ref: "pilot/GH-300", SHA: "sha77"},
		Base:           github.PRRef{Ref: "main"},
		HTMLURL:        "https://github.com/owner/repo/pull/77",
		Title:          "feat(api): new endpoint",
		Merged:         true,
		MergedAt:       recentMergedAt,
		MergeCommitSHA: "merge-sha-77",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/pulls"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*github.PullRequest{&pilotPR})
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/releases"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("[]"))
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

	// First scan: PR not in state store → metrics must be recorded.
	if err := c.ScanRecentlyMergedPRs(context.Background()); err != nil {
		t.Fatalf("first ScanRecentlyMergedPRs() error = %v", err)
	}

	snap := c.metrics.Snapshot()
	if snap.PRsMerged != 1 {
		t.Errorf("after first scan: PRsMerged = %d, want 1", snap.PRsMerged)
	}
	if snap.IssuesProcessed["success"] != 1 {
		t.Errorf("after first scan: IssuesProcessed[success] = %d, want 1", snap.IssuesProcessed["success"])
	}
	hist := c.metrics.HistogramSnapshot()
	if len(hist.PRTimeToMerge) != 1 {
		t.Errorf("after first scan: PRTimeToMerge samples = %d, want 1", len(hist.PRTimeToMerge))
	}

	// Second scan: PR is now in state store at StageReleasing → counts must not change.
	if err := c.ScanRecentlyMergedPRs(context.Background()); err != nil {
		t.Fatalf("second ScanRecentlyMergedPRs() error = %v", err)
	}

	snap2 := c.metrics.Snapshot()
	if snap2.PRsMerged != 1 {
		t.Errorf("after second scan (idempotency): PRsMerged = %d, want 1", snap2.PRsMerged)
	}
	if snap2.IssuesProcessed["success"] != 1 {
		t.Errorf("after second scan (idempotency): IssuesProcessed[success] = %d, want 1", snap2.IssuesProcessed["success"])
	}
	hist2 := c.metrics.HistogramSnapshot()
	if len(hist2.PRTimeToMerge) != 1 {
		t.Errorf("after second scan (idempotency): PRTimeToMerge samples = %d, want 1", len(hist2.PRTimeToMerge))
	}
}

// TestController_ScanRecentlyMergedPRs_BoardWriteBack verifies TASK-356 #2: an
// externally-merged Pilot PR (manual `gh pr merge`, never through handleMerging)
// still has its board card moved to Done by the scanner. Large PRs blocked by the
// stage approval-misconfig are merged manually, so without this the card stays
// stuck "In Review".
func TestController_ScanRecentlyMergedPRs_BoardWriteBack(t *testing.T) {
	recentMergedAt := time.Now().Add(-3 * time.Minute).UTC().Format(time.RFC3339)

	pilotPR := github.PullRequest{
		Number:         123,
		Head:           github.PRRef{Ref: "pilot/GH-456", SHA: "headsha123"},
		Base:           github.PRRef{Ref: "main"},
		HTMLURL:        "https://github.com/owner/repo/pull/123",
		Title:          "feat: big change merged manually",
		Merged:         true,
		MergedAt:       recentMergedAt,
		MergeCommitSHA: "merge-sha-123",
	}

	const issueNodeID = "I_kwDOissue456"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/pulls"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*github.PullRequest{&pilotPR})
		case r.URL.Path == "/repos/owner/repo/issues/456":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"node_id": issueNodeID})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Release = &ReleaseConfig{Enabled: true, Trigger: "on_merge", TagPrefix: "v"}
	cfg.MergedPRScanWindow = 30 * time.Minute

	mock := &mockBoardSyncer{}
	c := NewController(cfg, ghClient, nil, "owner", "repo",
		withBoardSyncerForTest(mock, "Done", "Failed", "In Review", "In Dev"))
	c.SetStateStore(newTestStateStore(t))

	if err := c.ScanRecentlyMergedPRs(context.Background()); err != nil {
		t.Fatalf("ScanRecentlyMergedPRs() error = %v", err)
	}

	if len(mock.calls) != 1 {
		t.Fatalf("board sync calls = %d, want 1 (externally-merged PR card should move to Done)", len(mock.calls))
	}
	if mock.calls[0].issueNodeID != issueNodeID {
		t.Errorf("board sync issueNodeID = %q, want %q", mock.calls[0].issueNodeID, issueNodeID)
	}
	if mock.calls[0].statusName != "Done" {
		t.Errorf("board sync statusName = %q, want %q", mock.calls[0].statusName, "Done")
	}
}

// TestController_ScanRecentlyMergedPRs_BoardWriteBack_NoRelease verifies TASK-356 #2
// (decouple): board write-back fires even when on_merge release is DISABLED, and the
// release-triggering tail is skipped (PR not added to activePRs). A board-sourced,
// non-releasing setup must still move a manually-merged PR's card to Done.
func TestController_ScanRecentlyMergedPRs_BoardWriteBack_NoRelease(t *testing.T) {
	recentMergedAt := time.Now().Add(-3 * time.Minute).UTC().Format(time.RFC3339)

	pilotPR := github.PullRequest{
		Number:         321,
		Head:           github.PRRef{Ref: "pilot/GH-654", SHA: "headsha321"},
		Base:           github.PRRef{Ref: "main"},
		HTMLURL:        "https://github.com/owner/repo/pull/321",
		Title:          "feat: merged manually, release off",
		Merged:         true,
		MergedAt:       recentMergedAt,
		MergeCommitSHA: "merge-sha-321",
	}

	const issueNodeID = "I_kwDOissue654"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/pulls"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*github.PullRequest{&pilotPR})
		case r.URL.Path == "/repos/owner/repo/issues/654":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"node_id": issueNodeID})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Release = &ReleaseConfig{Enabled: false} // release OFF
	cfg.MergedPRScanWindow = 30 * time.Minute

	mock := &mockBoardSyncer{}
	c := NewController(cfg, ghClient, nil, "owner", "repo",
		withBoardSyncerForTest(mock, "Done", "Failed", "In Review", "In Dev"))
	c.SetStateStore(newTestStateStore(t))

	if err := c.ScanRecentlyMergedPRs(context.Background()); err != nil {
		t.Fatalf("ScanRecentlyMergedPRs() error = %v", err)
	}

	if len(mock.calls) != 1 {
		t.Fatalf("board sync calls = %d, want 1 (card should move to Done even with release off)", len(mock.calls))
	}
	if mock.calls[0].issueNodeID != issueNodeID || mock.calls[0].statusName != "Done" {
		t.Errorf("board sync call = %+v, want {%q, Done}", mock.calls[0], issueNodeID)
	}

	// Release tail must be skipped: PR not registered for release triggering.
	c.mu.RLock()
	_, tracked := c.activePRs[321]
	c.mu.RUnlock()
	if tracked {
		t.Error("PR 321 was added to activePRs; release tail must be skipped when release is disabled")
	}
}

// TestController_ScanRecentlyMergedPRs_RecordsMetricsDespiteExistingRelease
// reproduces the bug where Pilot's own self-release pipeline always tags every
// merge within ~1min, so by the time the ~5-15min scanner tick runs the
// "already tagged" gate skips the PR before metrics fire. The recorder must
// fire BEFORE the gate so counters move in stage-mode auto-merge.
func TestController_ScanRecentlyMergedPRs_RecordsMetricsDespiteExistingRelease(t *testing.T) {
	recentMergedAt := time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339)
	recentCreatedAt := time.Now().Add(-12 * time.Minute).UTC().Format(time.RFC3339)
	mergeSHA := "merge-sha-99"

	pilotPR := github.PullRequest{
		Number:         99,
		Head:           github.PRRef{Ref: "pilot/GH-501", SHA: "headsha99"},
		Base:           github.PRRef{Ref: "main"},
		HTMLURL:        "https://github.com/owner/repo/pull/99",
		Title:          "feat(api): another endpoint",
		Merged:         true,
		CreatedAt:      recentCreatedAt,
		MergedAt:       recentMergedAt,
		MergeCommitSHA: mergeSHA,
	}

	// Tag already exists at the merge SHA (Pilot's self-shipping pattern).
	existingTag := github.Tag{Name: "v9.9.9", Commit: struct {
		SHA string `json:"sha"`
	}{SHA: mergeSHA}}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/pulls"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*github.PullRequest{&pilotPR})
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/tags"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*github.Tag{&existingTag})
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
		t.Fatalf("first ScanRecentlyMergedPRs() error = %v", err)
	}

	snap := c.metrics.Snapshot()
	if snap.PRsMerged != 1 {
		t.Errorf("PRsMerged = %d, want 1 (recorder must fire even when release tag exists)", snap.PRsMerged)
	}
	if snap.IssuesProcessed["success"] != 1 {
		t.Errorf("IssuesProcessed[success] = %d, want 1", snap.IssuesProcessed["success"])
	}
	hist := c.metrics.HistogramSnapshot()
	if len(hist.PRTimeToMerge) != 1 {
		t.Errorf("PRTimeToMerge samples = %d, want 1", len(hist.PRTimeToMerge))
	}

	// Verify release-exists gate still suppresses release triggering: PR should
	// NOT have been added to activePRs (would happen if scanner proceeded past
	// the gate).
	c.mu.RLock()
	_, tracked := c.activePRs[99]
	c.mu.RUnlock()
	if tracked {
		t.Error("PR 99 was added to activePRs; release-exists gate should still suppress release triggering")
	}

	// Second scan: counts unchanged (idempotency).
	if err := c.ScanRecentlyMergedPRs(context.Background()); err != nil {
		t.Fatalf("second ScanRecentlyMergedPRs() error = %v", err)
	}
	snap2 := c.metrics.Snapshot()
	if snap2.PRsMerged != 1 {
		t.Errorf("after second scan: PRsMerged = %d, want 1 (idempotent)", snap2.PRsMerged)
	}
}
