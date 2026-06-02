package autopilot

import (
	"context"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestController_handleMerging_IdempotentCompletionComment tests GH-2345:
// Re-entering StageMerging for an already-merged PR must not produce a second
// "PR merged" comment.
func TestController_handleMerging_IdempotentCompletionComment(t *testing.T) {
	commentCount := 0
	server := mergeMockServer(t, 42, 10, &commentCount)
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.AutoReview = false
	cfg.RequiredChecks = []string{"build"}

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc1234", "pilot/GH-10", "")
	prState, _ := c.GetPRState(42)
	prState.Stage = StageMerging

	ctx := context.Background()

	// First entry: posts the completion comment and advances stage.
	if err := c.handleMerging(ctx, prState); err != nil {
		t.Fatalf("first handleMerging returned error: %v", err)
	}
	if commentCount != 1 {
		t.Fatalf("after first handleMerging: comment count = %d, want 1", commentCount)
	}
	if !prState.MergeNotificationPosted {
		t.Fatal("MergeNotificationPosted should be true after first successful post")
	}

	// Simulate re-entry (e.g. via duplicate-dispatch or crash recovery).
	prState.Stage = StageMerging
	if err := c.handleMerging(ctx, prState); err != nil {
		t.Fatalf("re-entry handleMerging returned error: %v", err)
	}
	if commentCount != 1 {
		t.Errorf("after re-entry: comment count = %d, want 1 (no duplicate)", commentCount)
	}
}

// TestController_handleMerging_CommentFlagPersists tests GH-2345:
// MergeNotificationPosted round-trips through SavePRState/LoadAllPRStates so
// that crash recovery honors the flag and a restored PR never re-posts.
func TestController_handleMerging_CommentFlagPersists(t *testing.T) {
	store := newTestStateStore(t)

	pr := &PRState{
		PRNumber:                42,
		PRURL:                   "https://github.com/owner/repo/pull/42",
		IssueNumber:             10,
		BranchName:              "pilot/GH-10",
		HeadSHA:                 "abc1234",
		Stage:                   StageMerging,
		CIStatus:                CIPending,
		CreatedAt:               time.Now().Add(-5 * time.Minute).Truncate(time.Second),
		MergeNotificationPosted: true,
	}
	if err := store.SavePRState(pr); err != nil {
		t.Fatalf("SavePRState failed: %v", err)
	}

	loaded, err := store.GetPRState(42)
	if err != nil {
		t.Fatalf("GetPRState failed: %v", err)
	}
	if loaded == nil || !loaded.MergeNotificationPosted {
		t.Fatalf("MergeNotificationPosted did not persist: got %+v", loaded)
	}

	all, err := store.LoadAllPRStates()
	if err != nil {
		t.Fatalf("LoadAllPRStates failed: %v", err)
	}
	if len(all) != 1 || !all[0].MergeNotificationPosted {
		t.Fatalf("LoadAllPRStates did not preserve MergeNotificationPosted: %+v", all)
	}

	// Wire the restored state into a controller and run handleMerging — it
	// must not post a duplicate comment because the flag is already set.
	commentCount := 0
	server := mergeMockServer(t, 42, 10, &commentCount)
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.AutoReview = false
	cfg.RequiredChecks = []string{"build"}

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.SetStateStore(store)
	if _, err := c.RestoreState(); err != nil {
		t.Fatalf("RestoreState failed: %v", err)
	}

	restored, ok := c.GetPRState(42)
	if !ok {
		t.Fatal("restored PR not tracked")
	}
	restored.Stage = StageMerging

	if err := c.handleMerging(context.Background(), restored); err != nil {
		t.Fatalf("handleMerging after restore returned error: %v", err)
	}
	if commentCount != 0 {
		t.Errorf("after restore: comment count = %d, want 0 (flag honored)", commentCount)
	}
}
