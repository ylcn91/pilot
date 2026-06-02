package autopilot

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ylcn91/pilot/internal/adapters/github"
)

// handleMerging merges the PR.
func (c *Controller) handleMerging(ctx context.Context, prState *PRState) error {
	prState.MergeAttempts++

	c.log.Info("handleMerging: attempting merge",
		"pr", prState.PRNumber,
		"attempt", prState.MergeAttempts,
		"method", c.config.MergeMethod,
	)

	err := c.autoMerger.MergePR(ctx, prState)
	if err != nil {
		c.log.Error("handleMerging: merge failed",
			"pr", prState.PRNumber,
			"attempt", prState.MergeAttempts,
			"error", err,
		)

		// GH-880: Check if merge failed due to conflict.
		// If so, close PR and clear pilot-in-progress so issue can be retried.
		ghPR, ghErr := c.ghClient.GetPullRequest(ctx, c.owner, c.repo, prState.PRNumber)
		if ghErr == nil && c.isMergeConflict(ghPR) {
			return c.handleMergeConflict(ctx, prState)
		}

		// B5 (TASK-336): Hard cap on non-conflict merge retries. The circuit breaker
		// (MaxFailures) auto-resets after FailureResetTimeout, so without this cap a
		// PR blocked by branch-protection or a stuck status check retries indefinitely.
		// Once MergeAttempts reaches MaxMergeAttempts the failure is terminal and a
		// human must intervene.
		if prState.MergeAttempts >= c.config.MaxMergeAttempts {
			errMsg := fmt.Sprintf("merge failed after %d/%d attempts: %v — manual intervention required",
				prState.MergeAttempts, c.config.MaxMergeAttempts, err)
			c.log.Error("handleMerging: merge attempt cap reached — escalating to StageFailed",
				"pr", prState.PRNumber,
				"attempts", prState.MergeAttempts,
				"max", c.config.MaxMergeAttempts,
				"error", err,
			)
			if prState.IssueNumber > 0 {
				comment := fmt.Sprintf(
					"⚠️ **Merge escalation**: PR #%d failed to merge after %d attempts.\n\nLast error: `%v`\n\nManual intervention is required — no further automatic retries will be made.",
					prState.PRNumber, prState.MergeAttempts, err)
				if _, cerr := c.ghClient.AddComment(ctx, c.owner, c.repo, prState.IssueNumber, comment); cerr != nil {
					c.log.Warn("failed to post merge escalation comment", "issue", prState.IssueNumber, "error", cerr)
				}
			}
			prState.Stage = StageFailed
			prState.Error = errMsg
			c.metrics.RecordPRFailed()
			c.metrics.RecordIssueProcessed("failed")
			return nil
		}

		return fmt.Errorf("merge attempt %d failed: %w", prState.MergeAttempts, err)
	}

	c.log.Info("PR merged successfully", "pr", prState.PRNumber)
	prState.Stage = StageMerged
	c.recordMergeSuccess(prState)

	// GH-1015: Add pilot-done label after successful merge (not at PR creation)
	// This prevents false positives where PRs are closed without merging
	if prState.IssueNumber > 0 {
		// GH-3271: mark issue processed in all pollers before any label updates so
		// a poll tick that fires during the merge→pilot-done propagation window
		// cannot re-dispatch the issue (phantom pilot-blocked).
		if c.onIssueDone != nil {
			c.onIssueDone(prState.IssueNumber)
		}
		if err := c.ghClient.AddLabels(ctx, c.owner, c.repo, prState.IssueNumber, []string{github.LabelDone}); err != nil {
			c.log.Warn("failed to add pilot-done label after merge", "issue", prState.IssueNumber, "error", err)
		}
		if err := c.ghClient.RemoveLabel(ctx, c.owner, c.repo, prState.IssueNumber, github.LabelInProgress); err != nil {
			c.log.Warn("failed to remove pilot-in-progress label after merge", "issue", prState.IssueNumber, "error", err)
		}
		// GH-1302: Clean up stale pilot-failed label from prior failed attempt
		if err := c.ghClient.RemoveLabel(ctx, c.owner, c.repo, prState.IssueNumber, github.LabelFailed); err != nil {
			// 404 is expected if label doesn't exist - silently ignore
			c.log.Debug("pilot-failed label cleanup", "issue", prState.IssueNumber, "error", err)
		}
		// Close the issue after successful merge
		if err := c.ghClient.UpdateIssueState(ctx, c.owner, c.repo, prState.IssueNumber, "closed"); err != nil {
			c.log.Warn("failed to close issue after merge", "issue", prState.IssueNumber, "error", err)
		}
		c.log.Info("closed issue after merge", "issue", prState.IssueNumber, "pr", prState.PRNumber)

		// GH-2297: Post success comment so last comment isn't stale failure.
		// GH-2345: Guard against re-entry producing duplicate comments.
		if !prState.MergeNotificationPosted {
			comment := buildMergeCompletionComment(prState)
			if _, err := c.ghClient.AddComment(ctx, c.owner, c.repo, prState.IssueNumber, comment); err != nil {
				c.log.Warn("failed to post merge completion comment", "issue", prState.IssueNumber, "error", err)
			} else {
				prState.MergeNotificationPosted = true
			}
		}

		// GH-1336: Sync monitor state so dashboard shows "done" instead of stale "failed"
		if c.monitor != nil {
			taskID := fmt.Sprintf("GH-%d", prState.IssueNumber)
			c.monitor.Complete(taskID, prState.PRURL)
			c.log.Debug("updated monitor state to completed", "task", taskID, "pr", prState.PRNumber)
		}

		// GH-2279/GH-2402 + TASK-352: Self-heal execution records on merge.
		// Promotes prior "failed" rows (for the issue AND its parent epic) to
		// "completed" and stamps the PR URL so the dashboard reflects the merged
		// outcome (handles user-pushed commits, sub-issues merged via parent, etc.).
		c.selfHealForPR(ctx, prState.IssueNumber, prState.PRURL)

		// A4: Reinforce the project's patterns on merge — the strongest success
		// signal available (work shipped and passed review/CI). Guarded so each
		// merge reinforces exactly once.
		c.reinforceMergedPatterns(prState)

		// GH-1870: Sync board card to "Done" column on merge
		if c.boardSync != nil && prState.IssueNodeID != "" {
			if err := c.boardSync.UpdateProjectItemStatus(ctx, prState.IssueNodeID, c.doneStatus); err != nil {
				c.log.Warn("board sync on merge failed", "pr", prState.PRNumber, "error", err)
			}
		}
	}

	// GH-1383: Delete remote branch after successful merge
	// Branch is safe to delete — it's fully merged. If GitHub already deleted it
	// (delete_branch_on_merge setting), the API returns 404/422 which we ignore.
	if prState.BranchName != "" {
		if err := c.ghClient.DeleteBranch(ctx, c.owner, c.repo, prState.BranchName); err != nil {
			c.log.Warn("failed to delete branch after merge", "branch", prState.BranchName, "pr", prState.PRNumber, "error", err)
		} else {
			c.log.Info("deleted branch after merge", "branch", prState.BranchName, "pr", prState.PRNumber)
		}
	}

	// Notify merge success
	if c.notifier != nil {
		if err := c.notifier.NotifyMerged(ctx, prState); err != nil {
			c.log.Warn("failed to send merge notification", "error", err)
		}
	}

	return nil
}

// maybeCloseParentIssue checks whether the merged PR's issue is a sub-issue
// and, if all sibling sub-issues are also closed, closes the parent issue.
// All errors are logged as warnings without blocking the merge flow.
func (c *Controller) maybeCloseParentIssue(ctx context.Context, prState *PRState) {
	if prState.IssueNumber == 0 {
		return
	}

	// Fetch the sub-issue body to find parent reference.
	issue, err := c.ghClient.GetIssue(ctx, c.owner, c.repo, prState.IssueNumber)
	if err != nil {
		c.log.Warn("maybeCloseParentIssue: failed to fetch issue", slog.Int("issue", prState.IssueNumber), slog.Any("error", err))
		return
	}

	parentNum := github.ParseParentIssueNumber(issue.Body)
	if parentNum == 0 {
		return
	}

	// Check how many sibling sub-issues are still open.
	// Tier 1: try native GitHub sub-issues GraphQL API (more reliable, works even without text patterns).
	// Tier 2: fall back to text search when native links are absent (legacy repos use body "Parent: GH-N" only).
	openCount, hasNativeLinks, err := c.ghClient.GetOpenSubIssueCount(ctx, c.owner, c.repo, parentNum)
	if err != nil || !hasNativeLinks {
		if err != nil {
			c.log.Warn("maybeCloseParentIssue: native sub-issue count failed, falling back to search", slog.Int("parent", parentNum), slog.Any("error", err))
		} else {
			c.log.Debug("maybeCloseParentIssue: no native sub-issue links, falling back to search", slog.Int("parent", parentNum))
		}
		openCount, err = c.ghClient.SearchOpenSubIssues(ctx, c.owner, c.repo, parentNum)
		if err != nil {
			c.log.Warn("maybeCloseParentIssue: failed to search open sub-issues", slog.Int("parent", parentNum), slog.Any("error", err))
			return
		}
	}

	if openCount > 0 {
		c.log.Info("maybeCloseParentIssue: siblings still open", slog.Int("parent", parentNum), slog.Int("open", openCount))
		return
	}

	c.closeParentNow(ctx, parentNum)
}

// closeParentNow adds pilot-done, removes stale labels, posts a summary comment,
// and closes the parent issue. All errors are logged as warnings without propagating.
func (c *Controller) closeParentNow(ctx context.Context, parentNum int) {
	c.log.Info("closeParentNow: all sub-issues done, closing parent", slog.Int("parent", parentNum))

	// Label cleanup: add pilot-done, remove stale labels.
	if err := c.ghClient.AddLabels(ctx, c.owner, c.repo, parentNum, []string{"pilot-done"}); err != nil {
		c.log.Warn("closeParentNow: failed to add pilot-done label", slog.Int("parent", parentNum), slog.Any("error", err))
	}
	for _, stale := range []string{"pilot-failed", "pilot-in-progress", "pilot-blocked"} {
		if err := c.ghClient.RemoveLabel(ctx, c.owner, c.repo, parentNum, stale); err != nil {
			c.log.Warn("closeParentNow: failed to remove label", slog.String("label", stale), slog.Int("parent", parentNum), slog.Any("error", err))
		}
	}

	// Post summary comment.
	comment := fmt.Sprintf("All sub-issues for GH-%d are complete. Closing parent issue automatically.", parentNum)
	if _, err := c.ghClient.AddComment(ctx, c.owner, c.repo, parentNum, comment); err != nil {
		c.log.Warn("closeParentNow: failed to post comment", slog.Int("parent", parentNum), slog.Any("error", err))
	}

	// Close the parent issue.
	if err := c.ghClient.UpdateIssueState(ctx, c.owner, c.repo, parentNum, "closed"); err != nil {
		c.log.Warn("closeParentNow: failed to close parent issue", slog.Int("parent", parentNum), slog.Any("error", err))
	}
}

// recoverStaleParentIssues scans open pilot parent issues at startup and closes any
// whose sub-issues are all done. Catches parents orphaned when the daemon was down.
func (c *Controller) recoverStaleParentIssues(ctx context.Context) {
	const maxRecover = 50

	candidates, err := c.ghClient.SearchOpenPilotIssuesWithSubIssues(ctx, c.owner, c.repo, maxRecover)
	if err != nil {
		c.log.Warn("recoverStaleParentIssues: search failed", slog.Any("error", err))
		return
	}

	if len(candidates) == maxRecover {
		c.log.Info("recoverStaleParentIssues: hit limit, some candidates may be skipped", slog.Int("limit", maxRecover))
	}

	closed := 0
	for _, parentNum := range candidates {
		openCount, _, err := c.ghClient.GetOpenSubIssueCount(ctx, c.owner, c.repo, parentNum)
		if err != nil {
			c.log.Warn("recoverStaleParentIssues: failed to count sub-issues", slog.Int("parent", parentNum), slog.Any("error", err))
			continue
		}
		if openCount > 0 {
			continue
		}
		c.closeParentNow(ctx, parentNum)
		closed++
	}

	c.log.Info("recoverStaleParentIssues: done", slog.Int("closed", closed))
}

// Start runs one-time startup recovery sweeps. Call before the main Run loop.
func (c *Controller) Start(ctx context.Context) {
	c.recoverStaleParentIssues(ctx)
}

// isMergeConflict returns true if the PR has merge conflicts.
// GitHub's mergeable field is computed asynchronously, so:
//   - nil means GitHub hasn't computed it yet (not a conflict)
//   - false means conflicts exist
//   - true means no conflicts
//
// We also check mergeable_state for "dirty" which explicitly means conflicts.
func (c *Controller) isMergeConflict(pr *github.PullRequest) bool {
	// Check mergeable_state first (more specific)
	if pr.MergeableState == "dirty" {
		return true
	}
	// Fallback to mergeable bool
	if pr.Mergeable != nil && !*pr.Mergeable {
		return true
	}
	return false
}

// handleMergeConflict tries to auto-rebase the PR branch first. If that fails,
// falls back to closing the PR and returning the issue to the queue.
// GH-1796: Saves ~$8-15 per run by avoiding full re-execution for trivial conflicts.
func (c *Controller) handleMergeConflict(ctx context.Context, prState *PRState) error {
	c.log.Warn("merge conflict detected",
		"pr", prState.PRNumber,
		"issue", prState.IssueNumber,
		"branch", prState.BranchName,
	)

	// Try GitHub auto-update first (merge-from-base, not true rebase)
	err := c.ghClient.UpdatePullRequestBranch(ctx, c.owner, c.repo, prState.PRNumber)
	if err == nil {
		c.log.Info("auto-rebased conflicting PR", "pr", prState.PRNumber)
		prState.Stage = StageWaitingCI // rebase triggers new CI
		prState.HeadSHA = ""           // force refresh on next tick
		return nil
	}
	c.log.Warn("auto-rebase failed, closing PR for retry", "pr", prState.PRNumber, "error", err)

	// Add comment explaining the closure
	comment := "Merge conflict detected. Auto-rebase failed — closing PR so the issue can be re-executed from updated main."
	if _, err := c.ghClient.AddPRComment(ctx, c.owner, c.repo, prState.PRNumber, comment); err != nil {
		c.log.Warn("failed to comment on conflicting PR", "pr", prState.PRNumber, "error", err)
	}

	// Close the PR
	if err := c.ghClient.ClosePullRequest(ctx, c.owner, c.repo, prState.PRNumber); err != nil {
		c.log.Warn("failed to close conflicting PR", "pr", prState.PRNumber, "error", err)
	}

	// Restore issue to dispatch-ready state after conflict.
	// GH-3139/TASK-301: issue must remain OPEN with pilot label so the poller
	// can re-dispatch. Do NOT close the issue or add pilot-done here.
	if prState.IssueNumber > 0 {
		if err := c.ghClient.RemoveLabel(ctx, c.owner, c.repo, prState.IssueNumber, github.LabelInProgress); err != nil {
			c.log.Warn("failed to remove in-progress label", "issue", prState.IssueNumber, "error", err)
		}
		// Re-add pilot label so poller can pick up the issue on the next cycle.
		if err := c.ghClient.AddLabels(ctx, c.owner, c.repo, prState.IssueNumber, []string{github.LabelPilot}); err != nil {
			c.log.Warn("failed to re-add pilot label on conflict", "issue", prState.IssueNumber, "error", err)
		}
		// Guard: remove pilot-done if somehow present — prevents ghost-close.
		if err := c.ghClient.RemoveLabel(ctx, c.owner, c.repo, prState.IssueNumber, github.LabelDone); err != nil {
			c.log.Debug("pilot-done cleanup on conflict (may not exist)", "issue", prState.IssueNumber, "error", err)
		}
	}

	prState.Stage = StageFailed
	prState.Error = "merge conflict with base branch"
	return nil
}
