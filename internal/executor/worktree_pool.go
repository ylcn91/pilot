package executor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// WarmPool pre-creates poolSize worktrees for fast acquisition.
// GH-1078: Called at Runner startup when worktree pooling is enabled.
// Each pooled worktree is created in detached HEAD state at origin/main.
func (m *WorktreeManager) WarmPool(ctx context.Context) error {
	if m.poolSize <= 0 {
		return nil // Pooling disabled
	}

	m.poolMu.Lock()
	defer m.poolMu.Unlock()

	// Already warmed?
	if len(m.pool) >= m.poolSize {
		return nil
	}

	slog.Info("Warming worktree pool",
		slog.Int("pool_size", m.poolSize),
		slog.String("repo", m.repoPath),
	)

	for i := len(m.pool); i < m.poolSize; i++ {
		pooledWT, err := m.createPooledWorktree(ctx, i)
		if err != nil {
			slog.Warn("Failed to create pooled worktree",
				slog.Int("index", i),
				slog.Any("error", err),
			)
			// Continue trying to create remaining worktrees
			continue
		}
		m.pool = append(m.pool, pooledWT)
	}

	slog.Info("Worktree pool warmed",
		slog.Int("created", len(m.pool)),
		slog.Int("target", m.poolSize),
	)

	return nil
}

// createPooledWorktree creates a single worktree for the pool.
func (m *WorktreeManager) createPooledWorktree(ctx context.Context, index int) (*PooledWorktree, error) {
	// Pool paths use consistent naming: /tmp/pilot-worktree-pool-N/
	worktreePath := filepath.Join(os.TempDir(), fmt.Sprintf("pilot-worktree-pool-%d", index))

	// Remove any existing directory at this path
	_ = os.RemoveAll(worktreePath)

	// Remove any stale git worktree reference
	pruneCmd := exec.CommandContext(ctx, "git", "worktree", "prune")
	pruneCmd.Dir = m.repoPath
	_ = pruneCmd.Run()

	// Fetch latest origin/main to ensure fresh base
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin", "main")
	fetchCmd.Dir = m.repoPath
	_, _ = fetchCmd.CombinedOutput() // Non-fatal if this fails

	// Create worktree in detached HEAD state at origin/main
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", worktreePath, "origin/main")
	cmd.Dir = m.repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to create pooled worktree: %w: %s", err, output)
	}

	return &PooledWorktree{
		Path:      worktreePath,
		CreatedAt: time.Now(),
		InUse:     false,
	}, nil
}

// Acquire gets a worktree from the pool and prepares it for the given branch.
// If the pool is empty, falls back to CreateWorktreeWithBranch.
// GH-1078: Reuses pooled worktrees by running git clean -fd && git checkout -B <branch>.
//
// Returns a WorktreeResult with a cleanup function that calls Release instead of destroying.
func (m *WorktreeManager) Acquire(ctx context.Context, taskID, branchName, baseBranch string) (*WorktreeResult, error) {
	m.poolMu.Lock()

	// Find an available worktree in the pool
	var acquired *PooledWorktree
	for _, wt := range m.pool {
		if !wt.InUse {
			wt.InUse = true
			acquired = wt
			break
		}
	}

	m.poolMu.Unlock()

	// Pool empty or no available worktrees - fall back to standard creation
	if acquired == nil {
		slog.Debug("Pool empty, falling back to CreateWorktreeWithBranch",
			slog.String("task_id", taskID),
		)
		return m.CreateWorktreeWithBranch(ctx, taskID, branchName, baseBranch)
	}

	slog.Info("Acquired pooled worktree",
		slog.String("task_id", taskID),
		slog.String("path", acquired.Path),
		slog.String("branch", branchName),
	)

	// Prepare the pooled worktree for this task
	if err := m.preparePooledWorktree(ctx, acquired, branchName, baseBranch); err != nil {
		// Mark as not in use and fall back
		m.poolMu.Lock()
		acquired.InUse = false
		m.poolMu.Unlock()

		slog.Warn("Failed to prepare pooled worktree, falling back to create",
			slog.String("path", acquired.Path),
			slog.Any("error", err),
		)
		return m.CreateWorktreeWithBranch(ctx, taskID, branchName, baseBranch)
	}

	// Track as active
	m.mu.Lock()
	m.active[taskID] = acquired.Path
	m.mu.Unlock()

	// Create cleanup function that releases back to pool instead of destroying
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			m.Release(taskID, acquired)
		})
	}

	return &WorktreeResult{
		Path:    acquired.Path,
		Cleanup: cleanup,
	}, nil
}

// preparePooledWorktree cleans and switches a pooled worktree to the target branch.
// Runs: git clean -fd && git checkout -B <branch> <base>
func (m *WorktreeManager) preparePooledWorktree(ctx context.Context, wt *PooledWorktree, branchName, baseBranch string) error {
	// Determine base ref
	baseRef := "origin/main"
	if baseBranch != "" {
		baseRef = baseBranch
	}

	// Fetch latest to ensure we have fresh refs
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin", "main")
	fetchCmd.Dir = wt.Path
	_, _ = fetchCmd.CombinedOutput() // Non-fatal

	// Clean any leftover files from previous task
	cleanCmd := exec.CommandContext(ctx, "git", "clean", "-fd")
	cleanCmd.Dir = wt.Path
	if output, err := cleanCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clean failed: %w: %s", err, output)
	}

	// Reset to base ref (discard any local changes).
	// SAFE: this is an isolated pooled worktree (tmp dir), not a user-facing
	// branch. Unlike syncMainBranch (GH-3018), there are no local commits
	// that could be silently lost here — the worktree is reused across tasks
	// and reset to baseRef at the start of each one by design.
	resetCmd := exec.CommandContext(ctx, "git", "reset", "--hard", baseRef)
	resetCmd.Dir = wt.Path
	if output, err := resetCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git reset failed: %w: %s", err, output)
	}

	// Create/reset the target branch
	checkoutCmd := exec.CommandContext(ctx, "git", "checkout", "-B", branchName, baseRef)
	checkoutCmd.Dir = wt.Path
	if output, err := checkoutCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout failed: %w: %s", err, output)
	}

	return nil
}

// Release returns a worktree to the pool after task completion.
// Validates the worktree is in a clean state before returning to pool.
// GH-1078: If validation fails, the worktree is recreated.
func (m *WorktreeManager) Release(taskID string, wt *PooledWorktree) {
	// Remove from active tracking
	m.mu.Lock()
	delete(m.active, taskID)
	m.mu.Unlock()

	// Validate worktree is still usable
	if !m.validatePooledWorktree(wt) {
		slog.Warn("Released worktree failed validation, will recreate",
			slog.String("path", wt.Path),
		)
		// Try to recreate it
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		m.poolMu.Lock()
		// Find index of this worktree in pool
		for i, pooledWT := range m.pool {
			if pooledWT == wt {
				// Remove from pool
				m.pool = append(m.pool[:i], m.pool[i+1:]...)

				// Clean up the broken worktree
				_ = os.RemoveAll(wt.Path)
				pruneCmd := exec.Command("git", "-C", m.repoPath, "worktree", "prune")
				_ = pruneCmd.Run()

				// Create a new one
				newWT, err := m.createPooledWorktree(ctx, i)
				if err == nil {
					m.pool = append(m.pool, newWT)
				}
				break
			}
		}
		m.poolMu.Unlock()
		return
	}

	// Mark as available
	m.poolMu.Lock()
	wt.InUse = false
	m.poolMu.Unlock()

	slog.Debug("Released worktree back to pool",
		slog.String("task_id", taskID),
		slog.String("path", wt.Path),
	)
}

// validatePooledWorktree checks if a worktree is in a valid state for reuse.
func (m *WorktreeManager) validatePooledWorktree(wt *PooledWorktree) bool {
	// Check directory exists
	if _, err := os.Stat(wt.Path); err != nil {
		return false
	}

	// Check .git file exists (indicates valid worktree)
	gitFile := filepath.Join(wt.Path, ".git")
	if _, err := os.Stat(gitFile); err != nil {
		return false
	}

	// Check git status runs without error
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = wt.Path
	if err := statusCmd.Run(); err != nil {
		return false
	}

	return true
}

// Close drains the worktree pool and removes all pooled worktrees.
// Should be called during graceful shutdown.
// GH-1078: Ensures clean shutdown without leaving orphaned worktrees.
func (m *WorktreeManager) Close() {
	m.poolMu.Lock()
	defer m.poolMu.Unlock()

	if len(m.pool) == 0 {
		return
	}

	slog.Info("Draining worktree pool",
		slog.Int("count", len(m.pool)),
	)

	for _, wt := range m.pool {
		// Remove the git worktree reference
		removeCmd := exec.Command("git", "-C", m.repoPath, "worktree", "remove", "--force", wt.Path)
		_ = removeCmd.Run()

		// Remove directory if still exists
		_ = os.RemoveAll(wt.Path)
	}

	// Clear pool
	m.pool = m.pool[:0]

	// Prune any stale references
	pruneCmd := exec.Command("git", "-C", m.repoPath, "worktree", "prune")
	_ = pruneCmd.Run()

	slog.Info("Worktree pool drained")
}

// PoolSize returns the configured pool size.
func (m *WorktreeManager) PoolSize() int {
	return m.poolSize
}

// PoolAvailable returns the number of available (not in use) worktrees in the pool.
func (m *WorktreeManager) PoolAvailable() int {
	m.poolMu.Lock()
	defer m.poolMu.Unlock()

	count := 0
	for _, wt := range m.pool {
		if !wt.InUse {
			count++
		}
	}
	return count
}
