package autopilot

import (
	"testing"
	"time"
)

func TestStateStore_MigrateIdempotent(t *testing.T) {
	store := newTestStateStore(t)

	// Running migrate again should not fail
	if err := store.migrate(); err != nil {
		t.Fatalf("second migration failed: %v", err)
	}
}

// GH-834: Test per-PR failure persistence.
func TestStateStore_PRFailures(t *testing.T) {
	store := newTestStateStore(t)

	// Save failure state
	failureTime := time.Now().Truncate(time.Second)
	if err := store.SavePRFailures(42, 3, failureTime); err != nil {
		t.Fatalf("SavePRFailures failed: %v", err)
	}

	// Load and verify
	failures, err := store.LoadAllPRFailures()
	if err != nil {
		t.Fatalf("LoadAllPRFailures failed: %v", err)
	}
	if len(failures) != 1 {
		t.Fatalf("got %d failures, want 1", len(failures))
	}
	if failures[42] == nil {
		t.Fatal("PR 42 not in failures map")
	}
	if failures[42].FailureCount != 3 {
		t.Errorf("FailureCount = %d, want 3", failures[42].FailureCount)
	}

	// Update failure state
	if err := store.SavePRFailures(42, 5, time.Now()); err != nil {
		t.Fatalf("SavePRFailures update failed: %v", err)
	}
	failures, _ = store.LoadAllPRFailures()
	if failures[42].FailureCount != 5 {
		t.Errorf("FailureCount after update = %d, want 5", failures[42].FailureCount)
	}

	// Remove failure state
	if err := store.RemovePRFailures(42); err != nil {
		t.Fatalf("RemovePRFailures failed: %v", err)
	}
	failures, _ = store.LoadAllPRFailures()
	if len(failures) != 0 {
		t.Errorf("got %d failures after remove, want 0", len(failures))
	}
}

// GH-834: Test that RestoreState loads per-PR failures.
func TestController_RestoreState_LoadsPRFailures(t *testing.T) {
	store := newTestStateStore(t)

	// Pre-populate with PR state and failure state
	pr := &PRState{
		PRNumber:   42,
		PRURL:      "https://github.com/owner/repo/pull/42",
		BranchName: "pilot/GH-10",
		Stage:      StageWaitingCI,
		CIStatus:   CIRunning,
		CreatedAt:  time.Now(),
	}
	if err := store.SavePRState(pr); err != nil {
		t.Fatalf("SavePRState failed: %v", err)
	}
	if err := store.SavePRFailures(42, 2, time.Now()); err != nil {
		t.Fatalf("SavePRFailures failed: %v", err)
	}

	// Create controller and restore
	cfg := DefaultConfig()
	cfg.MaxFailures = 3
	c := NewController(cfg, nil, nil, "owner", "repo")
	c.SetStateStore(store)

	if _, err := c.RestoreState(); err != nil {
		t.Fatalf("RestoreState failed: %v", err)
	}

	// Verify failure count restored
	if c.GetPRFailures(42) != 2 {
		t.Errorf("GetPRFailures(42) = %d, want 2", c.GetPRFailures(42))
	}
}

// GH-834: Test that removePR also removes failure state.
func TestController_RemovePR_RemovesFailures(t *testing.T) {
	store := newTestStateStore(t)

	cfg := DefaultConfig()
	c := NewController(cfg, nil, nil, "owner", "repo")
	c.SetStateStore(store)

	// Create PR and add failure state
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "")
	c.mu.Lock()
	c.prFailures[42] = &prFailureState{FailureCount: 2, LastFailureTime: time.Now()}
	c.mu.Unlock()
	c.persistPRFailures(42, c.prFailures[42])

	// Verify failure state persisted
	failures, _ := store.LoadAllPRFailures()
	if len(failures) != 1 {
		t.Fatalf("expected 1 failure record, got %d", len(failures))
	}

	// Remove PR
	c.removePR(42)

	// Verify failure state also removed
	failures, _ = store.LoadAllPRFailures()
	if len(failures) != 0 {
		t.Errorf("expected 0 failure records after removePR, got %d", len(failures))
	}
}

// --- GH-1838: Generic adapter_processed tests ---

func TestStateStore_GenericAdapterProcessed(t *testing.T) {
	store := newTestStateStore(t)

	if err := store.Mark("jira", "", "PROJ-1"); err != nil {
		t.Fatalf("Mark failed: %v", err)
	}
	if err := store.Mark("jira", "", "PROJ-2"); err != nil {
		t.Fatalf("Mark failed: %v", err)
	}
	if err := store.Mark("linear", "", "LIN-ABC"); err != nil {
		t.Fatalf("Mark failed: %v", err)
	}

	ok, err := store.IsProcessed("jira", "", "PROJ-1")
	if err != nil {
		t.Fatalf("IsProcessed failed: %v", err)
	}
	if !ok {
		t.Error("PROJ-1 should be processed for jira")
	}

	ok, err = store.IsProcessed("jira", "", "PROJ-999")
	if err != nil {
		t.Fatalf("IsProcessed failed: %v", err)
	}
	if ok {
		t.Error("PROJ-999 should not be processed for jira")
	}

	// Same issue ID, different adapter — should not conflict
	ok, err = store.IsProcessed("linear", "", "PROJ-1")
	if err != nil {
		t.Fatalf("IsProcessed failed: %v", err)
	}
	if ok {
		t.Error("PROJ-1 should not be processed for linear adapter")
	}

	jiraProcessed, err := store.Load("jira", "")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(jiraProcessed) != 2 {
		t.Errorf("jira processed count = %d, want 2", len(jiraProcessed))
	}
	if _, ok := jiraProcessed["PROJ-1"]; !ok {
		t.Error("jira processed map missing PROJ-1")
	}
	if _, ok := jiraProcessed["PROJ-2"]; !ok {
		t.Error("jira processed map missing PROJ-2")
	}

	linearProcessed, err := store.Load("linear", "")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(linearProcessed) != 1 {
		t.Errorf("linear processed count = %d, want 1", len(linearProcessed))
	}

	if err := store.Unmark("jira", "", "PROJ-1"); err != nil {
		t.Fatalf("Unmark failed: %v", err)
	}
	ok, _ = store.IsProcessed("jira", "", "PROJ-1")
	if ok {
		t.Error("PROJ-1 should be unmarked after Unmark")
	}
}

func TestStateStore_GenericAdapterProcessed_Upsert(t *testing.T) {
	store := newTestStateStore(t)

	if err := store.Mark("github", "owner/repo", "42"); err != nil {
		t.Fatalf("Mark failed: %v", err)
	}
	// Re-mark (upsert) — should update processed_at, not add a duplicate
	if err := store.Mark("github", "owner/repo", "42"); err != nil {
		t.Fatalf("Mark (upsert) failed: %v", err)
	}

	all, err := store.Load("github", "owner/repo")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected 1 entry after upsert, got %d", len(all))
	}
}

func TestStateStore_MigrateLegacyProcessedTables(t *testing.T) {
	// Migration test: seed adapter_processed directly (legacy tables are dropped on migrate).
	store := newTestStateStore(t)

	// Verify migration is idempotent (already ran in NewStateStoreFromPath).
	if err := store.migrateLegacyProcessedTables(); err != nil {
		t.Fatalf("second migrateLegacyProcessedTables: %v", err)
	}

	// After migration on a fresh DB, adapter_processed should be empty.
	for _, src := range []string{"github", "linear", "gitlab", "jira", "asana", "azuredevops", "plane"} {
		rows, err := store.Load(src, "")
		if err != nil {
			t.Errorf("Load(%s): %v", src, err)
		}
		if len(rows) != 0 {
			t.Errorf("expected 0 rows for %s after fresh migration, got %d", src, len(rows))
		}
	}
}
