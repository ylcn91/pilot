package autopilot

import (
	"context"

	"github.com/ylcn91/pilot/internal/adapters/github"
)

// checkExternalMergeOrClose checks if a PR was merged or closed externally (by human).
// Returns true if the PR was removed from tracking, false otherwise.
// Accepts cached ghPR to avoid redundant API calls.
func (c *Controller) checkExternalMergeOrClose(ctx context.Context, prState *PRState, ghPR *github.PullRequest) bool {

	// Check if PR was merged externally
	if ghPR.Merged {
		c.log.Info("PR merged externally", "pr", prState.PRNumber)
		c.notifyExternalMerge(ctx, prState)

		// GH-1486: Close associated issue and add pilot-done label on external merge
		if prState.IssueNumber > 0 {
			closed := c.applyMergeLabels(ctx, prState.IssueNumber, mergeLabelLogging{
				addDoneFailMsg:     "failed to add pilot-done label after external merge",
				inProgressFailWarn: false,
				inProgressFailMsg:  "pilot-in-progress label cleanup on external merge",
				failedCleanupMsg:   "pilot-failed label cleanup on external merge",
				closeFailMsg:       "failed to close issue after external merge",
			})
			if closed {
				c.log.Info("closed issue after external merge", "issue", prState.IssueNumber, "pr", prState.PRNumber)

				// GH-2297: Post success comment so last comment isn't stale failure
				comment := buildMergeCompletionComment(prState)
				if _, err := c.ghClient.AddComment(ctx, c.owner, c.repo, prState.IssueNumber, comment); err != nil {
					c.log.Warn("failed to post merge completion comment on external merge", "issue", prState.IssueNumber, "error", err)
				}
			}
		}

		// GH-411: Trigger release for externally merged PRs if auto-release is enabled
		if c.shouldTriggerRelease() && prState.Stage != StageReleasing {
			c.log.Info("triggering release for externally merged PR", "pr", prState.PRNumber)
			// Update SHA to merge commit if available
			if ghPR.MergeCommitSHA != "" {
				prState.HeadSHA = ghPR.MergeCommitSHA
			}
			prState.Stage = StageReleasing
			c.persistPRState(prState)
			return false // Continue processing to handle release
		}

		c.removePR(prState.PRNumber)
		return true
	}

	// Check if PR was closed (without merge) externally
	if ghPR.State == "closed" {
		c.log.Info("PR closed externally, removing from tracking", "pr", prState.PRNumber)
		c.notifyExternalClose(ctx, prState)
		c.removePR(prState.PRNumber)
		return true
	}

	return false
}

// notifyExternalMerge sends notification when a PR is merged externally.
func (c *Controller) notifyExternalMerge(ctx context.Context, prState *PRState) {
	if c.notifier == nil {
		return
	}

	// Reuse the existing NotifyMerged notification
	if err := c.notifier.NotifyMerged(ctx, prState); err != nil {
		c.log.Warn("failed to send external merge notification", "pr", prState.PRNumber, "error", err)
	}
}

// notifyExternalClose sends notification when a PR is closed externally without merge.
// GH-1015: Marks the issue as pilot-retry-ready so it can be re-picked by the poller.
func (c *Controller) notifyExternalClose(ctx context.Context, prState *PRState) {
	c.log.Info("PR closed externally without merge", "pr", prState.PRNumber, "issue", prState.IssueNumber)

	// GH-1015: Add pilot-retry-ready label so the issue can be retried
	// Remove pilot-in-progress to allow the poller to re-pick it
	if prState.IssueNumber > 0 {
		// GH-2340: Skip pilot-retry-ready when the issue already carries
		// pilot-done. This happens when Pilot itself closed a duplicate PR
		// (e.g. via handleMergeConflict) after the original PR was already
		// merged. Adding pilot-retry-ready in that case strands the label
		// on a closed/done issue forever (poller skips non-open issues).
		issue, err := c.ghClient.GetIssue(ctx, c.owner, c.repo, prState.IssueNumber)
		if err != nil {
			c.log.Warn("failed to fetch issue for label check", "issue", prState.IssueNumber, "error", err)
		} else if github.HasLabel(issue, github.LabelDone) {
			c.log.Info("skipping pilot-retry-ready: issue already pilot-done", "issue", prState.IssueNumber, "pr", prState.PRNumber)
			c.maybeCloseParentIssue(ctx, prState)
			return
		}

		if err := c.ghClient.AddLabels(ctx, c.owner, c.repo, prState.IssueNumber, []string{github.LabelRetryReady}); err != nil {
			c.log.Warn("failed to add pilot-retry-ready label", "issue", prState.IssueNumber, "error", err)
		}
		if err := c.ghClient.RemoveLabel(ctx, c.owner, c.repo, prState.IssueNumber, github.LabelInProgress); err != nil {
			c.log.Warn("failed to remove pilot-in-progress label", "issue", prState.IssueNumber, "error", err)
		}
		// Remove stale pilot-failed label (GH-1302 gap)
		if err := c.ghClient.RemoveLabel(ctx, c.owner, c.repo, prState.IssueNumber, github.LabelFailed); err != nil {
			c.log.Debug("failed to remove pilot-failed (may not exist)", "issue", prState.IssueNumber, "error", err)
		}
		c.log.Info("marked issue as pilot-retry-ready (PR closed without merge)", "issue", prState.IssueNumber, "pr", prState.PRNumber)
	}

	// GH-2198: Close parent epic when all sub-issues are done (even if this one
	// was closed without merge). maybeCloseParentIssue no-ops for non-sub-issues.
	c.maybeCloseParentIssue(ctx, prState)
}
