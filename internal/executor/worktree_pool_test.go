package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWorktreePoolWarmup tests GH-1078: pool warmup creates worktrees at startup.
func TestWorktreePoolWarmup(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	ctx := context.Background()
	poolSize := 2
	manager := NewWorktreeManagerWithPool(localRepo, poolSize)
	defer manager.Close()

	// Verify pool is initially empty
	if manager.PoolAvailable() != 0 {
		t.Errorf("expected 0 available before warmup, got %d", manager.PoolAvailable())
	}

	// Warm the pool
	if err := manager.WarmPool(ctx); err != nil {
		t.Fatalf("WarmPool failed: %v", err)
	}

	// Verify pool is warmed
	if available := manager.PoolAvailable(); available != poolSize {
		t.Errorf("expected %d available after warmup, got %d", poolSize, available)
	}

	// Verify pool worktree directories exist
	for i := 0; i < poolSize; i++ {
		poolPath := filepath.Join(os.TempDir(), fmt.Sprintf("pilot-worktree-pool-%d", i))
		if _, err := os.Stat(poolPath); os.IsNotExist(err) {
			t.Errorf("pool worktree %d not created at %s", i, poolPath)
		}
	}
}

// TestWorktreePoolAcquireRelease tests GH-1078: acquire and release cycle.
func TestWorktreePoolAcquireRelease(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	ctx := context.Background()
	manager := NewWorktreeManagerWithPool(localRepo, 2)
	defer manager.Close()

	// Warm the pool
	if err := manager.WarmPool(ctx); err != nil {
		t.Fatalf("WarmPool failed: %v", err)
	}

	// Acquire a worktree
	branchName := "pilot/pool-test-1"
	result, err := manager.Acquire(ctx, "GH-1078-test", branchName, "main")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Verify worktree is on correct branch
	branchCmd := exec.Command("git", "-C", result.Path, "branch", "--show-current")
	branchOutput, _ := branchCmd.Output()
	if strings.TrimSpace(string(branchOutput)) != branchName {
		t.Errorf("expected branch %q, got %q", branchName, strings.TrimSpace(string(branchOutput)))
	}

	// Verify pool has one less available
	if available := manager.PoolAvailable(); available != 1 {
		t.Errorf("expected 1 available after acquire, got %d", available)
	}

	// Release the worktree
	result.Cleanup()

	// Give release a moment to complete
	time.Sleep(50 * time.Millisecond)

	// Verify pool has worktree back
	if available := manager.PoolAvailable(); available != 2 {
		t.Errorf("expected 2 available after release, got %d", available)
	}
}

// TestWorktreePoolFallbackWhenEmpty tests GH-1078: fallback to creation when pool empty.
func TestWorktreePoolFallbackWhenEmpty(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	ctx := context.Background()
	manager := NewWorktreeManagerWithPool(localRepo, 1)
	defer manager.Close()

	// Warm with size 1
	if err := manager.WarmPool(ctx); err != nil {
		t.Fatalf("WarmPool failed: %v", err)
	}

	// Acquire the only pooled worktree
	result1, err := manager.Acquire(ctx, "task-1", "pilot/branch-1", "main")
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	defer result1.Cleanup()

	// Pool is now empty - second acquire should fallback to CreateWorktreeWithBranch
	result2, err := manager.Acquire(ctx, "task-2", "pilot/branch-2", "main")
	if err != nil {
		t.Fatalf("second Acquire (fallback) failed: %v", err)
	}
	defer result2.Cleanup()

	// Verify both worktrees exist and are different
	if result1.Path == result2.Path {
		t.Error("expected different worktree paths for pool and fallback")
	}

	// Verify second worktree is on correct branch
	branchCmd := exec.Command("git", "-C", result2.Path, "branch", "--show-current")
	output, _ := branchCmd.Output()
	if strings.TrimSpace(string(output)) != "pilot/branch-2" {
		t.Errorf("expected branch pilot/branch-2, got %q", strings.TrimSpace(string(output)))
	}
}

// TestWorktreePoolClose tests GH-1078: clean shutdown drains pool.
func TestWorktreePoolClose(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	ctx := context.Background()
	poolSize := 2
	manager := NewWorktreeManagerWithPool(localRepo, poolSize)

	// Warm the pool
	if err := manager.WarmPool(ctx); err != nil {
		t.Fatalf("WarmPool failed: %v", err)
	}

	// Remember pool paths
	poolPaths := make([]string, poolSize)
	for i := 0; i < poolSize; i++ {
		poolPaths[i] = filepath.Join(os.TempDir(), fmt.Sprintf("pilot-worktree-pool-%d", i))
	}

	// Verify they exist before close
	for i, path := range poolPaths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("pool worktree %d should exist before close", i)
		}
	}

	// Close drains the pool
	manager.Close()

	// Verify pool worktrees are removed
	for i, path := range poolPaths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("pool worktree %d should be removed after close at %s", i, path)
		}
	}

	// Verify pool is empty
	if available := manager.PoolAvailable(); available != 0 {
		t.Errorf("expected 0 available after close, got %d", available)
	}
}

// TestWorktreePoolReuse tests GH-1078: same worktree reused across tasks.
func TestWorktreePoolReuse(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	ctx := context.Background()
	manager := NewWorktreeManagerWithPool(localRepo, 1)
	defer manager.Close()

	if err := manager.WarmPool(ctx); err != nil {
		t.Fatalf("WarmPool failed: %v", err)
	}

	// First task
	result1, err := manager.Acquire(ctx, "task-1", "pilot/first", "main")
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	firstPath := result1.Path

	// Create a file to verify it's cleaned on reuse
	testFile := filepath.Join(result1.Path, "task1-artifact.txt")
	if err := os.WriteFile(testFile, []byte("from task 1"), 0644); err != nil {
		t.Fatal(err)
	}

	// Release back to pool
	result1.Cleanup()
	time.Sleep(50 * time.Millisecond)

	// Second task - should reuse the same worktree
	result2, err := manager.Acquire(ctx, "task-2", "pilot/second", "main")
	if err != nil {
		t.Fatalf("second Acquire failed: %v", err)
	}
	defer result2.Cleanup()

	// Verify same path reused
	if result2.Path != firstPath {
		t.Errorf("expected same path %q, got %q", firstPath, result2.Path)
	}

	// Verify artifact was cleaned
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("artifact from previous task should be cleaned")
	}

	// Verify on new branch
	branchCmd := exec.Command("git", "-C", result2.Path, "branch", "--show-current")
	output, _ := branchCmd.Output()
	if strings.TrimSpace(string(output)) != "pilot/second" {
		t.Errorf("expected branch pilot/second, got %q", strings.TrimSpace(string(output)))
	}
}

// TestWorktreePoolSizeZeroDisabled tests GH-1078: pool_size=0 preserves current behavior.
func TestWorktreePoolSizeZeroDisabled(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	ctx := context.Background()

	// Pool size 0 = disabled
	manager := NewWorktreeManagerWithPool(localRepo, 0)
	defer manager.Close()

	// WarmPool should be a no-op
	if err := manager.WarmPool(ctx); err != nil {
		t.Fatalf("WarmPool failed: %v", err)
	}

	// Pool should be empty
	if available := manager.PoolAvailable(); available != 0 {
		t.Errorf("expected 0 available with pool disabled, got %d", available)
	}

	// Acquire should fallback to CreateWorktreeWithBranch
	result, err := manager.Acquire(ctx, "task-1", "pilot/no-pool", "main")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	defer result.Cleanup()

	// Verify worktree works
	branchCmd := exec.Command("git", "-C", result.Path, "branch", "--show-current")
	output, _ := branchCmd.Output()
	if strings.TrimSpace(string(output)) != "pilot/no-pool" {
		t.Errorf("expected branch pilot/no-pool, got %q", strings.TrimSpace(string(output)))
	}
}
