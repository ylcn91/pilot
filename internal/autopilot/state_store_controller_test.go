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

func TestController_RestoreState(t *testing.T) {
	store := newTestStateStore(t)

	// Pre-populate store with PR states
	pr1 := &PRState{
		PRNumber:    42,
		PRURL:       "https://github.com/owner/repo/pull/42",
		IssueNumber: 10,
		BranchName:  "pilot/GH-10",
		HeadSHA:     "abc123",
		Stage:       StageWaitingCI,
		CIStatus:    CIRunning,
		CreatedAt:   time.Now(),
	}
	pr2 := &PRState{
		PRNumber:    43,
		PRURL:       "https://github.com/owner/repo/pull/43",
		IssueNumber: 11,
		BranchName:  "pilot/GH-11",
		HeadSHA:     "def456",
		Stage:       StageCIPassed,
		CIStatus:    CISuccess,
		CreatedAt:   time.Now(),
	}
	// Failed PR should NOT be restored as active
	pr3 := &PRState{
		PRNumber:    44,
		PRURL:       "https://github.com/owner/repo/pull/44",
		IssueNumber: 12,
		BranchName:  "pilot/GH-12",
		Stage:       StageFailed,
		CIStatus:    CIFailure,
		CreatedAt:   time.Now(),
	}

	for _, pr := range []*PRState{pr1, pr2, pr3} {
		if err := store.SavePRState(pr); err != nil {
			t.Fatalf("SavePRState(%d) failed: %v", pr.PRNumber, err)
		}
	}

	// Save circuit breaker state
	if err := store.SaveMetadata("consecutive_failures", "2"); err != nil {
		t.Fatalf("SaveMetadata failed: %v", err)
	}

	// Create controller and restore
	cfg := DefaultConfig()
	c := NewController(cfg, nil, nil, "owner", "repo")
	c.SetStateStore(store)

	restored, err := c.RestoreState()
	if err != nil {
		t.Fatalf("RestoreState failed: %v", err)
	}

	// Should restore 3 total (from LoadAllPRStates), but only 2 active (failed filtered)
	if restored != 3 {
		t.Errorf("restored = %d, want 3 (total from store)", restored)
	}

	prs := c.GetActivePRs()
	if len(prs) != 2 {
		t.Fatalf("active PRs = %d, want 2 (failed should be excluded)", len(prs))
	}

	// Verify stages preserved
	pr42, ok := c.GetPRState(42)
	if !ok {
		t.Fatal("PR 42 not found in active PRs")
	}
	if pr42.Stage != StageWaitingCI {
		t.Errorf("PR 42 stage = %s, want %s", pr42.Stage, StageWaitingCI)
	}

	pr43, ok := c.GetPRState(43)
	if !ok {
		t.Fatal("PR 43 not found in active PRs")
	}
	if pr43.Stage != StageCIPassed {
		t.Errorf("PR 43 stage = %s, want %s", pr43.Stage, StageCIPassed)
	}

	// Failed PR should not be in active map
	_, ok = c.GetPRState(44)
	if ok {
		t.Error("PR 44 (failed) should not be in active PRs")
	}
}

func TestController_OnPRCreated_PersistsToStore(t *testing.T) {
	store := newTestStateStore(t)

	cfg := DefaultConfig()
	c := NewController(cfg, nil, nil, "owner", "repo")
	c.SetStateStore(store)

	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "")

	// Verify persisted to store
	loaded, err := store.GetPRState(42)
	if err != nil {
		t.Fatalf("GetPRState failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("PR state not persisted to store")
	}
	if loaded.Stage != StagePRCreated {
		t.Errorf("persisted stage = %s, want %s", loaded.Stage, StagePRCreated)
	}
}

func TestController_RemovePR_RemovesFromStore(t *testing.T) {
	store := newTestStateStore(t)

	cfg := DefaultConfig()
	c := NewController(cfg, nil, nil, "owner", "repo")
	c.SetStateStore(store)

	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "")

	// Remove
	c.removePR(42)

	// Verify removed from store
	loaded, err := store.GetPRState(42)
	if err != nil {
		t.Fatalf("GetPRState failed: %v", err)
	}
	if loaded != nil {
		t.Error("PR state should be removed from store")
	}
}

func TestController_ProcessPR_PersistsTransition(t *testing.T) {
	// Set up mock GitHub server for CI check
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return empty JSON for any request
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	store := newTestStateStore(t)
	cfg := DefaultConfig()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.SetStateStore(store)

	// Register a PR
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "")

	// Process — should transition from StagePRCreated to StageWaitingCI
	if err := c.ProcessPR(context.Background(), 42, nil); err != nil {
		t.Fatalf("ProcessPR failed: %v", err)
	}

	// Verify state persisted with new stage
	loaded, err := store.GetPRState(42)
	if err != nil {
		t.Fatalf("GetPRState failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("PR state not found in store after ProcessPR")
	}
	if loaded.Stage != StageWaitingCI {
		t.Errorf("persisted stage = %s, want %s", loaded.Stage, StageWaitingCI)
	}
}
