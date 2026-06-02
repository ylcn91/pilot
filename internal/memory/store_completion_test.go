package memory

import (
	"fmt"
	"os"
	"testing"
)

func TestHasCompletedExecution(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// No executions yet — should return false
	completed, err := store.HasCompletedExecution("GH-42", "/project")
	if err != nil {
		t.Fatalf("HasCompletedExecution failed: %v", err)
	}
	if completed {
		t.Error("expected false for non-existent task")
	}

	// Save a non-completed execution
	_ = store.SaveExecution(&Execution{
		ID:          "exec-pending",
		TaskID:      "GH-42",
		ProjectPath: "/project",
		Status:      "running",
	})
	completed, err = store.HasCompletedExecution("GH-42", "/project")
	if err != nil {
		t.Fatalf("HasCompletedExecution failed: %v", err)
	}
	if completed {
		t.Error("expected false for running task")
	}

	// Save a completed execution with a deliverable (commit_sha set).
	_ = store.SaveExecution(&Execution{
		ID:          "exec-done",
		TaskID:      "GH-42",
		ProjectPath: "/project",
		Status:      "completed",
		CommitSHA:   "abc123",
	})
	completed, err = store.HasCompletedExecution("GH-42", "/project")
	if err != nil {
		t.Fatalf("HasCompletedExecution failed: %v", err)
	}
	if !completed {
		t.Error("expected true for completed task with deliverable")
	}

	// Completed but no deliverables (epic-parent false-positive pattern, TASK-296).
	_ = store.SaveExecution(&Execution{
		ID:          "exec-epic",
		TaskID:      "GH-43",
		ProjectPath: "/project",
		Status:      "completed",
	})
	completed, err = store.HasCompletedExecution("GH-43", "/project")
	if err != nil {
		t.Fatalf("HasCompletedExecution failed: %v", err)
	}
	if completed {
		t.Error("expected false for completed task with no deliverable (epic-parent false-positive)")
	}

	// Different project path — should return false
	completed, _ = store.HasCompletedExecution("GH-42", "/other-project")
	if completed {
		t.Error("expected false for different project path")
	}
}

// TestHasCompletedExecution_OrphanRecovery verifies that a completed execution
// with a non-empty error field (e.g., from orphan recovery) does NOT count as
// completed. This prevents orphan-recovered executions from blocking re-dispatch.
// GH-2315: Defense-in-depth against orphan recovery blocking re-dispatch.
func TestHasCompletedExecution_OrphanRecovery(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	taskID := "GH-2305"
	projectPath := "/project"

	// Simulate 5 failed executions (original scenario from GH-2314)
	for i := 0; i < 5; i++ {
		execID := fmt.Sprintf("exec-failed-%d", i)
		_ = store.SaveExecution(&Execution{
			ID:          execID,
			TaskID:      taskID,
			ProjectPath: projectPath,
			Status:      "failed",
		})
	}

	// Simulate orphan recovery: marks stale running task as "completed" with error
	_ = store.SaveExecution(&Execution{
		ID:          "exec-orphan",
		TaskID:      taskID,
		ProjectPath: projectPath,
		Status:      "running",
	})
	// Orphan recovery calls UpdateExecutionStatus with error message
	_ = store.UpdateExecutionStatus("exec-orphan", "completed", "stale running task recovered (orphaned worker)")

	// The orphan-recovered "completed" execution should NOT count as completed
	completed, err := store.HasCompletedExecution(taskID, projectPath)
	if err != nil {
		t.Fatalf("HasCompletedExecution failed: %v", err)
	}
	if completed {
		t.Error("expected false — orphan-recovered execution with error should not block re-dispatch")
	}

	// Now add a genuine completed execution (no error, has deliverable).
	_ = store.SaveExecution(&Execution{
		ID:          "exec-genuine",
		TaskID:      taskID,
		ProjectPath: projectPath,
		Status:      "completed",
		CommitSHA:   "deadbeef",
	})
	completed, err = store.HasCompletedExecution(taskID, projectPath)
	if err != nil {
		t.Fatalf("HasCompletedExecution failed: %v", err)
	}
	if !completed {
		t.Error("expected true — genuine completed execution with deliverable should be found")
	}
}

func TestUpdateExecutionStatusByTaskID_UpdatesFailedToCompleted(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	_ = store.SaveExecution(&Execution{
		ID:          "exec-fail-1",
		TaskID:      "GH-100",
		ProjectPath: "/tmp/proj",
		Status:      "failed",
		Error:       "quality gate failed",
	})

	if err := store.UpdateExecutionStatusByTaskID("GH-100", "/tmp/proj", "completed"); err != nil {
		t.Fatalf("UpdateExecutionStatusByTaskID failed: %v", err)
	}

	exec, err := store.GetExecution("exec-fail-1")
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}
	if exec.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", exec.Status)
	}
	if exec.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}
}

func TestUpdateExecutionStatusByTaskID_SkipsNonFailed(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	_ = store.SaveExecution(&Execution{
		ID:          "exec-ok-1",
		TaskID:      "GH-200",
		ProjectPath: "/tmp/proj",
		Status:      "completed",
	})

	if err := store.UpdateExecutionStatusByTaskID("GH-200", "/tmp/proj", "completed"); err != nil {
		t.Fatalf("UpdateExecutionStatusByTaskID failed: %v", err)
	}

	exec, _ := store.GetExecution("exec-ok-1")
	// Status should remain "completed" — the WHERE clause only targets "failed"
	if exec.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", exec.Status)
	}
}

func TestUpdateExecutionStatusByTaskID_NoMatchingTask(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Should not error even with no matching rows
	if err := store.UpdateExecutionStatusByTaskID("GH-999", "/tmp/proj", "completed"); err != nil {
		t.Fatalf("expected no error for non-existent task, got: %v", err)
	}
}

// TestUpdateExecutionStatusByTaskID_ScopedToProject verifies that updating by task ID
// only affects rows matching the given project path (D3 regression).
func TestUpdateExecutionStatusByTaskID_ScopedToProject(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Same task ID, different projects
	_ = store.SaveExecution(&Execution{ID: "exec-a", TaskID: "GH-300", ProjectPath: "/proj/a", Status: "failed"})
	_ = store.SaveExecution(&Execution{ID: "exec-b", TaskID: "GH-300", ProjectPath: "/proj/b", Status: "failed"})

	// Only heal project a
	if err := store.UpdateExecutionStatusByTaskID("GH-300", "/proj/a", "completed"); err != nil {
		t.Fatalf("UpdateExecutionStatusByTaskID: %v", err)
	}

	execA, _ := store.GetExecution("exec-a")
	if execA.Status != "completed" {
		t.Errorf("exec-a: expected 'completed', got %q", execA.Status)
	}
	execB, _ := store.GetExecution("exec-b")
	if execB.Status != "failed" {
		t.Errorf("exec-b: expected 'failed' (unaffected), got %q", execB.Status)
	}
}

// TestSelfHealExecutionAfterMerge_ScopedToProject verifies that self-heal only
// promotes rows matching the given project path (D3 regression).
func TestSelfHealExecutionAfterMerge_ScopedToProject(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	_ = store.SaveExecution(&Execution{ID: "heal-a", TaskID: "GH-400", ProjectPath: "/proj/a", Status: "failed"})
	_ = store.SaveExecution(&Execution{ID: "heal-b", TaskID: "GH-400", ProjectPath: "/proj/b", Status: "failed"})

	if err := store.SelfHealExecutionAfterMerge("GH-400", "/proj/a", "https://github.com/org/repo/pull/1"); err != nil {
		t.Fatalf("SelfHealExecutionAfterMerge: %v", err)
	}

	execA, _ := store.GetExecution("heal-a")
	if execA.Status != "completed" {
		t.Errorf("heal-a: expected 'completed', got %q", execA.Status)
	}
	if execA.PRUrl != "https://github.com/org/repo/pull/1" {
		t.Errorf("heal-a: expected pr_url to be stamped, got %q", execA.PRUrl)
	}
	execB, _ := store.GetExecution("heal-b")
	if execB.Status != "failed" {
		t.Errorf("heal-b: expected 'failed' (unaffected), got %q", execB.Status)
	}
}

// TestSelfHealExecutionAfterMerge_EmptyProjectPath verifies that an empty
// projectPath falls back to task_id-only matching (legacy single-repo behavior),
// so a caller that cannot supply the discriminator still heals every matching row
// rather than silently matching nothing. TASK-352.
func TestSelfHealExecutionAfterMerge_EmptyProjectPath(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	_ = store.SaveExecution(&Execution{ID: "e1", TaskID: "GH-500", ProjectPath: "/proj/a", Status: "failed"})
	_ = store.SaveExecution(&Execution{ID: "e2", TaskID: "GH-500", ProjectPath: "/proj/b", Status: "failed"})

	if err := store.SelfHealExecutionAfterMerge("GH-500", "", "https://github.com/org/repo/pull/9"); err != nil {
		t.Fatalf("SelfHealExecutionAfterMerge: %v", err)
	}

	for _, id := range []string{"e1", "e2"} {
		ex, _ := store.GetExecution(id)
		if ex.Status != "completed" {
			t.Errorf("%s: expected 'completed' with empty projectPath fallback, got %q", id, ex.Status)
		}
	}
}

func TestInvalidateCompletion(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	taskID := "GH-500"
	projectPath := "/project"

	// Insert a genuine completed execution (no error, with deliverable).
	_ = store.SaveExecution(&Execution{
		ID:          "exec-genuine",
		TaskID:      taskID,
		ProjectPath: projectPath,
		Status:      "completed",
		CommitSHA:   "sha-genuine",
	})

	// Insert an orphan-recovered execution (status=completed, error set).
	_ = store.SaveExecution(&Execution{
		ID:          "exec-orphan",
		TaskID:      taskID,
		ProjectPath: projectPath,
		Status:      "running",
	})
	_ = store.UpdateExecutionStatus("exec-orphan", "completed", "stale running task recovered (orphaned worker)")

	// Confirm HasCompletedExecution sees the genuine one.
	completed, err := store.HasCompletedExecution(taskID, projectPath)
	if err != nil {
		t.Fatalf("HasCompletedExecution: %v", err)
	}
	if !completed {
		t.Fatal("expected true before invalidation")
	}

	// Invalidate: should remove the genuine row only.
	if err := store.InvalidateCompletion(taskID, projectPath); err != nil {
		t.Fatalf("InvalidateCompletion: %v", err)
	}

	// HasCompletedExecution should now return false.
	completed, err = store.HasCompletedExecution(taskID, projectPath)
	if err != nil {
		t.Fatalf("HasCompletedExecution after invalidation: %v", err)
	}
	if completed {
		t.Error("expected false after invalidation")
	}

	// Calling again on already-empty set should be a no-op (no error).
	if err := store.InvalidateCompletion(taskID, projectPath); err != nil {
		t.Errorf("InvalidateCompletion on empty set: %v", err)
	}

	// Different project path should be unaffected — add a new genuine row and check.
	otherPath := "/other-project"
	_ = store.SaveExecution(&Execution{
		ID:          "exec-other",
		TaskID:      taskID,
		ProjectPath: otherPath,
		Status:      "completed",
		CommitSHA:   "sha-other",
	})
	if err := store.InvalidateCompletion(taskID, projectPath); err != nil {
		t.Fatalf("InvalidateCompletion: %v", err)
	}
	completed, _ = store.HasCompletedExecution(taskID, otherPath)
	if !completed {
		t.Error("InvalidateCompletion should not affect different project path")
	}
}
