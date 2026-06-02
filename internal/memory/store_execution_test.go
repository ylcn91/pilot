package memory

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestExecutionCRUD(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Create
	exec := &Execution{
		ID:          "exec-1",
		TaskID:      "TASK-123",
		ProjectPath: "/path/to/project",
		Status:      "completed",
		Output:      "Success!",
		DurationMs:  5000,
		PRUrl:       "https://github.com/org/repo/pull/1",
		CommitSHA:   "abc123",
	}

	if err := store.SaveExecution(exec); err != nil {
		t.Fatalf("SaveExecution failed: %v", err)
	}

	// Read
	retrieved, err := store.GetExecution("exec-1")
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}

	if retrieved.TaskID != "TASK-123" {
		t.Errorf("Expected TaskID 'TASK-123', got '%s'", retrieved.TaskID)
	}
	if retrieved.Status != "completed" {
		t.Errorf("Expected Status 'completed', got '%s'", retrieved.Status)
	}
	if retrieved.PRUrl != "https://github.com/org/repo/pull/1" {
		t.Errorf("Expected PR URL, got '%s'", retrieved.PRUrl)
	}
}

func TestGetRecentExecutions(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Add multiple executions
	for i := 1; i <= 5; i++ {
		exec := &Execution{
			ID:          "exec-" + string(rune('0'+i)),
			TaskID:      "TASK-" + string(rune('0'+i)),
			ProjectPath: "/path",
			Status:      "completed",
		}
		_ = store.SaveExecution(exec)
	}

	recent, err := store.GetRecentExecutions(3)
	if err != nil {
		t.Fatalf("GetRecentExecutions failed: %v", err)
	}

	if len(recent) != 3 {
		t.Errorf("Expected 3 executions, got %d", len(recent))
	}
}

func TestExecution_FullLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	completedAt := time.Now()
	exec := &Execution{
		ID:               "exec-full-1",
		TaskID:           "TASK-456",
		ProjectPath:      "/path/to/project",
		Status:           "completed",
		Output:           "Build succeeded. All tests passed.",
		Error:            "",
		DurationMs:       15000,
		PRUrl:            "https://github.com/org/repo/pull/42",
		CommitSHA:        "abc123def456",
		CompletedAt:      &completedAt,
		TokensInput:      10000,
		TokensOutput:     5000,
		TokensTotal:      15000,
		EstimatedCostUSD: 0.15,
		FilesChanged:     5,
		LinesAdded:       100,
		LinesRemoved:     20,
		ModelName:        "claude-sonnet-4-6",
	}

	// Save
	if err := store.SaveExecution(exec); err != nil {
		t.Fatalf("SaveExecution failed: %v", err)
	}

	// Retrieve
	retrieved, err := store.GetExecution("exec-full-1")
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}

	// Verify all fields
	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"ID", retrieved.ID, exec.ID},
		{"TaskID", retrieved.TaskID, exec.TaskID},
		{"ProjectPath", retrieved.ProjectPath, exec.ProjectPath},
		{"Status", retrieved.Status, exec.Status},
		{"Output", retrieved.Output, exec.Output},
		{"DurationMs", retrieved.DurationMs, exec.DurationMs},
		{"PRUrl", retrieved.PRUrl, exec.PRUrl},
		{"CommitSHA", retrieved.CommitSHA, exec.CommitSHA},
		{"TokensInput", retrieved.TokensInput, exec.TokensInput},
		{"TokensOutput", retrieved.TokensOutput, exec.TokensOutput},
		{"TokensTotal", retrieved.TokensTotal, exec.TokensTotal},
		{"FilesChanged", retrieved.FilesChanged, exec.FilesChanged},
		{"LinesAdded", retrieved.LinesAdded, exec.LinesAdded},
		{"LinesRemoved", retrieved.LinesRemoved, exec.LinesRemoved},
		{"ModelName", retrieved.ModelName, exec.ModelName},
	}

	for _, tt := range tests {
		if tt.got != tt.expected {
			t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.expected)
		}
	}

	if retrieved.CompletedAt == nil {
		t.Error("CompletedAt should not be nil")
	}
}

func TestGetExecution_NotFound(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	_, err := store.GetExecution("nonexistent")
	if err == nil {
		t.Error("GetExecution should return error for nonexistent execution")
	}
}

func TestGetActiveExecutions(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Add executions with different statuses
	executions := []*Execution{
		{ID: "1", TaskID: "T1", ProjectPath: "/p", Status: "running"},
		{ID: "2", TaskID: "T2", ProjectPath: "/p", Status: "completed"},
		{ID: "3", TaskID: "T3", ProjectPath: "/p", Status: "running"},
		{ID: "4", TaskID: "T4", ProjectPath: "/p", Status: "failed"},
	}

	for _, e := range executions {
		_ = store.SaveExecution(e)
	}

	active, err := store.GetActiveExecutions()
	if err != nil {
		t.Fatalf("GetActiveExecutions failed: %v", err)
	}

	if len(active) != 2 {
		t.Errorf("Expected 2 active executions, got %d", len(active))
	}

	for _, e := range active {
		if e.Status != "running" {
			t.Errorf("Active execution has status %q, want 'running'", e.Status)
		}
	}
}

func TestGetQueuedTasks(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Add executions with different statuses
	executions := []*Execution{
		{ID: "1", TaskID: "T1", ProjectPath: "/p", Status: "queued"},
		{ID: "2", TaskID: "T2", ProjectPath: "/p", Status: "pending"},
		{ID: "3", TaskID: "T3", ProjectPath: "/p", Status: "running"},
		{ID: "4", TaskID: "T4", ProjectPath: "/p", Status: "queued"},
	}

	for _, e := range executions {
		_ = store.SaveExecution(e)
	}

	queued, err := store.GetQueuedTasks(10)
	if err != nil {
		t.Fatalf("GetQueuedTasks failed: %v", err)
	}

	if len(queued) != 3 {
		t.Errorf("Expected 3 queued/pending tasks, got %d", len(queued))
	}
}

// TestTaskLabelsRoundTrip verifies that Task.Labels survive the queue round-trip
// (SaveExecution → GetExecution and GetQueuedTasksForProject). Without this, labels
// like "no-decompose" are silently dropped and runner-side gates bypassed (GH-2326).
func TestTaskLabelsRoundTrip(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	cases := []struct {
		name   string
		labels []string
	}{
		{"nil labels", nil},
		{"empty slice", []string{}},
		{"single label", []string{"no-decompose"}},
		{"multiple labels", []string{"pilot", "no-decompose", "priority:high"}},
		{"special chars", []string{"kind/bug", "area/executor", "v1.0+"}},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			execID := fmt.Sprintf("exec-labels-%d", i)
			input := &Execution{
				ID:          execID,
				TaskID:      fmt.Sprintf("T-%d", i),
				ProjectPath: "/project/a",
				Status:      "queued",
				TaskTitle:   "test",
				TaskLabels:  tc.labels,
			}
			if err := store.SaveExecution(input); err != nil {
				t.Fatalf("SaveExecution: %v", err)
			}

			got, err := store.GetExecution(execID)
			if err != nil {
				t.Fatalf("GetExecution: %v", err)
			}
			// nil and empty slice both normalize to nil on read
			wantLen := len(tc.labels)
			if len(got.TaskLabels) != wantLen {
				t.Fatalf("labels length: got %d (%v), want %d (%v)", len(got.TaskLabels), got.TaskLabels, wantLen, tc.labels)
			}
			for j, l := range tc.labels {
				if got.TaskLabels[j] != l {
					t.Errorf("labels[%d]: got %q, want %q", j, got.TaskLabels[j], l)
				}
			}

			// Also verify the worker-facing read path returns labels.
			queued, err := store.GetQueuedTasksForProject("/project/a", 100)
			if err != nil {
				t.Fatalf("GetQueuedTasksForProject: %v", err)
			}
			var found *Execution
			for _, e := range queued {
				if e.ID == execID {
					found = e
					break
				}
			}
			if found == nil {
				t.Fatalf("execution %s not in queued list", execID)
			}
			if len(found.TaskLabels) != wantLen {
				t.Errorf("queued read labels length: got %d (%v), want %d", len(found.TaskLabels), found.TaskLabels, wantLen)
			}
		})
	}
}

func TestGetStaleQueuedExecutions(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	staleDuration := time.Hour

	now := time.Now()

	insertAt := func(exec *Execution, createdAt time.Time) {
		t.Helper()
		if err := store.SaveExecution(exec); err != nil {
			t.Fatalf("SaveExecution %s: %v", exec.ID, err)
		}
		if _, err := store.db.Exec(
			`UPDATE executions SET created_at = ? WHERE id = ?`, createdAt, exec.ID,
		); err != nil {
			t.Fatalf("set created_at for %s: %v", exec.ID, err)
		}
	}

	// Fresh queued execution (created now — should NOT be returned).
	insertAt(&Execution{ID: "queued-fresh", TaskID: "TASK-fresh", ProjectPath: "/proj", Status: "queued"}, now)

	// Stale queued execution (created 2 hours ago — should be returned).
	insertAt(&Execution{ID: "queued-stale", TaskID: "TASK-stale", ProjectPath: "/proj", Status: "queued"}, now.Add(-2*time.Hour))

	// Stale running execution — must NOT appear in queued results.
	insertAt(&Execution{ID: "running-stale", TaskID: "TASK-running", ProjectPath: "/proj", Status: "running"}, now.Add(-2*time.Hour))

	results, err := store.GetStaleQueuedExecutions(staleDuration)
	if err != nil {
		t.Fatalf("GetStaleQueuedExecutions: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 stale queued execution, got %d", len(results))
	}
	if results[0].ID != "queued-stale" {
		t.Errorf("expected ID %q, got %q", "queued-stale", results[0].ID)
	}
	if results[0].Status != "queued" {
		t.Errorf("expected status 'queued', got %q", results[0].Status)
	}
}
