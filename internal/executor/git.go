package executor

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// defaultExcludeDirs are top-level directory prefixes whose contents are never
// auto-staged by the executor's commit step. They represent project-management
// state, build artifacts, or third-party caches that should not appear in
// Pilot-authored commits unless the task explicitly references them.
var defaultExcludeDirs = []string{
	".agent/",
	".claude/",
	"node_modules/",
	"dist/",
	"build/",
	"coverage/",
	".cache/",
}

// defaultExcludeGlobs are base-name patterns whose matching files are never
// auto-staged. Lock files and OS-junk dominate this list.
var defaultExcludeGlobs = []string{
	"*.lock",
	"package-lock.json",
	"pnpm-lock.yaml",
	".DS_Store",
	"Thumbs.db",
}

// ErrNoStageableChanges signals that all dirty paths matched the exclude list —
// the commit step refuses to produce an empty commit rather than committing
// excluded paths.
var ErrNoStageableChanges = fmt.Errorf("no stageable changes after applying default excludes")

// isExcluded returns true if the given porcelain path matches any default
// exclude rule.
func isExcluded(path string) bool {
	for _, dir := range defaultExcludeDirs {
		if strings.HasPrefix(path, dir) {
			return true
		}
	}
	base := filepath.Base(path)
	for _, glob := range defaultExcludeGlobs {
		if matched, _ := filepath.Match(glob, base); matched {
			return true
		}
	}
	return false
}

// GitOperations handles git operations for tasks
type GitOperations struct {
	projectPath string
}

// NewGitOperations creates new git operations for a project
func NewGitOperations(projectPath string) *GitOperations {
	return &GitOperations{projectPath: projectPath}
}

// Commit stages filtered changes and commits. Files matching defaultExcludeDirs
// or defaultExcludeGlobs are never auto-staged. Returns ErrNoStageableChanges
// (wrapped) when all dirty paths are excluded.
func (g *GitOperations) Commit(ctx context.Context, message string) (string, error) {
	// Enumerate dirty paths via NUL-delimited porcelain output.
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain", "-z")
	statusCmd.Dir = g.projectPath
	statusOut, err := statusCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to enumerate dirty paths: %w", err)
	}

	var stage []string
	var skipped []string
	// -z output: NUL-terminated entries, each "XY path" (3-char prefix + path).
	for _, entry := range strings.Split(strings.TrimRight(string(statusOut), "\x00"), "\x00") {
		if len(entry) < 4 {
			continue
		}
		path := entry[3:]
		if isExcluded(path) {
			skipped = append(skipped, path)
			continue
		}
		stage = append(stage, path)
	}

	if len(stage) == 0 {
		return "", fmt.Errorf("%w: skipped paths: %v", ErrNoStageableChanges, skipped)
	}

	// Stage filtered set; -- disambiguates paths from flags.
	addArgs := append([]string{"add", "--"}, stage...)
	addCmd := exec.CommandContext(ctx, "git", addArgs...)
	addCmd.Dir = g.projectPath
	if output, err := addCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to stage changes: %w: %s", err, output)
	}

	// Commit
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", message)
	commitCmd.Dir = g.projectPath
	if output, err := commitCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to commit: %w: %s", err, output)
	}

	// Get commit SHA
	shaCmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	shaCmd.Dir = g.projectPath
	output, err := shaCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get commit SHA: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}
