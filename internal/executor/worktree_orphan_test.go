package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCleanupOrphanedWorktrees tests startup cleanup of orphaned worktree directories
func TestCleanupOrphanedWorktrees(t *testing.T) {
	repoPath := setupTestRepo(t)
	defer func() { _ = os.RemoveAll(repoPath) }()

	ctx := context.Background()
	tmpDir := os.TempDir()

	// Create some orphaned worktree directories in /tmp/
	orphan1 := filepath.Join(tmpDir, "pilot-worktree-task1-12345")
	orphan2 := filepath.Join(tmpDir, "pilot-worktree-task2-67890")
	orphan3 := filepath.Join(tmpDir, "some-other-directory") // Should be ignored

	// Create directories
	if err := os.MkdirAll(orphan1, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(orphan2, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(orphan3, 0755); err != nil {
		t.Fatal(err)
	}

	// Put some content in orphan directories to verify cleanup
	if err := os.WriteFile(filepath.Join(orphan1, "test.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphan2, "test.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify they exist before cleanup
	if _, err := os.Stat(orphan1); os.IsNotExist(err) {
		t.Fatal("orphan1 should exist before cleanup")
	}
	if _, err := os.Stat(orphan2); os.IsNotExist(err) {
		t.Fatal("orphan2 should exist before cleanup")
	}

	// Run cleanup
	err := CleanupOrphanedWorktrees(ctx, repoPath)
	if err != nil {
		// Error should report number of cleaned directories and freed space
		if !strings.Contains(err.Error(), "cleaned up") || !strings.Contains(err.Error(), "MB") {
			t.Errorf("unexpected error format: %v", err)
		}
	}

	// Verify orphaned pilot worktrees were removed
	if _, err := os.Stat(orphan1); !os.IsNotExist(err) {
		t.Error("orphan1 should be removed after cleanup")
	}
	if _, err := os.Stat(orphan2); !os.IsNotExist(err) {
		t.Error("orphan2 should be removed after cleanup")
	}

	// Verify other directories were not touched
	if _, err := os.Stat(orphan3); os.IsNotExist(err) {
		t.Error("orphan3 should not be removed (not a pilot worktree)")
	}

	// Cleanup test directory
	_ = os.RemoveAll(orphan3)
}

// TestCleanupOrphanedWorktrees_ValidWorktree tests GH-2168: valid worktrees connected
// to our repo ARE removed at startup — after OOM/SIGKILL, defer never runs so the
// worktree is valid but stale. At startup all non-pool pilot worktrees are stale.
func TestCleanupOrphanedWorktrees_ValidWorktree(t *testing.T) {
	repoPath := setupTestRepo(t)
	defer func() { _ = os.RemoveAll(repoPath) }()

	ctx := context.Background()
	manager := NewWorktreeManager(repoPath)

	// Create a valid worktree (simulates OOM scenario: worktree exists, .git reference valid)
	result, err := manager.CreateWorktree(ctx, "oom-task")
	if err != nil {
		t.Fatalf("failed to create worktree: %v", err)
	}

	// Verify worktree path looks like what cleanup would find
	if !strings.Contains(result.Path, "pilot-worktree-") {
		t.Skipf("worktree path doesn't match expected pattern: %s", result.Path)
	}

	// Verify it exists before cleanup
	if _, statErr := os.Stat(result.Path); statErr != nil {
		t.Fatalf("worktree should exist before cleanup: %v", statErr)
	}

	// Run cleanup — GH-2168: should remove valid-but-stale worktrees at startup
	err = CleanupOrphanedWorktrees(ctx, repoPath)
	if err == nil {
		t.Error("cleanup should report removed worktrees")
	} else if !strings.Contains(err.Error(), "cleaned up") {
		t.Errorf("unexpected error format: %v", err)
	}

	// Verify the valid worktree was removed (it's stale at startup)
	if _, statErr := os.Stat(result.Path); !os.IsNotExist(statErr) {
		t.Error("stale worktree should be removed after startup cleanup")
	}
}

// TestCleanupOrphanedWorktrees_PoolSkipped tests GH-2168: pool worktrees are NOT
// removed by startup cleanup — they are managed by WorktreeManager.Close().
func TestCleanupOrphanedWorktrees_PoolSkipped(t *testing.T) {
	repoPath := setupTestRepo(t)
	defer func() { _ = os.RemoveAll(repoPath) }()

	ctx := context.Background()
	tmpDir := os.TempDir()

	// Create a fake pool worktree directory
	poolDir := filepath.Join(tmpDir, "pilot-worktree-pool-0")
	if err := os.MkdirAll(poolDir, 0755); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(poolDir) }()

	// Run cleanup
	_ = CleanupOrphanedWorktrees(ctx, repoPath)

	// Pool worktree should NOT be removed
	if _, err := os.Stat(poolDir); os.IsNotExist(err) {
		t.Error("pool worktree should not be removed by startup cleanup")
	}
}

// TestCleanupOrphanedWorktrees_EmptyTmp tests behavior when /tmp/ has no pilot worktrees
func TestCleanupOrphanedWorktrees_EmptyTmp(t *testing.T) {
	repoPath := setupTestRepo(t)
	defer func() { _ = os.RemoveAll(repoPath) }()

	ctx := context.Background()

	// Run cleanup on clean system - should succeed with no action
	err := CleanupOrphanedWorktrees(ctx, repoPath)
	if err != nil {
		t.Errorf("cleanup should succeed with no orphans: %v", err)
	}
}
