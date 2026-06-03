package executor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// SoftResetTo runs `git reset --soft <sha>`, moving the branch tip back to sha
// while leaving the working tree and the index untouched. Any commits created
// after sha are undone, but the file changes they introduced are preserved as
// uncommitted (staged) changes. This is the revert primitive used by the
// read-only role guard: a misbehaving read-only role's commits are unwound, yet
// no work-in-progress is lost.
func (g *GitOperations) SoftResetTo(ctx context.Context, sha string) error {
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return fmt.Errorf("soft reset: empty target sha")
	}
	cmd := exec.CommandContext(ctx, "git", "reset", "--soft", sha)
	cmd.Dir = g.projectPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git reset --soft %s failed: %w: %s", sha, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// HardResetTo runs `git reset --hard <sha>`, moving the branch tip back to sha
// AND discarding every tracked-file change in the working tree and index so the
// tree matches sha exactly. Unlike SoftResetTo it does NOT preserve work — that
// is the point for the read-only guard's pristine restore: a read-only role
// (plan/architect) runs before any legitimate change, so the only tracked-file
// content this can throw away is what that read-only role itself wrote.
// Untracked files survive a hard reset; CleanUntracked removes those.
func (g *GitOperations) HardResetTo(ctx context.Context, sha string) error {
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return fmt.Errorf("hard reset: empty target sha")
	}
	cmd := exec.CommandContext(ctx, "git", "reset", "--hard", sha)
	cmd.Dir = g.projectPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git reset --hard %s failed: %w: %s", sha, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// CleanUntracked runs `git clean -fd`, removing untracked files and directories
// from the working tree. HardResetTo only restores tracked content, so a
// read-only role that wrote a brand-new (never-committed) file leaves it behind
// after a hard reset; this is the second half of the guard's pristine restore.
// It does not touch ignored files (no -x) — only untracked working-tree noise.
func (g *GitOperations) CleanUntracked(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "git", "clean", "-fd")
	cmd.Dir = g.projectPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clean -fd failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// IsDirty reports whether the working tree has any change relative to HEAD —
// modified tracked files OR untracked files — via `git status --porcelain`. The
// read-only guard uses it to detect a role that wrote files WITHOUT committing
// (HEAD unchanged but the tree is dirty), the leak that SHA-only comparison
// misses.
func (g *GitOperations) IsDirty(ctx context.Context) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = g.projectPath
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status --porcelain failed: %w", err)
	}
	return len(strings.TrimSpace(string(output))) > 0, nil
}
