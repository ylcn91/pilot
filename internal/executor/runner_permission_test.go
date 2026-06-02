package executor

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

// =============================================================================
// TeamChecker Permission Tests (GH-634)
// =============================================================================

func TestRunner_SetTeamChecker(t *testing.T) {
	runner := NewRunner()
	if runner.teamChecker != nil {
		t.Error("teamChecker should be nil by default")
	}

	checker := &mockTeamChecker{}
	runner.SetTeamChecker(checker)

	if runner.teamChecker == nil {
		t.Error("teamChecker should be set after SetTeamChecker")
	}
}

func TestRunner_Execute_NoTeamChecker_Allowed(t *testing.T) {
	// Without TeamChecker, execution should proceed past permission check.
	// It will fail later (no project dir, etc.) but NOT on permission.
	runner := NewRunner()

	task := &Task{
		ID:          "test-1",
		Title:       "Test task",
		Description: "Test",
		ProjectPath: "/tmp/test-no-checker",
		MemberID:    "member-123", // MemberID set but no checker
	}

	_, err := runner.Execute(t.Context(), task)
	if err != nil && strings.Contains(err.Error(), "permission") {
		t.Errorf("should not fail on permission without TeamChecker: %v", err)
	}
}

func TestRunner_Execute_TeamChecker_Denied(t *testing.T) {
	runner := NewRunner()
	checker := &mockTeamChecker{
		accessErr: os.ErrPermission,
	}
	runner.SetTeamChecker(checker)

	task := &Task{
		ID:          "test-denied",
		Title:       "Test task",
		Description: "Test",
		ProjectPath: "/tmp/test",
		MemberID:    "viewer-member",
	}

	result, err := runner.Execute(t.Context(), task)

	// Should return permission error
	if err == nil {
		t.Fatal("expected error for denied permission")
	}
	if !strings.Contains(err.Error(), "permission check failed") {
		t.Errorf("error should mention permission check: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result even on permission failure")
	}
	if result.Success {
		t.Error("result should not be successful on permission denial")
	}
	if !strings.Contains(result.Error, "permission denied") {
		t.Errorf("result error should mention permission denied: %s", result.Error)
	}

	// Verify the checker was called with correct args
	if checker.lastMember != "viewer-member" {
		t.Errorf("checker got memberID %q, want %q", checker.lastMember, "viewer-member")
	}
	if checker.lastPerm != "execute_tasks" {
		t.Errorf("checker got perm %q, want %q", checker.lastPerm, "execute_tasks")
	}
}

func TestRunner_Execute_TeamChecker_NoMemberID_Skipped(t *testing.T) {
	// When MemberID is empty, permission check should be skipped
	runner := NewRunner()
	checker := &mockTeamChecker{
		accessErr: os.ErrPermission, // Would fail if called
	}
	runner.SetTeamChecker(checker)

	task := &Task{
		ID:          "test-no-member",
		Title:       "Test task",
		Description: "Test",
		ProjectPath: "/tmp/test",
		MemberID:    "", // Empty MemberID
	}

	// Should NOT fail on permission (checker should not be called)
	_, err := runner.Execute(t.Context(), task)
	if err != nil && strings.Contains(err.Error(), "permission") {
		t.Errorf("should skip permission check when MemberID is empty: %v", err)
	}

	// Verify checker was NOT called
	if checker.lastMember != "" {
		t.Errorf("checker should not have been called, but got memberID %q", checker.lastMember)
	}
}

func TestTask_MemberID_Field(t *testing.T) {
	task := &Task{
		ID:       "test-1",
		MemberID: "member-abc",
	}

	if task.MemberID != "member-abc" {
		t.Errorf("got MemberID %q, want %q", task.MemberID, "member-abc")
	}
}

// =============================================================================
// CancelAll Tests (GH-883)
// =============================================================================

func TestRunner_CancelAll_Empty(t *testing.T) {
	runner := NewRunner()

	// CancelAll on empty running map should not panic
	runner.CancelAll()

	// Verify no tasks are running
	if len(runner.running) != 0 {
		t.Errorf("expected empty running map, got %d entries", len(runner.running))
	}
}

func TestRunner_CancelAll_WithProcesses(t *testing.T) {
	runner := NewRunner()

	// Create a long-running process (sleep)
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start test process: %v", err)
	}

	// Add it to the running map
	runner.mu.Lock()
	runner.running["test-task-1"] = cmd
	runner.mu.Unlock()

	// Verify process is running
	if cmd.Process == nil {
		t.Fatal("expected process to be started")
	}

	// CancelAll should signal the process
	runner.CancelAll()

	// Wait a bit for SIGTERM to take effect
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		// Process should have been terminated
		if err == nil {
			t.Error("expected process to be killed with error")
		}
	case <-time.After(2 * time.Second):
		// Force kill if still running (shouldn't happen)
		_ = cmd.Process.Kill()
		t.Error("process did not terminate after SIGTERM within timeout")
	}
}

func TestRunner_CancelAll_MultipleTasks(t *testing.T) {
	runner := NewRunner()

	// Start multiple sleep processes
	var cmds []*exec.Cmd
	for i := 0; i < 3; i++ {
		cmd := exec.Command("sleep", "60")
		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start test process %d: %v", i, err)
		}
		cmds = append(cmds, cmd)

		runner.mu.Lock()
		runner.running[fmt.Sprintf("test-task-%d", i)] = cmd
		runner.mu.Unlock()
	}

	// Verify all are in the map
	runner.mu.Lock()
	count := len(runner.running)
	runner.mu.Unlock()
	if count != 3 {
		t.Fatalf("expected 3 running tasks, got %d", count)
	}

	// CancelAll should signal all processes
	runner.CancelAll()

	// Wait for all processes to terminate
	for i, cmd := range cmds {
		done := make(chan error, 1)
		go func(c *exec.Cmd) {
			done <- c.Wait()
		}(cmd)

		select {
		case err := <-done:
			if err == nil {
				t.Errorf("expected process %d to be killed with error", i)
			}
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			t.Errorf("process %d did not terminate after SIGTERM within timeout", i)
		}
	}
}

// TestRunner_SetLogStore verifies SetLogStore wires the log store to the runner.
func TestRunner_SetLogStore(t *testing.T) {
	runner := NewRunner()
	if runner.logStore != nil {
		t.Fatal("logStore should be nil by default")
	}

	tmpDir := t.TempDir()
	store, err := memory.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	runner.SetLogStore(store)
	if runner.logStore != store {
		t.Error("SetLogStore did not set logStore")
	}
}

// TestRunner_saveLogEntry_NilStore verifies saveLogEntry is a no-op when logStore is nil.
func TestRunner_saveLogEntry_NilStore(t *testing.T) {
	runner := NewRunner()
	// Should not panic with nil store
	runner.saveLogEntry("exec-1", "info", "test message")
}

// TestRunner_saveLogEntry_WritesEntry verifies saveLogEntry writes to SQLite and can be read back.
func TestRunner_saveLogEntry_WritesEntry(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := memory.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	runner := NewRunner()
	runner.SetLogStore(store)

	// Write several milestone entries
	runner.saveLogEntry("exec-42", "info", "Task started: Add feature X")
	runner.saveLogEntry("exec-42", "info", "Branch created: pilot/GH-42")
	runner.saveLogEntry("exec-42", "info", "Implementing changes...")
	runner.saveLogEntry("exec-42", "info", "Running tests...")
	runner.saveLogEntry("exec-42", "info", "Running self-review...")
	runner.saveLogEntry("exec-42", "info", "PR created: https://github.com/test/repo/pull/42")
	runner.saveLogEntry("exec-42", "info", "Task completed successfully")

	// Read back and verify
	logs, err := store.GetRecentLogs(20)
	if err != nil {
		t.Fatalf("failed to get recent logs: %v", err)
	}

	if len(logs) != 7 {
		t.Fatalf("expected 7 log entries, got %d", len(logs))
	}

	// Logs are returned newest-first
	if logs[0].Message != "Task completed successfully" {
		t.Errorf("latest log message = %q, want %q", logs[0].Message, "Task completed successfully")
	}
	if logs[0].ExecutionID != "exec-42" {
		t.Errorf("execution_id = %q, want %q", logs[0].ExecutionID, "exec-42")
	}
	if logs[0].Level != "info" {
		t.Errorf("level = %q, want %q", logs[0].Level, "info")
	}
	if logs[0].Component != "executor" {
		t.Errorf("component = %q, want %q", logs[0].Component, "executor")
	}

	// Verify first entry (oldest, at end of list)
	if logs[6].Message != "Task started: Add feature X" {
		t.Errorf("oldest log message = %q, want %q", logs[6].Message, "Task started: Add feature X")
	}
}

// TestRunner_saveLogEntry_ErrorLevel verifies error-level entries are stored correctly.
func TestRunner_saveLogEntry_ErrorLevel(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := memory.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	runner := NewRunner()
	runner.SetLogStore(store)

	runner.saveLogEntry("exec-99", "error", "Task failed: compilation error")

	logs, err := store.GetRecentLogs(5)
	if err != nil {
		t.Fatalf("failed to get recent logs: %v", err)
	}

	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}
	if logs[0].Level != "error" {
		t.Errorf("level = %q, want %q", logs[0].Level, "error")
	}
	if logs[0].Message != "Task failed: compilation error" {
		t.Errorf("message = %q, want %q", logs[0].Message, "Task failed: compilation error")
	}
}
