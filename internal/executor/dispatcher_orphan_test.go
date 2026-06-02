package executor

import (
	"context"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

func TestRecoverStaleTasks_DeletesOrphanWhenCompleted(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Scenario: same TaskID has a completed row AND an orphan running/queued row.
	executions := []*memory.Execution{
		{ID: "exec-completed", TaskID: "TASK-ORPHAN", ProjectPath: "/project", Status: "completed", CommitSHA: "abc"},
		{ID: "exec-orphan-run", TaskID: "TASK-ORPHAN", ProjectPath: "/project", Status: "running"},
		{ID: "exec-orphan-q", TaskID: "TASK-ORPHAN", ProjectPath: "/project", Status: "queued"},
	}
	for _, exec := range executions {
		if err := store.SaveExecution(exec); err != nil {
			t.Fatalf("failed to save execution: %v", err)
		}
	}

	config := &DispatcherConfig{
		StaleRunningThreshold: 0,
		StaleQueuedThreshold:  0,
		StaleRecoveryInterval: time.Hour,
	}
	runner := NewRunner()
	dispatcher := NewDispatcher(store, runner, config)

	if err := dispatcher.Start(context.Background()); err != nil {
		t.Fatalf("failed to start dispatcher: %v", err)
	}
	defer dispatcher.Stop()

	// Orphan rows should be deleted, not marked failed.
	for _, id := range []string{"exec-orphan-run", "exec-orphan-q"} {
		exec, err := store.GetExecution(id)
		if err == nil && exec != nil {
			t.Errorf("expected orphan %s to be deleted, but it still exists with status '%s'", id, exec.Status)
		}
	}

	// Completed row should remain untouched.
	exec, err := store.GetExecution("exec-completed")
	if err != nil {
		t.Fatalf("failed to get completed execution: %v", err)
	}
	if exec.Status != "completed" {
		t.Errorf("expected completed execution to remain 'completed', got '%s'", exec.Status)
	}
}

func TestRecoverStaleTasks_MarksFailedWhenNoCompleted(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Scenario: orphan rows with no completed execution for the same TaskID.
	executions := []*memory.Execution{
		{ID: "exec-only-run", TaskID: "TASK-NOCOMPLETE", ProjectPath: "/project", Status: "running"},
		{ID: "exec-only-q", TaskID: "TASK-NOCOMPLETE-Q", ProjectPath: "/project", Status: "queued"},
	}
	for _, exec := range executions {
		if err := store.SaveExecution(exec); err != nil {
			t.Fatalf("failed to save execution: %v", err)
		}
	}

	config := &DispatcherConfig{
		StaleRunningThreshold: 0,
		StaleQueuedThreshold:  0,
		StaleRecoveryInterval: time.Hour,
	}
	runner := NewRunner()
	dispatcher := NewDispatcher(store, runner, config)

	if err := dispatcher.Start(context.Background()); err != nil {
		t.Fatalf("failed to start dispatcher: %v", err)
	}
	defer dispatcher.Stop()

	// Both should be marked failed (no completed execution exists).
	for _, id := range []string{"exec-only-run", "exec-only-q"} {
		exec, err := store.GetExecution(id)
		if err != nil {
			t.Fatalf("failed to get execution %s: %v", id, err)
		}
		if exec.Status != "failed" {
			t.Errorf("expected %s to be 'failed', got '%s'", id, exec.Status)
		}
	}
}

func TestRecoverStaleTasks_DifferentProjectPath(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Scenario: completed execution exists for a DIFFERENT project path.
	// The orphan should still be marked failed (HasCompletedExecution checks both fields).
	executions := []*memory.Execution{
		{ID: "exec-diff-completed", TaskID: "TASK-DIFF", ProjectPath: "/project-a", Status: "completed"},
		{ID: "exec-diff-orphan", TaskID: "TASK-DIFF", ProjectPath: "/project-b", Status: "running"},
	}
	for _, exec := range executions {
		if err := store.SaveExecution(exec); err != nil {
			t.Fatalf("failed to save execution: %v", err)
		}
	}

	config := &DispatcherConfig{
		StaleRunningThreshold: 0,
		StaleQueuedThreshold:  0,
		StaleRecoveryInterval: time.Hour,
	}
	runner := NewRunner()
	dispatcher := NewDispatcher(store, runner, config)

	if err := dispatcher.Start(context.Background()); err != nil {
		t.Fatalf("failed to start dispatcher: %v", err)
	}
	defer dispatcher.Stop()

	// Different project path → no match → should be marked failed, not deleted.
	exec, err := store.GetExecution("exec-diff-orphan")
	if err != nil {
		t.Fatalf("failed to get execution: %v", err)
	}
	if exec.Status != "failed" {
		t.Errorf("expected orphan with different project to be 'failed', got '%s'", exec.Status)
	}
}

func TestQueueTask_AfterRecovery(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Insert a stale task for the same task ID we'll try to queue.
	exec := &memory.Execution{
		ID:          "exec-old",
		TaskID:      "TASK-REQUEUE",
		ProjectPath: "/project",
		Status:      "running",
	}
	if err := store.SaveExecution(exec); err != nil {
		t.Fatalf("failed to save execution: %v", err)
	}

	// Start dispatcher with 0 threshold so it recovers immediately.
	config := &DispatcherConfig{
		StaleRunningThreshold: 0,
		StaleQueuedThreshold:  0,
		StaleRecoveryInterval: time.Hour,
	}
	runner := NewRunner()
	dispatcher := NewDispatcher(store, runner, config)

	if err := dispatcher.Start(context.Background()); err != nil {
		t.Fatalf("failed to start dispatcher: %v", err)
	}
	defer dispatcher.Stop()

	// The old task should now be failed, so re-queuing the same task ID should succeed.
	task := &Task{
		ID:          "TASK-REQUEUE",
		Title:       "Re-queued after recovery",
		Description: "Should succeed since old execution is failed",
		ProjectPath: "/project",
	}

	execID, err := dispatcher.QueueTask(context.Background(), task)
	if err != nil {
		t.Fatalf("expected re-queue to succeed after recovery, got error: %v", err)
	}
	if execID == "" {
		t.Error("expected non-empty execution ID")
	}
}
