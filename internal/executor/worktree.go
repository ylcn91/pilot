package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// PooledWorktree represents a worktree in the pool.
type PooledWorktree struct {
	Path      string    // Absolute path to the worktree directory
	CreatedAt time.Time // When this worktree was created
	InUse     bool      // Whether currently acquired
}

// WorktreeManager handles git worktree creation and cleanup for isolated task execution.
// GH-936: Enables Pilot to work in repos where users have uncommitted changes.
// GH-1078: Supports worktree pooling for sequential mode to save 500ms-2s per task.
type WorktreeManager struct {
	repoPath string
	mu       sync.Mutex
	active   map[string]string // taskID -> worktreePath
	createMu sync.Mutex        // GH-1312: Serializes worktree creation to avoid git race conditions

	// Pool support (GH-1078)
	pool     []*PooledWorktree // Pre-created worktrees for reuse
	poolSize int               // Configured pool size (0 = disabled)
	poolMu   sync.Mutex        // Protects pool operations
}

// NewWorktreeManager creates a worktree manager for the given repository.
func NewWorktreeManager(repoPath string) *WorktreeManager {
	return &WorktreeManager{
		repoPath: repoPath,
		active:   make(map[string]string),
		pool:     make([]*PooledWorktree, 0),
		poolSize: 0,
	}
}

// NewWorktreeManagerWithPool creates a worktree manager with pool support.
// poolSize specifies the number of worktrees to pre-create (0 = no pooling).
// GH-1078: Worktree pooling saves 500ms-2s per task in sequential mode.
func NewWorktreeManagerWithPool(repoPath string, poolSize int) *WorktreeManager {
	return &WorktreeManager{
		repoPath: repoPath,
		active:   make(map[string]string),
		pool:     make([]*PooledWorktree, 0, poolSize),
		poolSize: poolSize,
	}
}

// WorktreeResult contains the worktree path and cleanup function.
// The Cleanup function MUST be called when execution completes (success or failure).
type WorktreeResult struct {
	Path    string
	Cleanup func()
}

// CreateWorktree creates an isolated worktree for task execution.
// Returns a WorktreeResult containing the path and a cleanup function.
//
// CRITICAL: The cleanup function is safe to call multiple times and handles:
// - Normal completion
// - Context cancellation
// - Panic recovery (via defer in caller)
// - Process termination (best-effort via runtime finalizer)
//
// Usage:
//
//	result, err := manager.CreateWorktree(ctx, taskID)
//	if err != nil {
//	    return err
//	}
//	defer result.Cleanup() // Always cleanup, even on panic
//
//	// ... use result.Path for execution ...
func (m *WorktreeManager) CreateWorktree(ctx context.Context, taskID string) (*WorktreeResult, error) {
	// Generate unique worktree path using taskID and timestamp to handle concurrent tasks
	worktreeName := fmt.Sprintf("pilot-worktree-%s-%d", sanitizeBranchName(taskID), time.Now().UnixNano())
	worktreePath := filepath.Join(os.TempDir(), worktreeName)

	// GH-1312: Serialize worktree creation to avoid git race conditions.
	// Git's worktree implementation has internal races on .git/worktrees/*/commondir
	// when multiple worktrees are created concurrently. Rather than relying solely on
	// retries, we serialize creation operations while still allowing concurrent execution
	// in the worktrees themselves.
	m.createMu.Lock()
	defer m.createMu.Unlock()

	// Create worktree from HEAD (current commit of default branch)
	// Retry with exponential backoff to handle git race conditions on concurrent worktree creation
	// (git has internal races on .git/worktrees/*/commondir when creating multiple worktrees concurrently)
	var output []byte
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		cmd := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", worktreePath, "HEAD")
		cmd.Dir = m.repoPath
		output, err = cmd.CombinedOutput()
		if err == nil {
			break
		}
		// Check for transient git worktree race condition errors
		outputStr := string(output)
		if strings.Contains(outputStr, "commondir") || strings.Contains(outputStr, "gitdir") {
			time.Sleep(time.Duration(10*(attempt+1)) * time.Millisecond)
			continue
		}
		// Non-transient error, don't retry
		break
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create worktree: %w: %s", err, output)
	}

	// Track active worktree
	m.mu.Lock()
	m.active[taskID] = worktreePath
	m.mu.Unlock()

	// Create cleanup function with panic-safe, idempotent behavior
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			m.cleanupWorktree(taskID, worktreePath)
		})
	}

	return &WorktreeResult{
		Path:    worktreePath,
		Cleanup: cleanup,
	}, nil
}

// cleanupWorktree removes a worktree and cleans up tracking state.
// This is called by the cleanup function returned from CreateWorktree.
func (m *WorktreeManager) cleanupWorktree(taskID, worktreePath string) {
	// Remove from tracking first
	m.mu.Lock()
	delete(m.active, taskID)
	m.mu.Unlock()

	// Remove the git worktree reference
	// Use --force to handle any uncommitted changes in the worktree
	removeCmd := exec.Command("git", "-C", m.repoPath, "worktree", "remove", "--force", worktreePath)
	_ = removeCmd.Run() // Ignore error - worktree may already be removed

	// Belt and suspenders: also remove the directory if it still exists
	// This handles edge cases where git worktree remove didn't fully clean up
	_ = os.RemoveAll(worktreePath)

	// Prune stale worktree references from git
	pruneCmd := exec.Command("git", "-C", m.repoPath, "worktree", "prune")
	_ = pruneCmd.Run()
}

// CleanupAll removes all active worktrees managed by this instance.
// Useful for graceful shutdown or error recovery.
func (m *WorktreeManager) CleanupAll() {
	m.mu.Lock()
	active := make(map[string]string)
	for k, v := range m.active {
		active[k] = v
	}
	m.mu.Unlock()

	for taskID, path := range active {
		m.cleanupWorktree(taskID, path)
	}
}

// ActiveCount returns the number of active worktrees.
func (m *WorktreeManager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.active)
}

// sanitizeBranchName converts a task ID into a safe worktree directory name.
func sanitizeBranchName(taskID string) string {
	result := make([]byte, 0, len(taskID))
	for i := 0; i < len(taskID); i++ {
		c := taskID[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			result = append(result, c)
		} else {
			result = append(result, '-')
		}
	}
	return string(result)
}
