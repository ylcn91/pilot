package executor

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// TestEpicWorktreeCleanup verifies that worktree cleanup happens correctly
// for epic tasks, including when sub-issues fail.
func TestEpicWorktreeCleanup(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	ctx := context.Background()

	// Create a worktree manager for the test
	manager := NewWorktreeManager(localRepo)

	// Create a worktree (simulating what executeWithOptions does for parent epic)
	result, err := manager.CreateWorktreeWithBranch(ctx, "epic-test", "pilot/GH-EPIC", "main")
	if err != nil {
		t.Fatalf("CreateWorktreeWithBranch failed: %v", err)
	}
	worktreePath := result.Path

	// Verify worktree was created
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		t.Fatal("worktree should exist")
	}

	// Verify active count
	if count := manager.ActiveCount(); count != 1 {
		t.Errorf("expected 1 active worktree, got %d", count)
	}

	// Simulate sub-issue execution within the worktree
	// Sub-issues should NOT create additional worktrees
	runner := &Runner{
		config: &BackendConfig{
			ClaudeCode:  &ClaudeCodeConfig{Command: "echo"},
			UseWorktree: true,
		},
		running:             make(map[string]*exec.Cmd),
		progressCallbacks:   make(map[string]ProgressCallback),
		tokenCallbacks:      make(map[string]TokenCallback),
		log:                 testLogger(),
		modelRouter:         NewModelRouter(nil, nil),
		skipPreflightChecks: true,
	}

	// Track execution paths
	var executedPaths []string
	runner.executeFunc = func(ctx context.Context, task *Task) (*ExecutionResult, error) {
		executedPaths = append(executedPaths, task.ProjectPath)
		return &ExecutionResult{
			TaskID:    task.ID,
			Success:   true,
			PRUrl:     "https://github.com/owner/repo/pull/300",
			CommitSHA: "test123",
		}, nil
	}

	// Execute sub-issues with the worktree path as ProjectPath
	subIssues := []CreatedIssue{
		{Number: 200, Subtask: PlannedSubtask{Title: "Sub 1", Order: 1}},
		{Number: 201, Subtask: PlannedSubtask{Title: "Sub 2", Order: 2}},
	}

	parent := &Task{
		ID:          "GH-EPIC",
		Title:       "[epic] Cleanup test",
		ProjectPath: worktreePath, // Use worktree path
	}

	// GH-2177: Pass localRepo as repoPath so sub-issues branch from real repo
	err = runner.ExecuteSubIssues(ctx, parent, subIssues, worktreePath, localRepo)
	if err != nil {
		t.Fatalf("ExecuteSubIssues failed: %v", err)
	}

	// GH-2177: Verify sub-issues used real repo path (not worktree path)
	for i, path := range executedPaths {
		if path != localRepo {
			t.Errorf("sub-issue %d: executed in %q, want %q (real repo, not worktree)", i, path, localRepo)
		}
	}

	// Cleanup the worktree
	result.Cleanup()

	// Verify worktree was cleaned up
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Error("worktree should be cleaned up")
	}

	// Verify active count is 0
	if count := manager.ActiveCount(); count != 0 {
		t.Errorf("expected 0 active worktrees after cleanup, got %d", count)
	}
}

// TestNoRecursiveWorktreeInDecomposedTasks verifies that decomposed tasks
// (not epics, but regular decomposition) also don't create nested worktrees.
func TestNoRecursiveWorktreeInDecomposedTasks(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	ctx := context.Background()

	// Track worktree creation in executeWithOptions
	var mu sync.Mutex
	var worktreeAttempts int

	// GH-2178: Sub-issues now get allowWorktree=true, but this test uses executeFunc
	// mock which bypasses executeWithOptions. The mock observes task.ProjectPath
	// (real repo path from GH-2177), not worktree paths.
	runner := &Runner{
		config: &BackendConfig{
			ClaudeCode:  &ClaudeCodeConfig{Command: "echo"},
			UseWorktree: true,
		},
		running:             make(map[string]*exec.Cmd),
		progressCallbacks:   make(map[string]ProgressCallback),
		tokenCallbacks:      make(map[string]TokenCallback),
		log:                 testLogger(),
		modelRouter:         NewModelRouter(nil, nil),
		skipPreflightChecks: true,
	}

	// Override executeFunc to track what happens
	runner.executeFunc = func(ctx context.Context, task *Task) (*ExecutionResult, error) {
		mu.Lock()
		// Check if the path contains a worktree marker
		if strings.Contains(task.ProjectPath, "pilot-worktree-") {
			worktreeAttempts++
		}
		mu.Unlock()

		return &ExecutionResult{
			TaskID:    task.ID,
			Success:   true,
			PRUrl:     "https://github.com/owner/repo/pull/400",
			CommitSHA: "xyz789",
		}, nil
	}

	// Execute sub-issues
	subIssues := []CreatedIssue{
		{Number: 300, Subtask: PlannedSubtask{Title: "Decomposed 1", Order: 1}},
		{Number: 301, Subtask: PlannedSubtask{Title: "Decomposed 2", Order: 2}},
		{Number: 302, Subtask: PlannedSubtask{Title: "Decomposed 3", Order: 3}},
	}

	parent := &Task{
		ID:          "GH-DECOMP",
		Title:       "Decomposed task test",
		ProjectPath: localRepo, // NOT a worktree path
	}

	err := runner.ExecuteSubIssues(ctx, parent, subIssues, localRepo, "")
	if err != nil {
		t.Fatalf("ExecuteSubIssues failed: %v", err)
	}

	// CRITICAL: No worktree paths should have been passed to sub-issues
	if worktreeAttempts > 0 {
		t.Errorf("expected 0 worktree path attempts in sub-issues, got %d", worktreeAttempts)
	}
}
