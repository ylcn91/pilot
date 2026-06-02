package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// TestWorktreeIsolationWithNavigatorCopy verifies that Navigator config
// is properly copied to worktree and available for sub-issue execution.
func TestWorktreeIsolationWithNavigatorCopy(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	// Create .agent/ directory in local repo
	agentDir := filepath.Join(localRepo, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("failed to create .agent dir: %v", err)
	}
	devReadme := filepath.Join(agentDir, "DEVELOPMENT-README.md")
	if err := os.WriteFile(devReadme, []byte("# Navigator\n"), 0644); err != nil {
		t.Fatalf("failed to write DEVELOPMENT-README.md: %v", err)
	}

	ctx := context.Background()

	// Create worktree
	manager := NewWorktreeManager(localRepo)
	result, err := manager.CreateWorktreeWithBranch(ctx, "nav-test", "pilot/GH-NAV", "main")
	if err != nil {
		t.Fatalf("CreateWorktreeWithBranch failed: %v", err)
	}
	defer result.Cleanup()

	// Copy Navigator to worktree
	if err := EnsureNavigatorInWorktree(localRepo, result.Path); err != nil {
		t.Fatalf("EnsureNavigatorInWorktree failed: %v", err)
	}

	// Verify Navigator was copied
	worktreeReadme := filepath.Join(result.Path, ".agent", "DEVELOPMENT-README.md")
	if _, err := os.Stat(worktreeReadme); err != nil {
		t.Errorf("Navigator should be copied to worktree: %v", err)
	}

	// Now execute sub-issues in the worktree - they should have access to Navigator
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

	// Check Navigator availability in executeFunc
	var navigatorAvailable bool
	runner.executeFunc = func(ctx context.Context, task *Task) (*ExecutionResult, error) {
		navPath := filepath.Join(task.ProjectPath, ".agent", "DEVELOPMENT-README.md")
		if _, err := os.Stat(navPath); err == nil {
			navigatorAvailable = true
		}
		return &ExecutionResult{
			TaskID:    task.ID,
			Success:   true,
			PRUrl:     "https://github.com/owner/repo/pull/500",
			CommitSHA: "nav123",
		}, nil
	}

	subIssues := []CreatedIssue{
		{Number: 400, Subtask: PlannedSubtask{Title: "Nav test", Order: 1}},
	}

	parent := &Task{
		ID:          "GH-NAV",
		Title:       "[epic] Navigator copy test",
		ProjectPath: result.Path, // Use worktree path
	}

	// GH-2177: Pass localRepo as repoPath — sub-issues use real repo path.
	// Navigator availability check still works because localRepo has .agent/
	err = runner.ExecuteSubIssues(ctx, parent, subIssues, result.Path, localRepo)
	if err != nil {
		t.Fatalf("ExecuteSubIssues failed: %v", err)
	}

	if !navigatorAvailable {
		t.Error("Navigator should be available in sub-issue repo path")
	}
}

// TestConcurrentEpicsWithWorktrees verifies that multiple epics can run
// concurrently with worktree isolation without conflicts.
func TestConcurrentEpicsWithWorktrees(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	ctx := context.Background()
	manager := NewWorktreeManager(localRepo)

	// Create multiple worktrees concurrently (simulating concurrent epics)
	const numEpics = 3
	var wg sync.WaitGroup
	results := make([]*WorktreeResult, numEpics)
	errors := make([]error, numEpics)

	for i := 0; i < numEpics; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			branchName := "pilot/GH-EPIC-" + string(rune('A'+idx))
			result, err := manager.CreateWorktreeWithBranch(ctx, "epic-"+string(rune('A'+idx)), branchName, "main")
			results[idx] = result
			errors[idx] = err
		}(i)
	}
	wg.Wait()

	// Verify all worktrees were created successfully
	for i := 0; i < numEpics; i++ {
		if errors[i] != nil {
			t.Errorf("epic %d worktree creation failed: %v", i, errors[i])
			continue
		}
		if results[i] == nil {
			t.Errorf("epic %d: nil result", i)
			continue
		}
		if _, err := os.Stat(results[i].Path); os.IsNotExist(err) {
			t.Errorf("epic %d: worktree not created at %s", i, results[i].Path)
		}
	}

	// Verify unique paths
	paths := make(map[string]bool)
	for i := 0; i < numEpics; i++ {
		if results[i] != nil {
			if paths[results[i].Path] {
				t.Error("duplicate worktree paths detected")
			}
			paths[results[i].Path] = true
		}
	}

	// Verify active count
	if count := manager.ActiveCount(); count != numEpics {
		t.Errorf("expected %d active worktrees, got %d", numEpics, count)
	}

	// Cleanup all concurrently
	for i := 0; i < numEpics; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if results[idx] != nil {
				results[idx].Cleanup()
			}
		}(i)
	}
	wg.Wait()

	// Verify all cleaned up
	if count := manager.ActiveCount(); count != 0 {
		t.Errorf("expected 0 active worktrees after cleanup, got %d", count)
	}
}
