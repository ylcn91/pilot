package executor

import (
	"fmt"
	"os"
	"path/filepath"
)

// CopyNavigatorToWorktree copies the .agent/ directory from the original repo to the worktree.
// This handles cases where .agent/ contains untracked content (common when .agent/ is gitignored).
//
// GH-936-4: Worktrees only contain tracked files from HEAD. If .agent/ has untracked content
// (like .context-markers/, research notes, or custom SOPs), they won't exist in the worktree.
// This function copies the entire .agent/ directory to ensure Navigator functionality.
//
// Behavior:
// - If .agent/ doesn't exist in source, returns nil (no-op)
// - If .agent/ already exists in worktree (from git), merges untracked content
// - Preserves file permissions during copy
func CopyNavigatorToWorktree(sourceRepo, worktreePath string) error {
	sourceAgent := filepath.Join(sourceRepo, ".agent")
	destAgent := filepath.Join(worktreePath, ".agent")

	// Check if source .agent/ exists
	sourceInfo, err := os.Stat(sourceAgent)
	if err != nil {
		if os.IsNotExist(err) {
			// No .agent/ in source - nothing to copy
			return nil
		}
		return fmt.Errorf("failed to stat source .agent: %w", err)
	}
	if !sourceInfo.IsDir() {
		return nil // .agent is a file, not a directory - skip
	}

	// Copy directory recursively
	return copyDir(sourceAgent, destAgent)
}

// copyDir recursively copies a directory from src to dst.
// If dst exists, files are merged (existing files in dst are overwritten).
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	// Create destination directory with same permissions
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dst, err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", src, err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// copyFile copies a single file from src to dst, preserving permissions.
func copyFile(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	// Read source file
	content, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", src, err)
	}

	// Write to destination with same permissions
	if err := os.WriteFile(dst, content, srcInfo.Mode()); err != nil {
		return fmt.Errorf("failed to write %s: %w", dst, err)
	}

	return nil
}

// EnsureNavigatorInWorktree ensures the worktree has Navigator structure.
// This is the primary function to call after creating a worktree.
//
// Strategy:
// 1. Copy .agent/ from source repo (handles untracked content)
// 2. If .agent/ still doesn't exist, initialize Navigator from templates
//
// The sourceRepo is the original repository path where the user may have
// an existing .agent/ directory with project-specific configuration.
func EnsureNavigatorInWorktree(sourceRepo, worktreePath string) error {
	// First, copy from source to preserve any existing Navigator config
	if err := CopyNavigatorToWorktree(sourceRepo, worktreePath); err != nil {
		return fmt.Errorf("failed to copy navigator to worktree: %w", err)
	}

	// Check if .agent/ now exists in worktree
	agentDir := filepath.Join(worktreePath, ".agent")
	if _, err := os.Stat(agentDir); err == nil {
		// Navigator exists (either from git or from copy)
		return nil
	}

	// No .agent/ exists - will be initialized by runner.maybeInitNavigator()
	// Return nil here to let the normal init flow handle it
	return nil
}
