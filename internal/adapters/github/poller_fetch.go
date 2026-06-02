package github

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"
)

// findOldestUnprocessedIssue finds the oldest issue with the pilot label
// that hasn't been processed yet and has no pending dependencies.
// fetchCandidates returns the raw candidate issues for this poll cycle. When a
// projectBoardSource is configured it sources from the board column (GH-3228);
// otherwise it lists open issues by label. This is the single source-selection
// point used by BOTH dispatch paths — findOldestUnprocessedIssue (sequential)
// and checkForNewIssues (parallel/auto) — so the board source is honored
// regardless of execution mode (TASK-338). Previously only the sequential path
// consulted the board source, so source_enabled + mode:parallel silently
// reverted to label polling.
func (p *Poller) fetchCandidates(ctx context.Context) ([]*Issue, error) {
	if p.projectBoardSource != nil {
		sourceStatus := p.projectBoardSource.config.SourceStatus
		if sourceStatus == "" {
			sourceStatus = "Todo"
		}
		return p.projectBoardSource.FindIssuesFromProject(ctx, sourceStatus)
	}
	return p.client.ListIssues(ctx, p.owner, p.repo, &ListIssuesOptions{
		Labels: []string{p.label},
		State:  StateOpen,
		Sort:   "created", // oldest first
	})
}

// When projectBoardSource is set, candidates are fetched from the board column
// instead of by label; all downstream filters remain identical.
func (p *Poller) findOldestUnprocessedIssue(ctx context.Context) (*Issue, error) {
	issues, err := p.fetchCandidates(ctx)
	if err != nil {
		return nil, err
	}

	// Filter out already processed and in-progress issues
	var candidates []*Issue
	for _, issue := range issues {
		// Skip pull requests (GitHub Issues API returns both issues and PRs)
		if issue.PullRequest != nil {
			continue
		}

		// Skip if in-progress or done
		if HasLabel(issue, LabelInProgress) || HasLabel(issue, LabelDone) {
			continue
		}

		// GH-2402: Skip permanently-blocked issues. The user must remove
		// the pilot-blocked label to retry (e.g. after fixing a non-conventional title).
		if HasLabel(issue, LabelBlocked) {
			continue
		}

		// GH-2768: Skip issues declined as unactionable. Remove the label to re-enable dispatch.
		if HasLabel(issue, LabelNeedsClarification) {
			continue
		}

		// GH-2402: Auto-close sub-issues whose parent epic already shipped.
		if p.skipSupersededByParent(ctx, issue) {
			continue
		}

		// GH-2176: Auto-retry issues stuck with pilot-failed (no pilot-done)
		if HasLabel(issue, LabelFailed) {
			if !p.shouldRetryFailedIssue(ctx, issue) {
				continue
			}
			// Label removed, fall through to candidate selection
		}

		// GH-2276: Auto-retry issues with pilot-retry-ready (PR closed without merge)
		if HasLabel(issue, LabelRetryReady) {
			if !p.shouldRetryRetryReadyIssue(ctx, issue) {
				continue
			}
			// Label removed, fall through to candidate selection
		}

		// Check if previously processed
		p.mu.RLock()
		processedAt, processed := p.processed[issue.Number]
		p.mu.RUnlock()

		// If processed but no status labels, allow retry (pilot-failed was removed)
		if processed {
			// GH-2201: Check grace period before allowing retry
			if p.retryGracePeriod > 0 && time.Since(processedAt) < p.retryGracePeriod {
				p.logger.Debug("Issue within retry grace period, skipping",
					slog.Int("number", issue.Number),
					slog.Duration("elapsed", time.Since(processedAt)),
					slog.Duration("grace_period", p.retryGracePeriod))
				continue
			}

			// GH-2201: Check if task is still queued/in-progress
			if p.taskChecker != nil {
				taskID := fmt.Sprintf("GH-%d", issue.Number)
				if p.taskChecker.IsTaskQueued(taskID) {
					p.logger.Debug("Issue still queued/in-progress, skipping retry",
						slog.Int("number", issue.Number),
						slog.String("task_id", taskID))
					continue
				}
			}

			p.logger.Info("Issue was processed but status labels removed, allowing retry",
				slog.Int("number", issue.Number))
			p.mu.Lock()
			delete(p.processed, issue.Number)
			p.mu.Unlock()
			// Also clear from persistent store
			if p.processedStore != nil {
				if err := p.processedStore.Unmark("github", p.repoKey(), strconv.Itoa(issue.Number)); err != nil {
					p.logger.Warn("Failed to unmark issue in store",
						slog.Int("number", issue.Number),
						slog.Any("error", err))
				}
			}

			// GH-1983: Before retrying, check if merged PRs already exist
			if p.hasMergedWork(ctx, issue) {
				continue
			}

			// TASK-341: a re-dispatch whose pilot/GH-N PR is still OPEN (created but
			// not yet merged — pilot-done/close are deferred to merge time per
			// GH-3139/TASK-301) would only produce a "no new commit produced" no-op
			// that the handler used to mislabel pilot-blocked. Re-mark so the grace
			// window throttles re-checks (mirrors hasMergedWork); do NOT label — the
			// autopilot merge flow owns this issue until the PR merges or closes.
			if p.hasOpenPRAwaitingMerge(ctx, issue) {
				p.markProcessed(issue.Number)
				continue
			}
		}

		// GH-3269: Fresh candidates (never processed / post-unmark) bypass the
		// retry block above, so apply the merged-work guard unconditionally for them.
		if !processed && p.hasMergedWork(ctx, issue) {
			continue
		}

		// GH-3269: Mirror the parallel-mode HasCompletedExecution guard — prevents
		// re-dispatch when the pilot-done label failed to apply after execution.
		if p.execChecker != nil {
			taskID := fmt.Sprintf("GH-%d", issue.Number)
			completed, err := p.execChecker.HasCompletedExecution(taskID, p.projectPath)
			if err != nil {
				p.logger.Warn("Failed to check execution status",
					slog.Int("number", issue.Number),
					slog.Any("error", err))
			} else if completed {
				p.logger.Info("Skipping re-dispatch — completed execution exists",
					slog.Int("number", issue.Number),
					slog.String("task_id", taskID))
				p.markProcessed(issue.Number)
				continue
			}
		}

		candidates = append(candidates, issue)
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Sort by creation date (oldest first)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})

	// Find the oldest issue without pending dependencies
	for _, candidate := range candidates {
		if !p.hasPendingDependencies(ctx, candidate) {
			return candidate, nil
		}
		p.logger.Info("Skipping issue with pending dependencies",
			slog.Int("number", candidate.Number),
			slog.String("title", candidate.Title),
		)
	}

	// All candidates have pending dependencies
	return nil, nil
}
