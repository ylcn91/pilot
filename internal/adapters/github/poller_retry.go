package github

import (
	"context"
	"fmt"
	"log/slog"
)

// handlePreFlightReject handles an issue rejected by the pre-flight judge (GH-2802):
//   - adds pilot-needs-clarification label so the issue is filtered on subsequent polls
//   - posts a comment explaining the decision and how to re-trigger
//   - saves a declined-preflight execution record if an ExecutionSaver is wired
//
// The caller must NOT call markProcessed after this so that label removal re-triggers dispatch.
func (p *Poller) handlePreFlightReject(ctx context.Context, issue *Issue, verdict Verdict) {
	taskID := fmt.Sprintf("GH-%d", issue.Number)

	if err := p.client.AddLabels(ctx, p.owner, p.repo, issue.Number, []string{LabelNeedsClarification}); err != nil {
		p.logger.Warn("pre-flight: failed to add needs-clarification label",
			slog.Int("issue", issue.Number),
			slog.Any("error", err))
	}

	comment := fmt.Sprintf(
		"**Pre-flight check declined this issue.**\n\n"+
			"**Decision:** `%s`\n"+
			"**Reason:** %s\n"+
			"**Confidence:** %.0f%%\n\n"+
			"To re-trigger: edit the issue to address the above, then remove the `%s` label.",
		verdict.Decision, verdict.Reason, verdict.Confidence*100, LabelNeedsClarification,
	)
	if _, err := p.client.AddComment(ctx, p.owner, p.repo, issue.Number, comment); err != nil {
		p.logger.Warn("pre-flight: failed to post rejection comment",
			slog.Int("issue", issue.Number),
			slog.Any("error", err))
	}

	if p.dispatch.execSaver != nil {
		if err := p.dispatch.execSaver.SaveDeclinedExecution(taskID, p.dispatch.projectPath, "declined-preflight", verdict.Reason); err != nil {
			p.logger.Warn("pre-flight: failed to save execution record",
				slog.Int("issue", issue.Number),
				slog.Any("error", err))
		}
	}

	// #17: move the board card out of In Progress so a rejected issue doesn't
	// orphan there. Best-effort; no-op unless a blocked status is configured.
	p.syncBoardStatusBlocked(ctx, issue)
}

// hasMergedWork checks if the issue already has merged PRs (e.g. "GH-123" in title).
// If merged work exists, the issue is marked as done and should be skipped.
func (p *Poller) hasMergedWork(ctx context.Context, issue *Issue) bool {
	found, err := p.client.SearchMergedPRsForIssue(ctx, p.owner, p.repo, issue.Number)
	if err != nil {
		p.logger.Warn("Failed to check for merged PRs",
			slog.Int("issue", issue.Number),
			slog.Any("error", err),
		)
		// Fall through to branch lookup below — don't block on Search API errors alone.
	}

	// GH-2341: Search API has up to ~30s indexing lag. Supplement with a direct REST
	// lookup by branch (strongly consistent) to catch just-merged PRs on pilot/GH-N.
	if !found {
		branch := fmt.Sprintf("pilot/GH-%d", issue.Number)
		branchFound, berr := p.client.FindMergedPRByBranch(ctx, p.owner, p.repo, branch)
		if berr != nil {
			p.logger.Warn("Failed to check merged PRs by branch",
				slog.Int("issue", issue.Number),
				slog.String("branch", branch),
				slog.Any("error", berr),
			)
			return false
		}
		if !branchFound {
			if p.dispatch.execChecker != nil && !HasLabel(issue, LabelRetryReady) {
				taskID := fmt.Sprintf("GH-%d", issue.Number)
				completed, cerr := p.dispatch.execChecker.HasCompletedExecution(taskID, p.dispatch.projectPath)
				if cerr != nil {
					p.logger.Warn("Failed to check completed execution fallback",
						slog.Int("issue", issue.Number),
						slog.Any("error", cerr),
					)
					return false
				}
				if !completed {
					return false
				}
				p.logger.Debug("hasMergedWork: DB fallback hit",
					slog.String("task_id", taskID),
				)
			} else {
				return false
			}
		} else {
			p.logger.Info("Merged PR found via branch lookup (Search API lag)",
				slog.Int("issue", issue.Number),
				slog.String("branch", branch),
			)
		}
	}

	p.logger.Info("Issue already has merged PRs, marking as done",
		slog.Int("issue", issue.Number),
		slog.String("title", issue.Title),
	)
	if err := p.client.AddLabels(ctx, p.owner, p.repo, issue.Number, []string{LabelDone}); err != nil {
		p.logger.Warn("Failed to add pilot-done label",
			slog.Int("issue", issue.Number),
			slog.Any("error", err),
		)
	}
	// Remove stale pilot-failed label (GH-1302 gap)
	if err := p.client.RemoveLabel(ctx, p.owner, p.repo, issue.Number, LabelFailed); err != nil {
		p.logger.Debug("Failed to remove pilot-failed (may not exist)",
			slog.Int("issue", issue.Number),
			slog.Any("error", err),
		)
	}
	p.markProcessed(issue.Number)
	return true
}

// hasOpenPRAwaitingMerge reports whether an OPEN pilot PR already exists for the
// issue. Unlike hasMergedWork it is read-only — it mutates no labels and marks
// nothing processed, because the awaiting-merge state is transient (the PR will
// merge → hasMergedWork closes it, or close-without-merge → the retry-ready path
// re-picks it). TASK-321/TASK-341: skip the redundant re-dispatch that would
// otherwise produce a "no new commit produced" no-op during the
// PR-created-but-not-yet-merged window. Branch lookup is strongly consistent
// (no Search API lag), which matters here because the PR may have been created
// only one poll cycle earlier.
func (p *Poller) hasOpenPRAwaitingMerge(ctx context.Context, issue *Issue) bool {
	branch := fmt.Sprintf("pilot/GH-%d", issue.Number)
	found, err := p.client.FindOpenPRByBranch(ctx, p.owner, p.repo, branch)
	if err != nil {
		p.logger.Warn("Failed to check for open PRs by branch",
			slog.Int("issue", issue.Number),
			slog.String("branch", branch),
			slog.Any("error", err),
		)
		return false
	}
	if found {
		p.logger.Info("Issue has an open PR awaiting merge",
			slog.Int("issue", issue.Number),
			slog.String("branch", branch),
		)
	}
	return found
}

// shouldRetryFailedIssue checks if a pilot-failed issue should be auto-retried.
// Returns true if the issue should be retried (label removed), false if it should be skipped.
// GH-2176: Issues stuck with pilot-failed get retried up to maxFailedRetries times.
func (p *Poller) shouldRetryFailedIssue(ctx context.Context, issue *Issue) bool {
	// Don't retry closed issues — they may have stale pilot-failed labels (GH-2252)
	if issue.State != "open" {
		p.logger.Info("Skipping retry — issue is closed",
			slog.Int("number", issue.Number),
			slog.String("state", issue.State),
		)
		return false
	}

	// Never retry if also marked done
	if HasLabel(issue, LabelDone) {
		return false
	}

	// GH-2363: Title-guard escalation explicitly halted retries. A human must
	// edit the title and remove pilot-title-rejected before we try again.
	if HasLabel(issue, LabelTitleRejected) {
		p.logger.Info("Skipping retry — pilot-title-rejected set (GH-2363)",
			slog.Int("number", issue.Number),
		)
		return false
	}

	p.mu.RLock()
	retries := p.dispatch.failedRetryCount[issue.Number]
	p.mu.RUnlock()

	if retries >= p.dispatch.maxFailedRetries {
		p.logger.Warn("Issue has reached max failed retries, skipping",
			slog.Int("number", issue.Number),
			slog.Int("retries", retries),
			slog.Int("max", p.dispatch.maxFailedRetries),
		)
		return false
	}

	// Check if merged work already exists before retrying
	if p.hasMergedWork(ctx, issue) {
		return false
	}

	// Remove pilot-failed label and increment retry count
	if err := p.client.RemoveLabel(ctx, p.owner, p.repo, issue.Number, LabelFailed); err != nil {
		p.logger.Warn("Failed to remove pilot-failed label for retry",
			slog.Int("number", issue.Number),
			slog.Any("error", err),
		)
		return false
	}

	p.mu.Lock()
	p.dispatch.failedRetryCount[issue.Number] = retries + 1
	p.mu.Unlock()

	// Clear from processed map so the issue can be re-picked
	p.ClearProcessed(issue.Number)

	p.logger.Info("Auto-retrying pilot-failed issue",
		slog.Int("number", issue.Number),
		slog.Int("retry", retries+1),
		slog.Int("max", p.dispatch.maxFailedRetries),
	)

	return true
}

// shouldRetryRetryReadyIssue checks if a pilot-retry-ready issue should be auto-retried.
// Returns true if the issue should be retried (label removed), false if it should be skipped.
// GH-2276: Issues with pilot-retry-ready (PR closed without merge) get retried up to maxRetryReadyRetries times.
//
// GH-2432: Retry counter is persisted via GitHub labels (pilot-retry-1, -2,
// -exhausted) so the count survives `pilot start` restarts. The previous
// in-memory map silently reset on restart, allowing pathological issues to
// consume Opus indefinitely.
func (p *Poller) shouldRetryRetryReadyIssue(ctx context.Context, issue *Issue) bool {
	// Don't retry closed issues
	if issue.State != "open" {
		p.logger.Info("Skipping retry — issue is closed",
			slog.Int("number", issue.Number),
			slog.String("state", issue.State),
		)
		return false
	}

	// Never retry if also marked done
	if HasLabel(issue, LabelDone) {
		return false
	}

	// GH-2432: terminal state — exhausted retries never retry again.
	if HasLabel(issue, LabelRetryExhausted) {
		p.logger.Warn("Issue is pilot-retry-exhausted, skipping",
			slog.Int("number", issue.Number),
		)
		return false
	}

	// Determine the next retry-counter label based on current state.
	var currentRetryLabel, nextRetryLabel string
	switch {
	case HasLabel(issue, LabelRetry2):
		currentRetryLabel = LabelRetry2
		nextRetryLabel = LabelRetryExhausted
	case HasLabel(issue, LabelRetry1):
		currentRetryLabel = LabelRetry1
		nextRetryLabel = LabelRetry2
	default:
		nextRetryLabel = LabelRetry1
	}

	// If escalating to exhausted, mark the label and skip dispatch.
	if nextRetryLabel == LabelRetryExhausted {
		if err := p.client.RemoveLabel(ctx, p.owner, p.repo, issue.Number, currentRetryLabel); err != nil {
			p.logger.Warn("Failed to remove prior retry label",
				slog.Int("number", issue.Number),
				slog.String("label", currentRetryLabel),
				slog.Any("error", err),
			)
		}
		if err := p.client.AddLabels(ctx, p.owner, p.repo, issue.Number, []string{LabelRetryExhausted}); err != nil {
			p.logger.Warn("Failed to add pilot-retry-exhausted label",
				slog.Int("number", issue.Number),
				slog.Any("error", err),
			)
		}
		// Also clear pilot-retry-ready so the poller doesn't keep finding it.
		_ = p.client.RemoveLabel(ctx, p.owner, p.repo, issue.Number, LabelRetryReady)
		p.logger.Warn("Issue exhausted retry budget — escalated to pilot-retry-exhausted",
			slog.Int("number", issue.Number),
		)
		return false
	}

	// Check if merged work already exists before retrying
	if p.hasMergedWork(ctx, issue) {
		return false
	}

	// Swap retry-N label: remove current (if any), add next.
	if currentRetryLabel != "" {
		if err := p.client.RemoveLabel(ctx, p.owner, p.repo, issue.Number, currentRetryLabel); err != nil {
			p.logger.Warn("Failed to remove prior retry label",
				slog.Int("number", issue.Number),
				slog.String("label", currentRetryLabel),
				slog.Any("error", err),
			)
		}
	}
	if err := p.client.AddLabels(ctx, p.owner, p.repo, issue.Number, []string{nextRetryLabel}); err != nil {
		p.logger.Warn("Failed to add retry label",
			slog.Int("number", issue.Number),
			slog.String("label", nextRetryLabel),
			slog.Any("error", err),
		)
		// Continue anyway; we'd rather retry than block on a label flake.
	}

	// Remove pilot-retry-ready label so the poller doesn't loop the same issue.
	if err := p.client.RemoveLabel(ctx, p.owner, p.repo, issue.Number, LabelRetryReady); err != nil {
		p.logger.Warn("Failed to remove pilot-retry-ready label for retry",
			slog.Int("number", issue.Number),
			slog.Any("error", err),
		)
		return false
	}

	// GH-2432: the retry budget is now tracked entirely via the pilot-retry-N
	// labels swapped above; no in-memory counter to bump.

	if p.dispatch.execChecker != nil {
		taskID := fmt.Sprintf("GH-%d", issue.Number)
		if err := p.dispatch.execChecker.InvalidateCompletion(taskID, p.dispatch.projectPath); err != nil {
			p.logger.Warn("InvalidateCompletion failed on retry-ready re-dispatch; proceeding",
				slog.String("task_id", taskID),
				slog.Any("error", err),
			)
		}
	}

	// Clear from processed map so the issue can be re-picked
	p.ClearProcessed(issue.Number)

	p.logger.Info("Auto-retrying pilot-retry-ready issue",
		slog.Int("number", issue.Number),
		slog.String("retry_label", nextRetryLabel),
	)

	return true
}

// skipSupersededByParent auto-closes a sub-issue whose parent epic has already
// shipped (closed AND has pilot-done). Returns true if the issue was closed
// and should be skipped from dispatch. GH-2402.
//
// On transient API errors (e.g. failed parent lookup) the function returns
// false so the issue falls through to normal dispatch — we never want to lose
// work because of a flaky GET.
func (p *Poller) skipSupersededByParent(ctx context.Context, issue *Issue) bool {
	parentNum := ParseParentIssueNumber(issue.Body)
	if parentNum <= 0 {
		return false
	}

	parent, err := p.client.GetIssue(ctx, p.owner, p.repo, parentNum)
	if err != nil {
		p.logger.Warn("Failed to fetch parent issue, falling through to normal dispatch",
			slog.Int("issue", issue.Number),
			slog.Int("parent", parentNum),
			slog.Any("error", err),
		)
		return false
	}

	// Parent must be both closed AND marked done. A closed-without-done parent
	// (e.g. user closed the epic manually before Pilot finished) shouldn't
	// strand sub-issues.
	if parent.State != StateClosed || !HasLabel(parent, LabelDone) {
		return false
	}

	p.logger.Info("Auto-closing sub-issue: parent epic already shipped",
		slog.Int("issue", issue.Number),
		slog.Int("parent", parentNum),
	)

	comment := fmt.Sprintf(
		"🔁 Auto-closed by Pilot: parent epic #%d already shipped this work. "+
			"This sub-issue is redundant.",
		parentNum,
	)
	if _, cerr := p.client.AddComment(ctx, p.owner, p.repo, issue.Number, comment); cerr != nil {
		p.logger.Warn("Failed to post superseded comment",
			slog.Int("issue", issue.Number),
			slog.Any("error", cerr),
		)
	}
	if lerr := p.client.AddLabels(ctx, p.owner, p.repo, issue.Number, []string{LabelSuperseded}); lerr != nil {
		p.logger.Warn("Failed to add pilot-superseded label",
			slog.Int("issue", issue.Number),
			slog.Any("error", lerr),
		)
	}
	if uerr := p.client.UpdateIssueState(ctx, p.owner, p.repo, issue.Number, StateClosed); uerr != nil {
		p.logger.Warn("Failed to close superseded sub-issue",
			slog.Int("issue", issue.Number),
			slog.Any("error", uerr),
		)
	}

	p.markProcessed(issue.Number)
	return true
}
