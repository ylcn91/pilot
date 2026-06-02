package autopilot

import (
	"fmt"
	"testing"
	"time"
)

func TestStateStore_ProcessedIssues(t *testing.T) {
	store := newTestStateStore(t)

	// Not processed initially
	processed, err := store.IsProcessed("github", "owner/repo", "100")
	if err != nil {
		t.Fatalf("IsProcessed failed: %v", err)
	}
	if processed {
		t.Error("issue should not be processed initially")
	}

	// Mark processed
	if err := store.Mark("github", "owner/repo", "100"); err != nil {
		t.Fatalf("Mark failed: %v", err)
	}

	processed, err = store.IsProcessed("github", "owner/repo", "100")
	if err != nil {
		t.Fatalf("IsProcessed failed: %v", err)
	}
	if !processed {
		t.Error("issue should be processed after marking")
	}

	// Load all
	all, err := store.Load("github", "owner/repo")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("got %d processed, want 1", len(all))
	}
	if _, ok := all["100"]; !ok {
		t.Error("issue 100 should be in processed map")
	}

	// Idempotent mark
	if err := store.Mark("github", "owner/repo", "100"); err != nil {
		t.Fatalf("idempotent Mark failed: %v", err)
	}
	all, _ = store.Load("github", "owner/repo")
	if len(all) != 1 {
		t.Errorf("got %d processed after idempotent mark, want 1", len(all))
	}

	// Unmark processed (for retry when pilot-failed label removed)
	if err := store.Unmark("github", "owner/repo", "100"); err != nil {
		t.Fatalf("Unmark failed: %v", err)
	}
	processed, err = store.IsProcessed("github", "owner/repo", "100")
	if err != nil {
		t.Fatalf("IsProcessed after unmark failed: %v", err)
	}
	if processed {
		t.Error("issue should not be processed after unmarking")
	}

	// Unmark non-existent issue should not error
	if err := store.Unmark("github", "owner/repo", "999"); err != nil {
		t.Fatalf("Unmark for non-existent issue failed: %v", err)
	}
}

func TestStateStore_Metadata(t *testing.T) {
	store := newTestStateStore(t)

	// Get non-existent key
	val, err := store.GetMetadata("missing")
	if err != nil {
		t.Fatalf("GetMetadata failed: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string for missing key, got %q", val)
	}

	// Set and get
	if err := store.SaveMetadata("consecutive_failures", "5"); err != nil {
		t.Fatalf("SaveMetadata failed: %v", err)
	}

	val, err = store.GetMetadata("consecutive_failures")
	if err != nil {
		t.Fatalf("GetMetadata failed: %v", err)
	}
	if val != "5" {
		t.Errorf("got %q, want %q", val, "5")
	}

	// Update
	if err := store.SaveMetadata("consecutive_failures", "0"); err != nil {
		t.Fatalf("SaveMetadata update failed: %v", err)
	}
	val, _ = store.GetMetadata("consecutive_failures")
	if val != "0" {
		t.Errorf("got %q after update, want %q", val, "0")
	}
}

func TestStateStore_Purge(t *testing.T) {
	store := newTestStateStore(t)

	for i := 1; i <= 5; i++ {
		if err := store.Mark("github", "owner/repo", fmt.Sprintf("%d", i)); err != nil {
			t.Fatalf("Mark(%d) failed: %v", i, err)
		}
	}

	purged, err := store.Purge("github", 0)
	if err != nil {
		t.Fatalf("Purge failed: %v", err)
	}
	if purged != 5 {
		t.Errorf("purged = %d, want 5", purged)
	}

	all, _ := store.Load("github", "owner/repo")
	if len(all) != 0 {
		t.Errorf("got %d after purge, want 0", len(all))
	}
}

func TestStateStore_PurgeTerminalPRStates(t *testing.T) {
	store := newTestStateStore(t)

	// Save a failed PR and an active PR
	failedPR := &PRState{
		PRNumber:   1,
		PRURL:      "https://github.com/owner/repo/pull/1",
		BranchName: "pilot/GH-1",
		Stage:      StageFailed,
		CIStatus:   CIFailure,
		CreatedAt:  time.Now(),
	}
	activePR := &PRState{
		PRNumber:   2,
		PRURL:      "https://github.com/owner/repo/pull/2",
		BranchName: "pilot/GH-2",
		Stage:      StageWaitingCI,
		CIStatus:   CIRunning,
		CreatedAt:  time.Now(),
	}

	if err := store.SavePRState(failedPR); err != nil {
		t.Fatalf("SavePRState(failed) failed: %v", err)
	}
	if err := store.SavePRState(activePR); err != nil {
		t.Fatalf("SavePRState(active) failed: %v", err)
	}

	// Purge terminal states older than 0 (immediate)
	purged, err := store.PurgeTerminalPRStates(0)
	if err != nil {
		t.Fatalf("PurgeTerminalPRStates failed: %v", err)
	}
	if purged != 1 {
		t.Errorf("purged = %d, want 1 (only failed)", purged)
	}

	// Active PR should still exist
	states, _ := store.LoadAllPRStates()
	if len(states) != 1 {
		t.Fatalf("got %d states, want 1", len(states))
	}
	if states[0].PRNumber != 2 {
		t.Errorf("remaining PR = %d, want 2", states[0].PRNumber)
	}
}

// B4 (TASK-309): PurgeTerminalPRStates also reaps 'releasing' rows stuck past
// releasingStaleThreshold, while leaving fresh releases-in-flight alone.
func TestStateStore_PurgeTerminalPRStates_Releasing(t *testing.T) {
	store := newTestStateStore(t)

	freshReleasing := &PRState{
		PRNumber:   1,
		BranchName: "pilot/GH-1",
		Stage:      StageReleasing,
		CreatedAt:  time.Now(),
	}
	staleReleasing := &PRState{
		PRNumber:   2,
		BranchName: "pilot/GH-2",
		Stage:      StageReleasing,
		CreatedAt:  time.Now(),
	}
	for _, pr := range []*PRState{freshReleasing, staleReleasing} {
		if err := store.SavePRState(pr); err != nil {
			t.Fatalf("SavePRState(%d): %v", pr.PRNumber, err)
		}
	}
	// Backdate PR 2 well past the 30-min staleness threshold.
	if _, err := store.db.Exec(
		`UPDATE autopilot_pr_state SET updated_at = datetime('now', '-2 hours') WHERE pr_number = ?`, 2,
	); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	// A large olderThan keeps any 'failed' row out of scope, so the only eligible
	// row is the stale 'releasing' one (reaped on its own 30-min threshold).
	purged, err := store.PurgeTerminalPRStates(24 * time.Hour)
	if err != nil {
		t.Fatalf("PurgeTerminalPRStates: %v", err)
	}
	if purged != 1 {
		t.Errorf("purged = %d, want 1 (only the stale releasing row)", purged)
	}

	states, _ := store.LoadAllPRStates()
	if len(states) != 1 || states[0].PRNumber != 1 {
		t.Errorf("remaining states = %+v, want only the fresh releasing PR 1", states)
	}
}

// B3 (TASK-309): PersistedReleasingAge reports whether a PR already has a release
// in flight in the state store, and how stale that row is.
func TestStateStore_PersistedReleasingAge(t *testing.T) {
	store := newTestStateStore(t)

	// (a) no row → not found.
	if _, found, err := store.PersistedReleasingAge(99); err != nil || found {
		t.Errorf("missing row: got found=%v err=%v, want found=false err=nil", found, err)
	}

	// (b) non-releasing stage → not found.
	if err := store.SavePRState(&PRState{PRNumber: 10, BranchName: "pilot/GH-10", Stage: StageWaitingCI, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("SavePRState(waiting): %v", err)
	}
	if _, found, err := store.PersistedReleasingAge(10); err != nil || found {
		t.Errorf("non-releasing row: got found=%v err=%v, want found=false", found, err)
	}

	// (c) fresh releasing row → found, age below threshold.
	if err := store.SavePRState(&PRState{PRNumber: 20, BranchName: "pilot/GH-20", Stage: StageReleasing, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("SavePRState(releasing): %v", err)
	}
	age, found, err := store.PersistedReleasingAge(20)
	if err != nil || !found {
		t.Fatalf("fresh releasing: got found=%v err=%v, want found=true", found, err)
	}
	if age >= releasingStaleThreshold {
		t.Errorf("fresh releasing age = %v, want < %v", age, releasingStaleThreshold)
	}

	// (d) stale releasing row → found, age above threshold.
	if _, err := store.db.Exec(
		`UPDATE autopilot_pr_state SET updated_at = datetime('now', '-2 hours') WHERE pr_number = ?`, 20,
	); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	age, found, err = store.PersistedReleasingAge(20)
	if err != nil || !found {
		t.Fatalf("stale releasing: got found=%v err=%v, want found=true", found, err)
	}
	if age < releasingStaleThreshold {
		t.Errorf("stale releasing age = %v, want >= %v", age, releasingStaleThreshold)
	}
}
