package executor

import (
	"testing"
)

func TestNewMonitor(t *testing.T) {
	monitor := NewMonitor()

	if monitor == nil {
		t.Fatal("NewMonitor returned nil")
	}
	if monitor.tasks == nil {
		t.Error("tasks map not initialized")
	}
}

func TestMonitorRegister(t *testing.T) {
	monitor := NewMonitor()

	monitor.Register("task-1", "Test Task", "")

	state, ok := monitor.Get("task-1")
	if !ok {
		t.Fatal("Failed to get registered task")
	}
	if state.ID != "task-1" {
		t.Errorf("Expected ID 'task-1', got '%s'", state.ID)
	}
	if state.Title != "Test Task" {
		t.Errorf("Expected title 'Test Task', got '%s'", state.Title)
	}
	if state.Status != StatusPending {
		t.Errorf("Expected status pending, got %s", state.Status)
	}
}

func TestMonitorQueue(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Test Task", "")

	monitor.Queue("task-1")

	state, _ := monitor.Get("task-1")
	if state.Status != StatusQueued {
		t.Errorf("Expected status queued, got %s", state.Status)
	}
	if state.Phase != "Queued" {
		t.Errorf("Expected phase 'Queued', got '%s'", state.Phase)
	}
}

func TestMonitorQueueThenStart(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Test Task", "")

	monitor.Queue("task-1")
	state, _ := monitor.Get("task-1")
	if state.Status != StatusQueued {
		t.Errorf("Expected status queued, got %s", state.Status)
	}

	monitor.Start("task-1")
	state, _ = monitor.Get("task-1")
	if state.Status != StatusRunning {
		t.Errorf("Expected status running after start, got %s", state.Status)
	}
	if state.StartedAt == nil {
		t.Error("StartedAt not set after start")
	}
}

func TestMonitorStart(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Test Task", "")

	monitor.Start("task-1")

	state, _ := monitor.Get("task-1")
	if state.Status != StatusRunning {
		t.Errorf("Expected status running, got %s", state.Status)
	}
	if state.StartedAt == nil {
		t.Error("StartedAt not set")
	}
}

func TestMonitorUpdateProgress(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Test Task", "")
	monitor.Start("task-1")

	monitor.UpdateProgress("task-1", "IMPL", 50, "Working...")

	state, _ := monitor.Get("task-1")
	if state.Phase != "IMPL" {
		t.Errorf("Expected phase 'IMPL', got '%s'", state.Phase)
	}
	if state.Progress != 50 {
		t.Errorf("Expected progress 50, got %d", state.Progress)
	}
	if state.Message != "Working..." {
		t.Errorf("Expected message 'Working...', got '%s'", state.Message)
	}
}

func TestMonitorComplete(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Test Task", "")
	monitor.Start("task-1")

	monitor.Complete("task-1", "https://github.com/org/repo/pull/1")

	state, _ := monitor.Get("task-1")
	if state.Status != StatusCompleted {
		t.Errorf("Expected status completed, got %s", state.Status)
	}
	if state.PRUrl != "https://github.com/org/repo/pull/1" {
		t.Errorf("Expected PR URL, got '%s'", state.PRUrl)
	}
	if state.CompletedAt == nil {
		t.Error("CompletedAt not set")
	}
}

func TestMonitorFail(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Test Task", "")
	monitor.Start("task-1")

	monitor.Fail("task-1", "Something went wrong")

	state, _ := monitor.Get("task-1")
	if state.Status != StatusFailed {
		t.Errorf("Expected status failed, got %s", state.Status)
	}
	if state.Error != "Something went wrong" {
		t.Errorf("Expected error message, got '%s'", state.Error)
	}
}

func TestMonitorRemove(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Task 1", "")

	monitor.Remove("task-1")

	_, ok := monitor.Get("task-1")
	if ok {
		t.Error("Task should have been removed")
	}
}

func TestMonitorCancel(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Test Task", "")
	monitor.Start("task-1")

	monitor.Cancel("task-1")

	state, ok := monitor.Get("task-1")
	if !ok {
		t.Fatal("Task should still exist after cancel")
	}
	if state.Status != StatusCancelled {
		t.Errorf("Expected status cancelled, got %s", state.Status)
	}
	if state.Phase != "Cancelled" {
		t.Errorf("Expected phase 'Cancelled', got '%s'", state.Phase)
	}
	if state.CompletedAt == nil {
		t.Error("CompletedAt should be set on cancel")
	}
}

func TestMonitorOperationsOnNonexistent(t *testing.T) {
	monitor := NewMonitor()

	// These should not panic on nonexistent tasks
	monitor.Start("nonexistent")
	monitor.UpdateProgress("nonexistent", "Phase", 50, "Message")
	monitor.Complete("nonexistent", "url")
	monitor.Fail("nonexistent", "error")
	monitor.Cancel("nonexistent")
	monitor.Remove("nonexistent")

	// Count should still be 0
	if monitor.Count() != 0 {
		t.Errorf("Count should be 0, got %d", monitor.Count())
	}
}

func TestMonitorUpdateProgressEmptyMessage(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Test Task", "")
	monitor.UpdateProgress("task-1", "Phase1", 25, "Initial message")

	// Update with empty message should not overwrite existing message
	monitor.UpdateProgress("task-1", "Phase2", 50, "")

	state, _ := monitor.Get("task-1")
	if state.Phase != "Phase2" {
		t.Errorf("Phase should update to Phase2, got %s", state.Phase)
	}
	if state.Progress != 50 {
		t.Errorf("Progress should update to 50, got %d", state.Progress)
	}
	if state.Message != "Initial message" {
		t.Errorf("Empty message should not overwrite, got '%s'", state.Message)
	}
}
