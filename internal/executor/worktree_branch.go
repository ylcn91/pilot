package executor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CreateWorktreeWithBranch creates an isolated worktree with a proper branch (not detached HEAD).
// This is the preferred method when the worktree needs to push to remote, as detached HEAD
// makes push operations more complex.
//
// The branch is created from the specified baseBranch (e.g., "main").
// If baseBranch is empty, HEAD is used.
//
// GH-1016: Uses atomic creation with retry to handle race conditions when two Pilots
// try to create the same branch simultaneously. Uses git worktree add -B to force
// create/reset the branch.
//
// Usage:
//
//	result, err := manager.CreateWorktreeWithBranch(ctx, taskID, "pilot/GH-123", "main")
//	if err != nil {
//	    return err
//	}
//	defer result.Cleanup()
//
//	// Worktree is on branch "pilot/GH-123", ready for commits and push
func (m *WorktreeManager) CreateWorktreeWithBranch(ctx context.Context, taskID, branchName, baseBranch string) (*WorktreeResult, error) {
	// Generate unique worktree path
	worktreeName := fmt.Sprintf("pilot-worktree-%s-%d", sanitizeBranchName(taskID), time.Now().UnixNano())
	worktreePath := filepath.Join(os.TempDir(), worktreeName)

	// GH-1312: Serialize worktree creation to avoid git race conditions.
	// Git's worktree implementation has internal races on .git/worktrees/*/commondir
	// when multiple worktrees are created concurrently.
	m.createMu.Lock()
	defer m.createMu.Unlock()

	// Determine the base ref and the branch to fetch. Default to main for
	// backward compatibility; an explicit task base branch (e.g. a dev-based
	// fork) overrides it so the worktree branches from the right base instead
	// of a hardcoded origin/main.
	baseBranchName := "main"
	baseRef := "origin/main"
	if baseBranch != "" {
		baseBranchName = strings.TrimPrefix(baseBranch, "origin/")
		baseRef = baseBranch
	}

	// GH-1211: Always fetch the base from origin before creating the worktree to
	// prevent branching from a stale local ref.
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin", baseBranchName)
	fetchCmd.Dir = m.repoPath
	if output, fetchErr := fetchCmd.CombinedOutput(); fetchErr != nil {
		slog.Warn("Failed to fetch base branch before worktree creation",
			slog.String("base", baseBranchName),
			slog.Any("error", fetchErr),
			slog.String("output", string(output)),
		)
		// Non-fatal: proceed with whatever local ref resolves.
	}

	// GH-963: Clean up any stale worktree for this branch before creating.
	// This handles retries where previous cleanup failed to fully remove the worktree reference.
	m.cleanupStaleWorktreeForBranch(ctx, branchName)

	// Use -B to force create/reset the branch atomically
	// git worktree add -B <branch> <path> <base>
	// -B creates the branch if it doesn't exist, or resets it if it does
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "-B", branchName, worktreePath, baseRef)
	cmd.Dir = m.repoPath
	output, err := cmd.CombinedOutput()

	if err != nil {
		return nil, fmt.Errorf("failed to create worktree with branch: %w: %s", err, output)
	}

	// Success - track and return
	m.mu.Lock()
	m.active[taskID] = worktreePath
	m.mu.Unlock()

	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			m.cleanupWorktreeAndBranch(taskID, worktreePath, branchName)
		})
	}

	return &WorktreeResult{
		Path:    worktreePath,
		Cleanup: cleanup,
	}, nil
}

// cleanupWorktreeAndBranch removes a worktree, its branch, and cleans up tracking state.
// The branch is also deleted since it was created specifically for this worktree.
func (m *WorktreeManager) cleanupWorktreeAndBranch(taskID, worktreePath, branchName string) {
	// First remove the worktree
	m.cleanupWorktree(taskID, worktreePath)

	// Then delete the local branch (it was created for this worktree)
	// Use -D to force delete even if not merged
	deleteCmd := exec.Command("git", "-C", m.repoPath, "branch", "-D", branchName)
	_ = deleteCmd.Run() // Ignore error - branch may have been pushed and deleted elsewhere
}

// cleanupStaleWorktreeForBranch removes any stale worktree reference for the given branch.
// GH-963: When a task fails and Pilot retries, the worktree cleanup may have failed to fully
// remove the reference in .git/worktrees/. This leaves git thinking the branch is still in use
// by another worktree, causing "is already used by worktree" errors on retry.
//
// GH-1017: Enhanced cleanup with additional steps:
// 1. Run git worktree prune -v first to clean up orphaned refs
// 2. Scan /tmp/pilot-worktree-* for orphaned directories
// 3. Delete stale branch refs only if no commits ahead of main
//
// This function is best-effort: errors are ignored since we're just trying to clean up
// stale state before creating a new worktree.
func (m *WorktreeManager) cleanupStaleWorktreeForBranch(ctx context.Context, branchName string) {
	// GH-1017: Run prune first to clean up any orphaned worktree references
	pruneCmd := exec.CommandContext(ctx, "git", "worktree", "prune", "-v")
	pruneCmd.Dir = m.repoPath
	_ = pruneCmd.Run()

	// GH-1017: Scan temp directory for orphaned pilot worktree directories
	// These may exist if Pilot crashed before cleanup
	tmpDir := os.TempDir()
	entries, err := os.ReadDir(tmpDir)
	if err == nil {
		branchSafe := sanitizeBranchName(branchName)
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			// Match pilot-worktree-*-<branchSafe>-* pattern
			if strings.HasPrefix(name, "pilot-worktree-") && strings.Contains(name, branchSafe) {
				orphanPath := filepath.Join(tmpDir, name)
				// Try to remove the worktree via git first
				removeCmd := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", orphanPath)
				removeCmd.Dir = m.repoPath
				_ = removeCmd.Run()
				// Then remove directory
				_ = os.RemoveAll(orphanPath)
			}
		}
	}

	// Get list of all worktrees in porcelain format
	listCmd := exec.CommandContext(ctx, "git", "worktree", "list", "--porcelain")
	listCmd.Dir = m.repoPath
	output, err := listCmd.Output()
	if err != nil {
		return // Ignore - best effort cleanup
	}

	// Parse output to find worktree using this branch
	// Porcelain format:
	//   worktree /path/to/worktree
	//   HEAD abc123def456...
	//   branch refs/heads/pilot/GH-963
	//   <blank line>
	//   worktree /path/to/another
	//   ...
	lines := strings.Split(string(output), "\n")
	var staleWorktreePath string
	targetBranch := "branch refs/heads/" + branchName

	for i, line := range lines {
		if strings.TrimSpace(line) == targetBranch {
			// Found the branch - now find the worktree path (should be a few lines before)
			for j := i - 1; j >= 0; j-- {
				if strings.HasPrefix(lines[j], "worktree ") {
					staleWorktreePath = strings.TrimPrefix(lines[j], "worktree ")
					break
				}
			}
			break
		}
	}

	if staleWorktreePath == "" || staleWorktreePath == m.repoPath {
		// No stale worktree found, or it's the main repo (don't remove that!)
		return
	}

	// Found a stale worktree - remove it
	removeCmd := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", staleWorktreePath)
	removeCmd.Dir = m.repoPath
	_ = removeCmd.Run() // Ignore error - may already be partially removed

	// Belt and suspenders: also remove the directory if it still exists
	_ = os.RemoveAll(staleWorktreePath)

	// GH-1017: Clean up stale branch ref if it has no commits ahead of main
	// Check if branch has commits not in main
	revListCmd := exec.CommandContext(ctx, "git", "rev-list", "--count", "main.."+branchName)
	revListCmd.Dir = m.repoPath
	countOutput, err := revListCmd.Output()
	if err == nil {
		count := strings.TrimSpace(string(countOutput))
		if count == "0" {
			// Branch has no unique commits - safe to delete
			deleteCmd := exec.CommandContext(ctx, "git", "branch", "-D", branchName)
			deleteCmd.Dir = m.repoPath
			_ = deleteCmd.Run()
		}
	}

	// Final prune to clean up any remaining references
	finalPruneCmd := exec.CommandContext(ctx, "git", "worktree", "prune")
	finalPruneCmd.Dir = m.repoPath
	_ = finalPruneCmd.Run()
}

// VerifyRemoteAccess checks that the worktree can access the remote.
// This is useful for pre-flight validation before long-running tasks.
func (m *WorktreeManager) VerifyRemoteAccess(ctx context.Context, worktreePath string) error {
	// Check that 'origin' remote exists and is accessible
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = worktreePath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remote 'origin' not accessible from worktree: %w: %s", err, output)
	}

	// Verify we can ls-remote (lightweight check without fetching)
	lsCmd := exec.CommandContext(ctx, "git", "ls-remote", "--exit-code", "origin", "HEAD")
	lsCmd.Dir = worktreePath
	if lsOutput, lsErr := lsCmd.CombinedOutput(); lsErr != nil {
		return fmt.Errorf("cannot reach remote 'origin': %w: %s", lsErr, lsOutput)
	}

	return nil
}

// CreateWorktree is a standalone helper function for simple use cases.
// Returns the worktree path and a cleanup function.
//
// CRITICAL: The cleanup function MUST be called via defer to ensure cleanup
// even on panic or early return.
//
// Usage:
//
//	worktreePath, cleanup, err := CreateWorktree(ctx, repoPath, taskID)
//	if err != nil {
//	    return err
//	}
//	defer cleanup() // ALWAYS defer cleanup immediately after creation
//
//	// ... use worktreePath for execution ...
func CreateWorktree(ctx context.Context, repoPath, taskID string) (string, func(), error) {
	manager := NewWorktreeManager(repoPath)
	result, err := manager.CreateWorktree(ctx, taskID)
	if err != nil {
		return "", nil, err
	}
	return result.Path, result.Cleanup, nil
}

// CreateWorktreeWithBranch is a standalone helper that creates a worktree with a branch.
// Returns the worktree path and a cleanup function.
//
// Use this when you need to push changes to remote, as it creates a proper branch
// instead of a detached HEAD state.
func CreateWorktreeWithBranch(ctx context.Context, repoPath, taskID, branchName, baseBranch string) (string, func(), error) {
	manager := NewWorktreeManager(repoPath)
	result, err := manager.CreateWorktreeWithBranch(ctx, taskID, branchName, baseBranch)
	if err != nil {
		return "", nil, err
	}
	return result.Path, result.Cleanup, nil
}
