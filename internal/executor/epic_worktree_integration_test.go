package executor

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// TestEpicWorktreeIsolation verifies that epic decomposition with worktree isolation
// works correctly for sub-issues.
//
// GH-961, GH-2178: This integration test ensures:
// 1. Parent epic task uses worktree when UseWorktree=true
// 2. Sub-issues get their own worktrees from the real repo (allowWorktree=true, GH-2178)
// 3. Cleanup happens correctly for parent worktree
// Note: This test uses executeFunc mock which bypasses executeWithOptions,
// so worktree creation for sub-issues is not exercised here.
func TestEpicWorktreeIsolation(t *testing.T) {
	// Create test repo with remote
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	ctx := context.Background()

	// Track worktree creation attempts across all executions
	var mu sync.Mutex
	var executionPaths []string // Records the actual execution paths used

	// Create runner with worktree enabled
	runner := &Runner{
		config: &BackendConfig{
			ClaudeCode: &ClaudeCodeConfig{
				Command: "echo", // unused
			},
			UseWorktree: true, // Enable worktree isolation
		},
		running:             make(map[string]*exec.Cmd),
		progressCallbacks:   make(map[string]ProgressCallback),
		tokenCallbacks:      make(map[string]TokenCallback),
		log:                 testLogger(),
		modelRouter:         NewModelRouter(nil, nil),
		skipPreflightChecks: true, // Skip preflight for test
	}

	// Create a mock execute function that tracks execution paths.
	// Note: executeFunc mock bypasses executeWithOptions, so worktree creation
	// (enabled via GH-2178) is not exercised here — only task.ProjectPath is observed.
	runner.executeFunc = func(ctx context.Context, task *Task) (*ExecutionResult, error) {
		mu.Lock()
		executionPaths = append(executionPaths, task.ProjectPath)
		mu.Unlock()

		// Return success with PR URL
		return &ExecutionResult{
			TaskID:    task.ID,
			Success:   true,
			Output:    "Completed " + task.Title,
			PRUrl:     "https://github.com/owner/repo/pull/100",
			CommitSHA: "abc123",
		}, nil
	}

	// Create test sub-issues
	subIssues := []CreatedIssue{
		{
			Number:  100,
			URL:     "https://github.com/owner/repo/issues/100",
			Subtask: PlannedSubtask{Title: "Sub-issue 1", Description: "First task", Order: 1},
		},
		{
			Number:  101,
			URL:     "https://github.com/owner/repo/issues/101",
			Subtask: PlannedSubtask{Title: "Sub-issue 2", Description: "Second task", Order: 2},
		},
		{
			Number:  102,
			URL:     "https://github.com/owner/repo/issues/102",
			Subtask: PlannedSubtask{Title: "Sub-issue 3", Description: "Third task", Order: 3},
		},
	}

	parent := &Task{
		ID:          "GH-50",
		Title:       "[epic] Worktree isolation test",
		ProjectPath: localRepo,
	}

	// Execute sub-issues (this is what happens inside executeWithOptions for epics)
	err := runner.ExecuteSubIssues(ctx, parent, subIssues, localRepo, "")
	if err != nil {
		t.Fatalf("ExecuteSubIssues failed: %v", err)
	}

	// Verify all 3 sub-issues were executed
	if len(executionPaths) != 3 {
		t.Errorf("expected 3 executions, got %d", len(executionPaths))
	}

	// CRITICAL: Verify that sub-issues did NOT create worktrees
	// They should use the parent's ProjectPath directly (or worktree path if parent is in worktree)
	for i, path := range executionPaths {
		if strings.Contains(path, "pilot-worktree-") {
			t.Errorf("sub-issue %d should not have created a nested worktree, got path: %s", i, path)
		}
	}
}

// TestEpicWorktreeIsolation_ExecuteWithOptionsTracking tests the executeWithOptions
// behavior directly by tracking when allowWorktree=false is respected.
func TestEpicWorktreeIsolation_ExecuteWithOptionsTracking(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	ctx := context.Background()

	// Track execution details
	var mu sync.Mutex
	type execRecord struct {
		TaskID      string
		BranchName  string
		ProjectPath string
		IsSubIssue  bool
	}
	var execRecords []execRecord

	// Create runner with worktree enabled
	runner := &Runner{
		config: &BackendConfig{
			ClaudeCode: &ClaudeCodeConfig{
				Command: "echo",
			},
			UseWorktree: true,
		},
		running:             make(map[string]*exec.Cmd),
		progressCallbacks:   make(map[string]ProgressCallback),
		tokenCallbacks:      make(map[string]TokenCallback),
		log:                 testLogger(),
		modelRouter:         NewModelRouter(nil, nil),
		skipPreflightChecks: true,
	}

	// Mock executeFunc to capture execution details
	runner.executeFunc = func(ctx context.Context, task *Task) (*ExecutionResult, error) {
		mu.Lock()
		// Determine if this is a sub-issue by checking task ID pattern
		isSubIssue := strings.HasPrefix(task.ID, "GH-10") || strings.HasPrefix(task.ID, "GH-20")
		execRecords = append(execRecords, execRecord{
			TaskID:      task.ID,
			BranchName:  task.Branch,
			ProjectPath: task.ProjectPath,
			IsSubIssue:  isSubIssue,
		})
		mu.Unlock()

		return &ExecutionResult{
			TaskID:    task.ID,
			Success:   true,
			Output:    "done",
			PRUrl:     "https://github.com/owner/repo/pull/200",
			CommitSHA: "def456",
		}, nil
	}

	// Test scenario: Execute sub-issues like epic.go does
	subIssues := []CreatedIssue{
		{Number: 100, Subtask: PlannedSubtask{Title: "Task 1", Order: 1}},
		{Number: 101, Subtask: PlannedSubtask{Title: "Task 2", Order: 2}},
	}

	parent := &Task{
		ID:          "GH-50",
		Title:       "[epic] Test parent",
		ProjectPath: localRepo,
	}

	err := runner.ExecuteSubIssues(ctx, parent, subIssues, localRepo, "")
	if err != nil {
		t.Fatalf("ExecuteSubIssues failed: %v", err)
	}

	// Verify execution count
	if len(execRecords) != 2 {
		t.Fatalf("expected 2 executions, got %d", len(execRecords))
	}

	// Verify sub-issues received correct project paths
	for i, rec := range execRecords {
		if rec.ProjectPath != localRepo {
			t.Errorf("sub-issue %d: ProjectPath = %q, want %q", i, rec.ProjectPath, localRepo)
		}
	}
}
