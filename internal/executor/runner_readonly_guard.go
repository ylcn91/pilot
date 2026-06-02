package executor

import (
	"context"
	"log/slog"
)

// readOnlyHeadBefore captures the current HEAD SHA so a read-only role's commits
// can later be detected and reverted by enforceReadOnly. A missing git layer or
// an unreadable HEAD yields "" — enforceReadOnly treats that as "skip", so the
// guard never blocks the run on a capture failure.
func (r *Runner) readOnlyHeadBefore(s *executeState) string {
	if s.git == nil {
		return ""
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
// role invocation. Violated is true when the role created commits (HEAD moved
// off the captured pre-role SHA) and the guard reverted them. RevertedFrom is
// the post-role HEAD that was unwound (empty when no violation occurred); it is
// recorded for the audit trail.
type readOnlyGuardResult struct {
	Violated     bool
	RevertedFrom string
}

// enforceReadOnly is the single side-effect policy for read-only roles
// (pipeline PLAN, TDD ARCHITECT). Those roles are design-only: they must not
// commit. AllowedTools blocks commits for claude-code, but every other backend
// ignores tool restrictions, so a non-claude planner/architect can still pollute
// the branch. This guard is the backend-agnostic, controller-owned enforcement:
// it compares the current HEAD against the SHA captured before the role ran and,
// if the role created commits, logs a clear warning and soft-reverts to the
// captured SHA so the role's commits are undone while its working-tree changes
// survive (git reset --soft).
//
// headBefore is the SHA captured immediately before the role's Execute call. A
// well-behaved read-only role leaves HEAD unchanged, so the guard is a no-op. An
// empty headBefore (could not capture) or a git that cannot report HEAD makes the
// guard a no-op too — it never aborts the run, it only ever prevents pollution.
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

	if headAfter == headBefore {
		return readOnlyGuardResult{}
	}

	if log != nil {
		log.Warn("read-only "+role+" role committed; reverting to keep the role side-effect-free",
			slog.String("role", role),
			slog.String("head_before", headBefore),
			slog.String("head_after", headAfter),
		)
	}

	if err := git.SoftResetTo(ctx, headBefore); err != nil {
		// Revert failed: the violation still happened and is still flagged, but we
		// could not unwind it. Surface the error loudly; do not abort the run.
		if log != nil {
			log.Error("read-only guard failed to revert stray commits",
				slog.String("role", role),
				slog.String("head_before", headBefore),
				slog.Any("error", err),
			)
		}
		return readOnlyGuardResult{Violated: true, RevertedFrom: headAfter}
	}

	return readOnlyGuardResult{Violated: true, RevertedFrom: headAfter}
}
