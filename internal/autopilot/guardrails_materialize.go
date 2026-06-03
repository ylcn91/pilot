package autopilot

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// materializeHead resolves the content source the rules read for this run.
// Git work trees are copied from the PR head SHA into a tempdir; plain fixture
// directories are used directly.
func (g *GuardrailsGate) materializeHead(ctx context.Context, headSHA string, changed []string) (worktreePath string, cleanup func(), ok bool) {
	noop := func() {}
	if g.repoPath == "" || !isGitWorkTree(g.repoPath) {
		return g.repoPath, noop, true
	}
	if err := g.ensureHead(ctx, headSHA); err != nil {
		g.log.Warn("guardrails: cannot resolve PR head, skipping", "sha", ShortSHA(headSHA), "error", err)
		return "", noop, false
	}
	tmp, err := os.MkdirTemp("", "pilot-guardrails-*")
	if err != nil {
		g.log.Warn("guardrails: cannot create temp worktree, skipping", "error", err)
		return "", noop, false
	}
	cleanup = func() {
		if rmErr := os.RemoveAll(tmp); rmErr != nil {
			g.log.Warn("guardrails: failed to remove temp worktree", "dir", tmp, "error", rmErr)
		}
	}
	for _, rel := range changed {
		if err := ctx.Err(); err != nil {
			cleanup()
			g.log.Warn("guardrails: context cancelled while materializing head, skipping", "error", err)
			return "", noop, false
		}
		content, showErr := g.showHeadFile(ctx, headSHA, rel)
		if showErr != nil {
			continue
		}
		if writeErr := writeMaterializedFile(tmp, rel, content); writeErr != nil {
			cleanup()
			g.log.Warn("guardrails: cannot write materialized file, skipping", "file", rel, "error", writeErr)
			return "", noop, false
		}
	}
	return tmp, cleanup, true
}

func (g *GuardrailsGate) ensureHead(ctx context.Context, headSHA string) error {
	if g.headPresent(ctx, headSHA) {
		return nil
	}
	fetch := exec.CommandContext(ctx, "git", "-C", g.repoPath, "fetch", "--quiet", "origin", headSHA)
	fetchOut, fetchErr := fetch.CombinedOutput()
	if g.headPresent(ctx, headSHA) {
		return nil
	}
	if fetchErr != nil {
		return fmt.Errorf("git fetch %s: %w: %s", ShortSHA(headSHA), fetchErr, strings.TrimSpace(string(fetchOut)))
	}
	return fmt.Errorf("commit %s not present after fetch", ShortSHA(headSHA))
}

func (g *GuardrailsGate) headPresent(ctx context.Context, headSHA string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", g.repoPath, "cat-file", "-e", headSHA+"^{commit}")
	return cmd.Run() == nil
}

func (g *GuardrailsGate) showHeadFile(ctx context.Context, headSHA, rel string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", g.repoPath, "show", headSHA+":"+filepath.ToSlash(rel))
	return cmd.Output()
}

func writeMaterializedFile(dir, rel string, content []byte) error {
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, content, 0o644)
}

func isGitWorkTree(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
