package executor

import (
	"context"
	"log/slog"
)

// readOnlyHeadBefore captures the current HEAD SHA so a read-only role's traces
// can later be detected and reverted by enforceReadOnly. A missing git layer or
// an unreadable HEAD yields "" — enforceReadOnly treats that as "skip", so the
// guard never blocks the run on a capture failure.
//
// The PLAN guard fires from executePipelinePlan, which runs BEFORE executePrepare
// assigns s.git. Without lazy init the guard would be silently inert on the
// pilot PLAN path (a nil git => "" => no enforcement). We initialize s.git from
// s.executionPath here so the guard is active; executePrepare later recreates the
// same NewGitOperations(s.executionPath), so this is idempotent.
func (r *Runner) readOnlyHeadBefore(s *executeState) string {
	if s.git == nil {
		if s.executionPath == "" {
			return ""
		}
		s.git = NewGitOperations(s.executionPath)
	}
	sha, err := s.git.GetCurrentCommitSHA(s.ctx)
	if err != nil {
		r.log.Warn("read-only guard could not capture pre-role HEAD; enforcement disabled for this role",
			slog.Any("error", err))
		return ""
	}
	return sha
}

// readOnlyGuardResult reports the outcome of enforceReadOnly for one read-only
// role invocation. Violated is true when the role left ANY trace — a moved HEAD
// (committed) OR a dirty working tree (uncommitted writes / untracked files) —
// and the guard restored the worktree to its pristine pre-role state.
// RevertedFrom is the post-role HEAD that was unwound when the role committed
// (empty when the trace was an uncommitted write that left HEAD in place); it is
// recorded for the audit trail.
type readOnlyGuardResult struct {
	Violated     bool
	RevertedFrom string
}

// enforceReadOnly is the single side-effect policy for read-only roles
// (pipeline PLAN, TDD ARCHITECT). Those roles are design-only: they must not
// modify the worktree at all — no commit, no file write. AllowedTools blocks
// writes for claude-code, but every other backend ignores tool restrictions, so
// a non-claude planner/architect can still pollute the branch. This guard is the
// backend-agnostic, controller-owned enforcement.
//
// A read-only role runs BEFORE any legitimate change, so the only trace it can
// leave is what it wrote itself. The guard therefore restores the worktree to a
// PRISTINE headBefore state whenever the role left ANY trace:
//   - committed: HEAD moved off headBefore, OR
//   - dirty: `git status --porcelain` non-empty (modified tracked files OR
//     untracked files) even with HEAD unchanged.
//
// Restore is `git reset --hard headBefore` (drops committed + tracked-file
// traces) followed by `git clean -fd` (drops untracked files the hard reset
// leaves behind). This closes both leaks the old SHA-only soft-reset guard
// missed: (1) an uncommitted write leaving HEAD unchanged, and (2) a committed
// write whose files --soft preserved.
//
// headBefore is the SHA captured immediately before the role's Execute call. A
// well-behaved read-only role leaves HEAD unchanged AND a clean tree, so the
// guard is a no-op. An empty headBefore (could not capture) or a git that cannot
// report HEAD makes the guard a no-op too — it never aborts the run, it only
// ever prevents pollution.
func enforceReadOnly(ctx context.Context, git *GitOperations, headBefore, role string, log *slog.Logger) readOnlyGuardResult {
	if git == nil || headBefore == "" {
		return readOnlyGuardResult{}
	}

	headAfter, err := git.GetCurrentCommitSHA(ctx)
	if err != nil {
		if log != nil {
			log.Warn("read-only guard could not read HEAD; skipping enforcement",
				slog.String("role", role), slog.Any("error", err))
		}
		return readOnlyGuardResult{}
	}

	headMoved := headAfter != headBefore

	dirty, err := git.IsDirty(ctx)
	if err != nil {
		if log != nil {
			log.Warn("read-only guard could not read working-tree status; skipping enforcement",
				slog.String("role", role), slog.Any("error", err))
		}
		return readOnlyGuardResult{}
	}

	// No trace: HEAD unchanged and a clean tree. The role behaved.
	if !headMoved && !dirty {
		return readOnlyGuardResult{}
	}

	revertedFrom := ""
	if headMoved {
		revertedFrom = headAfter
	}

	if log != nil {
		log.Warn("read-only "+role+" role left a trace; restoring pristine worktree to keep the role side-effect-free",
			slog.String("role", role),
			slog.String("head_before", headBefore),
			slog.String("head_after", headAfter),
			slog.Bool("head_moved", headMoved),
			slog.Bool("dirty", dirty),
		)
	}

	if err := git.HardResetTo(ctx, headBefore); err != nil {
		if log != nil {
			log.Error("read-only guard failed to hard-reset stray changes",
				slog.String("role", role),
				slog.String("head_before", headBefore),
				slog.Any("error", err),
			)
		}
		return readOnlyGuardResult{Violated: true, RevertedFrom: revertedFrom}
	}

	// Hard reset restores tracked content only; untracked files the role created
	// survive it. Remove them so the worktree is truly pristine.
	if err := git.CleanUntracked(ctx); err != nil {
		if log != nil {
			log.Error("read-only guard failed to clean untracked files",
				slog.String("role", role),
				slog.Any("error", err),
			)
		}
		return readOnlyGuardResult{Violated: true, RevertedFrom: revertedFrom}
	}

	return readOnlyGuardResult{Violated: true, RevertedFrom: revertedFrom}
}
