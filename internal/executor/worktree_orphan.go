package executor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CleanupOrphanedWorktrees scans for orphaned pilot worktree directories and removes them.
// This handles cases where Pilot was killed (OOM/SIGKILL) before deferred cleanup could run,
// leaving stale worktrees in /tmp/ that compound memory pressure on subsequent retries.
//
// GH-962: During normal operation, worktrees are cleaned up by deferred functions.
// GH-2168: OOM-killed processes (exit 137) never run defers, so worktrees persist.
// Heavy JS projects with node_modules (800MB+ per worktree) can exhaust memory fast.
//
// Strategy:
// 1. Scan /tmp/ for directories matching "pilot-worktree-*" pattern (skip pool worktrees)
// 2. For broken worktrees (.git missing or gitdir broken): remove directly
// 3. For valid worktrees connected to our repo: also remove — at startup, all are stale
// 4. For worktrees connected to other repos: skip (may belong to another Pilot instance)
// 5. Run `git worktree prune` to clean up stale references in .git/worktrees/
//
// This function is safe to call at startup. Pool worktrees (pilot-worktree-pool-*)
// are excluded because they are managed by the WorktreeManager lifecycle.
func CleanupOrphanedWorktrees(ctx context.Context, repoPath string) error {
	tmpDir := os.TempDir()
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return fmt.Errorf("failed to read temp directory %s: %w", tmpDir, err)
	}

	// Resolve symlinks in repoPath for reliable comparison with gitdir values.
	// On macOS, os.TempDir() returns /var/folders/... but git resolves to /private/var/folders/...
	resolvedRepoPath := repoPath
	if resolved, err := filepath.EvalSymlinks(repoPath); err == nil {
		resolvedRepoPath = resolved
	}

	orphanCount := 0
	var totalBytes int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if this looks like a pilot worktree
		name := entry.Name()
		if !strings.HasPrefix(name, "pilot-worktree-") {
			continue
		}

		// Skip pool worktrees — they are managed by WorktreeManager.Close()
		if strings.HasPrefix(name, "pilot-worktree-pool-") {
			continue
		}

		worktreePath := filepath.Join(tmpDir, name)

		// Measure size before removal for logging
		dirSize := dirSizeBytes(worktreePath)

		// Check if this is still a valid worktree by checking if .git file exists
		gitFile := filepath.Join(worktreePath, ".git")
		if _, err := os.Stat(gitFile); err != nil {
			// .git file doesn't exist - this is an orphaned directory
			slog.Warn("Removing orphaned pilot worktree (no .git)",
				slog.String("path", worktreePath),
				slog.String("size_mb", fmt.Sprintf("%.1f", float64(dirSize)/(1024*1024))),
			)
			if removeErr := os.RemoveAll(worktreePath); removeErr == nil {
				orphanCount++
				totalBytes += dirSize
			}
			continue
		}

		// Directory has .git file - check if it's actually connected to our repo
		gitContent, err := os.ReadFile(gitFile)
		if err != nil {
			continue
		}

		// .git file contains: "gitdir: /path/to/repo/.git/worktrees/name"
		gitdirLine := strings.TrimSpace(string(gitContent))
		if !strings.HasPrefix(gitdirLine, "gitdir: ") {
			continue
		}

		gitdir := strings.TrimPrefix(gitdirLine, "gitdir: ")

		// Check if the gitdir points to our repository's worktree area.
		// Resolve gitdir symlinks too for consistent comparison.
		resolvedGitdir := gitdir
		if resolved, err := filepath.EvalSymlinks(filepath.Dir(gitdir)); err == nil {
			resolvedGitdir = filepath.Join(resolved, filepath.Base(gitdir))
		}
		expectedPrefix := filepath.Join(resolvedRepoPath, ".git", "worktrees")
		if !strings.HasPrefix(resolvedGitdir, expectedPrefix) {
			// Belongs to a different repo — don't touch it
			continue
		}

		// GH-2168: This worktree belongs to our repo. At startup, any existing
		// pilot worktree is stale — the previous process was killed before cleanup.
		// Use git worktree remove --force for proper cleanup, then RemoveAll as fallback.
		slog.Warn("Removing stale pilot worktree from previous execution",
			slog.String("path", worktreePath),
			slog.String("size_mb", fmt.Sprintf("%.1f", float64(dirSize)/(1024*1024))),
		)

		removeCmd := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", worktreePath)
		removeCmd.Dir = repoPath
		_ = removeCmd.Run()

		// Belt and suspenders: also remove the directory if git worktree remove didn't fully clean up
		_ = os.RemoveAll(worktreePath)
		orphanCount++
		totalBytes += dirSize
	}

	// Run git worktree prune to clean up any stale references in .git/worktrees/
	// This removes references to worktrees that no longer exist on disk
	if repoPath != "" {
		pruneCmd := exec.CommandContext(ctx, "git", "worktree", "prune", "-v")
		pruneCmd.Dir = repoPath
		// Ignore errors - prune is best-effort cleanup
		_ = pruneCmd.Run()
	}

	if orphanCount > 0 {
		slog.Warn("Stale worktree cleanup complete",
			slog.Int("removed", orphanCount),
			slog.String("freed_mb", fmt.Sprintf("%.1f", float64(totalBytes)/(1024*1024))),
		)
		return fmt.Errorf("cleaned up %d orphaned pilot worktree directories (freed %.1f MB)", orphanCount, float64(totalBytes)/(1024*1024))
	}

	return nil
}

// dirSizeBytes returns the total size of a directory tree in bytes.
// Returns 0 on any error — used for best-effort logging only.
func dirSizeBytes(path string) int64 {
	var size int64
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors, best-effort
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				size += info.Size()
			}
		}
		return nil
	})
	return size
}
