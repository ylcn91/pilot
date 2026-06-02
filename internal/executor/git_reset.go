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
