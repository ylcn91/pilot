package executor

import (
	"context"
	"testing"
	"time"
)

func TestMonitorGetAll(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Task 1", "")
	monitor.Register("task-2", "Task 2", "")
	monitor.Register("task-3", "Task 3", "")

	all := monitor.GetAll()
	if len(all) != 3 {
		t.Errorf("Expected 3 tasks, got %d", len(all))
	}
}

func TestMonitorGetRunning(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Task 1", "")
	monitor.Register("task-2", "Task 2", "")
	monitor.Start("task-1")

	running := monitor.GetRunning()
	if len(running) != 1 {
		t.Errorf("Expected 1 running task, got %d", len(running))
	}
	if running[0].ID != "task-1" {
		t.Errorf("Expected task-1, got %s", running[0].ID)
	}
}

func TestMonitorCount(t *testing.T) {
	monitor := NewMonitor()

	if monitor.Count() != 0 {
		t.Error("Expected count 0 for empty monitor")
	}

	monitor.Register("task-1", "Task 1", "")
	monitor.Register("task-2", "Task 2", "")

	if monitor.Count() != 2 {
		t.Errorf("Expected count 2, got %d", monitor.Count())
	}
}

func TestMonitorGetNonexistent(t *testing.T) {
	monitor := NewMonitor()

	state, ok := monitor.Get("nonexistent")
	if ok {
		t.Error("Should not find nonexistent task")
	}
	if state != nil {
		t.Error("State should be nil for nonexistent task")
	}
}

func TestMonitorGetReturnsCopy(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Test Task", "")
	monitor.Start("task-1")

	state1, _ := monitor.Get("task-1")
	state1.Progress = 999 // Modify the copy

	state2, _ := monitor.Get("task-1")
	if state2.Progress == 999 {
		t.Error("Get should return a copy, not the original")
	}
}

func TestMonitorGetRunningMultiple(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Task 1", "")
	monitor.Register("task-2", "Task 2", "")
	monitor.Register("task-3", "Task 3", "")

	monitor.Start("task-1")
	monitor.Start("task-2")
	// task-3 remains pending

	running := monitor.GetRunning()
	if len(running) != 2 {
		t.Errorf("Expected 2 running tasks, got %d", len(running))
	}

	// Verify both running tasks are included
	ids := map[string]bool{}
	for _, s := range running {
		ids[s.ID] = true
	}
	if !ids["task-1"] || !ids["task-2"] {
		t.Error("GetRunning should include task-1 and task-2")
	}
	if ids["task-3"] {
		t.Error("GetRunning should not include pending task-3")
	}
}

func TestMonitorGetRunningNoRunning(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Task 1", "")
	monitor.Register("task-2", "Task 2", "")

	running := monitor.GetRunning()
	if len(running) != 0 {
		t.Errorf("Expected 0 running tasks, got %d", len(running))
	}
}

func TestMonitorGetRunningTaskIDs(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Task 1", "")
	monitor.Register("task-2", "Task 2", "")
	monitor.Register("task-3", "Task 3", "")

	monitor.Queue("task-1")
	monitor.Start("task-2")
	// task-3 stays pending

	ids := monitor.GetRunningTaskIDs()
	if len(ids) != 2 {
		t.Fatalf("Expected 2 active tasks (queued+running), got %d", len(ids))
	}
	// Should include both queued and running
	found := map[string]bool{}
	for _, id := range ids {
		found[id] = true
	}
	if !found["task-1"] {
		t.Error("Expected queued task-1 in active list")
	}
	if !found["task-2"] {
		t.Error("Expected running task-2 in active list")
	}
	if found["task-3"] {
		t.Error("Pending task-3 should not be in active list")
	}
}

func TestMonitorWaitForTasks(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Task 1", "")
	monitor.Start("task-1")

	// Complete task in background after 100ms
	go func() {
		time.Sleep(100 * time.Millisecond)
		monitor.Complete("task-1", "")
	}()

	ctx := context.Background()
	err := monitor.WaitForTasks(ctx, 5*time.Second)
	if err != nil {
		t.Fatalf("WaitForTasks should succeed, got: %v", err)
	}
}

func TestMonitorWaitForTasksTimeout(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Task 1", "")
	monitor.Start("task-1")
	// Never complete task-1

	ctx := context.Background()
	err := monitor.WaitForTasks(ctx, 100*time.Millisecond)
	if err == nil {
		t.Fatal("WaitForTasks should timeout")
	}
}

func TestTaskStatusConstants(t *testing.T) {
	tests := []struct {
		status   TaskStatus
		expected string
	}{
		{StatusPending, "pending"},
		{StatusQueued, "queued"},
		{StatusRunning, "running"},
		{StatusCompleted, "completed"},
		{StatusFailed, "failed"},
		{StatusCancelled, "cancelled"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("status = %q, want %q", tt.status, tt.expected)
			}
		})
	}
}

func TestTaskStateFields(t *testing.T) {
	state := &TaskState{
		ID:       "TASK-123",
		Title:    "Test Task",
		Status:   StatusRunning,
		Phase:    "Implementing",
		Progress: 50,
		Message:  "Working on it",
		Error:    "",
		PRUrl:    "",
	}

	if state.ID != "TASK-123" {
		t.Errorf("ID = %q, want TASK-123", state.ID)
	}
	if state.Title != "Test Task" {
		t.Errorf("Title = %q, want Test Task", state.Title)
	}
	if state.Status != StatusRunning {
		t.Errorf("Status = %v, want running", state.Status)
	}
	if state.Phase != "Implementing" {
		t.Errorf("Phase = %q, want Implementing", state.Phase)
	}
	if state.Progress != 50 {
		t.Errorf("Progress = %d, want 50", state.Progress)
	}
	if state.Message != "Working on it" {
		t.Errorf("Message = %q, want Working on it", state.Message)
	}
}

// GH-2167: SetProjectInfo attaches project path and name to a registered task.
func TestMonitorSetProjectInfo(t *testing.T) {
	m := NewMonitor()
	m.Register("GH-1", "Test task", "https://example.com/1")
	m.SetProjectInfo("GH-1", "/home/user/pilot", "pilot")

	state, ok := m.Get("GH-1")
	if !ok {
		t.Fatal("task not found")
	}
	if state.ProjectPath != "/home/user/pilot" {
		t.Errorf("ProjectPath = %q, want /home/user/pilot", state.ProjectPath)
	}
	if state.ProjectName != "pilot" {
		t.Errorf("ProjectName = %q, want pilot", state.ProjectName)
	}
}

func TestMonitorSetProjectInfo_NonExistent(t *testing.T) {
	m := NewMonitor()
	// Should not panic on non-existent task
	m.SetProjectInfo("GH-999", "/tmp/foo", "foo")
}
