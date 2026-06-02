package executor

import (
	"context"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

func TestDispatcher_RecoverStaleTasks(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Insert a "stale" running task (we use 0 duration to make it immediately stale)
	exec := &memory.Execution{
		ID:          "exec-recover",
		TaskID:      "TASK-RECOVER",
		ProjectPath: "/project",
		Status:      "running",
	}
	if err := store.SaveExecution(exec); err != nil {
		t.Fatalf("failed to save execution: %v", err)
	}

	// Create dispatcher with 0 stale duration
	config := &DispatcherConfig{
		StaleTaskDuration: 0, // Everything is stale
	}
	runner := NewRunner()
	dispatcher := NewDispatcher(store, runner, config)

	if err := dispatcher.Start(context.Background()); err != nil {
		t.Fatalf("failed to start dispatcher: %v", err)
	}
	defer dispatcher.Stop()

	// Check that the task was marked failed (not re-queued — re-queuing without
	// a worker just recreates the orphan).
	updated, err := store.GetExecution("exec-recover")
	if err != nil {
		t.Fatalf("failed to get execution: %v", err)
	}

	if updated.Status != "failed" {
		t.Errorf("expected recovered task to have status 'failed', got '%s'", updated.Status)
	}
}

func TestRecoverStaleTasks_QueuedAndRunning(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Insert a stale running task and a stale queued task.
	executions := []*memory.Execution{
		{ID: "exec-stale-run", TaskID: "TASK-RUN", ProjectPath: "/project", Status: "running"},
		{ID: "exec-stale-q", TaskID: "TASK-Q", ProjectPath: "/project", Status: "queued"},
		{ID: "exec-ok", TaskID: "TASK-OK", ProjectPath: "/project", Status: "completed"},
	}
	for _, exec := range executions {
		if err := store.SaveExecution(exec); err != nil {
			t.Fatalf("failed to save execution: %v", err)
		}
	}

	// Use 0 thresholds so everything is stale immediately.
	config := &DispatcherConfig{
		StaleRunningThreshold: 0,
		StaleQueuedThreshold:  0,
		StaleRecoveryInterval: time.Hour, // won't tick in this test
	}
	runner := NewRunner()
	dispatcher := NewDispatcher(store, runner, config)

	if err := dispatcher.Start(context.Background()); err != nil {
		t.Fatalf("failed to start dispatcher: %v", err)
	}
	defer dispatcher.Stop()

	// Both stale tasks should be failed.
	for _, id := range []string{"exec-stale-run", "exec-stale-q"} {
		exec, err := store.GetExecution(id)
		if err != nil {
			t.Fatalf("failed to get execution %s: %v", id, err)
		}
		if exec.Status != "failed" {
			t.Errorf("expected %s to be 'failed', got '%s'", id, exec.Status)
		}
	}

	// Completed task should be untouched.
	exec, err := store.GetExecution("exec-ok")
	if err != nil {
		t.Fatalf("failed to get execution: %v", err)
	}
	if exec.Status != "completed" {
		t.Errorf("expected completed task to remain 'completed', got '%s'", exec.Status)
	}
}

func TestRecoverStaleTasks_RunningSkipsWhenLiveWorker(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	exec := &memory.Execution{ID: "exec-live-run", TaskID: "TASK-LR", ProjectPath: "/project-live", Status: "running"}
	if err := store.SaveExecution(exec); err != nil {
		t.Fatalf("failed to save execution: %v", err)
	}

	config := &DispatcherConfig{
		StaleRunningThreshold: 0,
		StaleQueuedThreshold:  0,
		StaleRecoveryInterval: time.Hour,
	}
	runner := NewRunner()
	dispatcher := NewDispatcher(store, runner, config)

	// Inject a live worker for the project so the reaper should skip it.
	dispatcher.mu.Lock()
	dispatcher.workers["/project-live"] = &ProjectWorker{projectPath: "/project-live"}
	dispatcher.mu.Unlock()

	dispatcher.recoverStaleTasks()

	got, err := store.GetExecution("exec-live-run")
	if err != nil {
		t.Fatalf("failed to get execution: %v", err)
	}
	if got.Status != "running" {
		t.Errorf("expected running task with live worker to remain 'running', got '%s'", got.Status)
	}
}

func TestRecoverStaleTasks_QueuedSkipsWhenLiveWorker(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	exec := &memory.Execution{ID: "exec-live-q", TaskID: "TASK-LQ", ProjectPath: "/project-live", Status: "queued"}
	if err := store.SaveExecution(exec); err != nil {
		t.Fatalf("failed to save execution: %v", err)
	}

	config := &DispatcherConfig{
		StaleRunningThreshold: 0,
		StaleQueuedThreshold:  0,
		StaleRecoveryInterval: time.Hour,
	}
	runner := NewRunner()
	dispatcher := NewDispatcher(store, runner, config)

	dispatcher.mu.Lock()
	dispatcher.workers["/project-live"] = &ProjectWorker{projectPath: "/project-live"}
	dispatcher.mu.Unlock()

	dispatcher.recoverStaleTasks()

	got, err := store.GetExecution("exec-live-q")
	if err != nil {
		t.Fatalf("failed to get execution: %v", err)
	}
	if got.Status != "queued" {
		t.Errorf("expected queued task with live worker to remain 'queued', got '%s'", got.Status)
	}
}

func TestRecoverStaleTasks_RespectsThresholds(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Insert running and queued tasks that were just created.
	executions := []*memory.Execution{
		{ID: "exec-fresh-run", TaskID: "TASK-FR", ProjectPath: "/project", Status: "running"},
		{ID: "exec-fresh-q", TaskID: "TASK-FQ", ProjectPath: "/project", Status: "queued"},
	}
	for _, exec := range executions {
		if err := store.SaveExecution(exec); err != nil {
			t.Fatalf("failed to save execution: %v", err)
		}
	}

	// Use very long thresholds so nothing is stale.
	config := &DispatcherConfig{
		StaleRunningThreshold: 24 * time.Hour,
		StaleQueuedThreshold:  24 * time.Hour,
		StaleRecoveryInterval: time.Hour,
	}
	runner := NewRunner()
	dispatcher := NewDispatcher(store, runner, config)

	if err := dispatcher.Start(context.Background()); err != nil {
		t.Fatalf("failed to start dispatcher: %v", err)
	}
	defer dispatcher.Stop()

	// Nothing should have been marked failed.
	for _, tc := range []struct {
		id     string
		expect string
	}{
		{"exec-fresh-run", "running"},
		{"exec-fresh-q", "queued"},
	} {
		exec, err := store.GetExecution(tc.id)
		if err != nil {
			t.Fatalf("failed to get execution %s: %v", tc.id, err)
		}
		if exec.Status != tc.expect {
			t.Errorf("expected %s to remain '%s', got '%s'", tc.id, tc.expect, exec.Status)
		}
	}
}

func TestRunStaleRecoveryLoop_Periodic(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Use a very short interval so the loop ticks quickly.
	config := &DispatcherConfig{
		StaleRunningThreshold: 0,
		StaleQueuedThreshold:  0,
		StaleRecoveryInterval: 50 * time.Millisecond,
	}
	runner := NewRunner()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dispatcher := NewDispatcher(store, runner, config)
	if err := dispatcher.Start(ctx); err != nil {
		t.Fatalf("failed to start dispatcher: %v", err)
	}
	defer dispatcher.Stop()

	// Insert a stale task AFTER Start() (so the initial pass doesn't see it).
	time.Sleep(20 * time.Millisecond)
	exec := &memory.Execution{
		ID:          "exec-periodic",
		TaskID:      "TASK-PERIODIC",
		ProjectPath: "/project",
		Status:      "running",
	}
	if err := store.SaveExecution(exec); err != nil {
		t.Fatalf("failed to save execution: %v", err)
	}

	// Wait for the loop to tick and recover it.
	time.Sleep(200 * time.Millisecond)

	updated, err := store.GetExecution("exec-periodic")
	if err != nil {
		t.Fatalf("failed to get execution: %v", err)
	}
	if updated.Status != "failed" {
		t.Errorf("expected periodic recovery to mark task 'failed', got '%s'", updated.Status)
	}
}
