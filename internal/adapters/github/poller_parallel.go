package github

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/skipreason"
	"github.com/ylcn91/pilot/internal/executor"
)

// checkForNewIssues fetches issues and dispatches new ones concurrently (parallel mode)
func (p *Poller) checkForNewIssues(ctx context.Context) {
	issues, err := p.fetchCandidates(ctx)
	if err != nil {
		p.logger.Warn("Failed to fetch issues", slog.Any("error", err))
		return
	}

	// Phase 1: Collect candidates eligible for dispatch
	var candidates []*Issue
	for _, issue := range issues {
		// Skip pull requests (GitHub Issues API returns both issues and PRs)
		if issue.PullRequest != nil {
			continue
		}

		// Skip if already in progress
		if HasLabel(issue, LabelInProgress) {
			p.recordSkip(skipreason.ReasonInProgress)
			continue
		}

		// GH-2402: Skip permanently-blocked issues. The user must remove
		// the pilot-blocked label to retry (e.g. after fixing a non-conventional title).
		if HasLabel(issue, LabelBlocked) {
			p.recordSkip(skipreason.ReasonBlocked)
			continue
		}

		// GH-2768: Skip issues declined as unactionable. Remove the label to re-enable dispatch.
		if HasLabel(issue, LabelNeedsClarification) {
			p.recordSkip(skipreason.ReasonNeedsClarification)
			continue
		}

		// GH-2402: Auto-close sub-issues whose parent epic already shipped.
		if p.skipSupersededByParent(ctx, issue) {
			p.recordSkip(skipreason.ReasonSuperseded)
			continue
		}

		// GH-2176: Auto-retry issues stuck with pilot-failed (no pilot-done)
		if HasLabel(issue, LabelFailed) {
			if !p.shouldRetryFailedIssue(ctx, issue) {
				p.recordSkip(skipreason.ReasonFailedSkip)
				continue
			}
			// Label removed, fall through to candidate selection
		}

		// GH-2276: Auto-retry issues with pilot-retry-ready (PR closed without merge)
		if HasLabel(issue, LabelRetryReady) {
			if !p.shouldRetryRetryReadyIssue(ctx, issue) {
				p.recordSkip(skipreason.ReasonRetryReadySkip)
				continue
			}
			// Label removed, fall through to candidate selection
		}

		// Skip and mark done issues as permanently processed
		if HasLabel(issue, LabelDone) {
			p.markProcessed(issue.Number)
			p.recordSkip(skipreason.ReasonDone)
			continue
		}

		// Check if already processed
		p.mu.RLock()
		processedAt, processed := p.processed[issue.Number]
		p.mu.RUnlock()

		// If processed but no status labels, allow retry (pilot-failed was removed)
		if processed {
			// GH-2201: Check grace period before allowing retry
			if p.dispatch.retryGracePeriod > 0 && time.Since(processedAt) < p.dispatch.retryGracePeriod {
				p.logger.Debug("Issue within retry grace period, skipping",
					slog.Int("number", issue.Number),
					slog.Duration("elapsed", time.Since(processedAt)),
					slog.Duration("grace_period", p.dispatch.retryGracePeriod))
				p.recordSkip(skipreason.ReasonProcessedGrace)
				continue
			}

			// GH-2201: Check if task is still queued/in-progress
			if p.dispatch.taskChecker != nil {
				taskID := fmt.Sprintf("GH-%d", issue.Number)
				if p.dispatch.taskChecker.IsTaskQueued(taskID) {
					p.logger.Debug("Issue still queued/in-progress, skipping retry",
						slog.Int("number", issue.Number),
						slog.String("task_id", taskID))
					p.recordSkip(skipreason.ReasonTaskQueued)
					continue
				}
			}

			p.logger.Info("Issue was processed but status labels removed, allowing retry",
				slog.Int("number", issue.Number))
			p.mu.Lock()
			delete(p.processed, issue.Number)
			p.mu.Unlock()
			if p.processedStore != nil {
				if err := p.processedStore.Unmark("github", p.repoKey(), strconv.Itoa(issue.Number)); err != nil {
					p.logger.Warn("Failed to unmark issue in store",
						slog.Int("number", issue.Number),
						slog.Any("error", err))
				}
			}

			// GH-1983: Before retrying, check if merged PRs already exist
			if p.hasMergedWork(ctx, issue) {
				p.recordSkip(skipreason.ReasonHasMergedWork)
				continue
			}

			// TASK-341: skip a re-dispatch whose pilot/GH-N PR is still OPEN (parallel
			// mode). Mirrors the sequential guard — the open PR is awaiting merge
			// (pilot-done/close deferred per GH-3139/TASK-301), so re-dispatch would
			// only no-op ("no new commit produced") and used to be mislabeled
			// pilot-blocked. Re-mark so the grace window throttles re-checks; do NOT
			// label — leave the open PR for the autopilot merge flow.
			if p.hasOpenPRAwaitingMerge(ctx, issue) {
				p.markProcessed(issue.Number)
				p.recordSkip(skipreason.ReasonHasOpenPR)
				continue
			}
		}

		// GH-3269 / TASK-321 PR-4: Fresh candidates (never processed / post-unmark)
		// bypass the retry block above, so apply the merged-work guard
		// unconditionally for them — mirrors the sequential
		// findOldestUnprocessedIssue guard and prevents phantom
		// "no new commit produced" redispatch in parallel mode.
		if !processed && p.hasMergedWork(ctx, issue) {
			p.recordSkip(skipreason.ReasonHasMergedWork)
			continue
		}

		// Skip issues with pending dependencies
		if p.hasPendingDependencies(ctx, issue) {
			p.logger.Debug("Skipping issue with pending dependencies in parallel mode",
				slog.Int("number", issue.Number),
			)
			p.recordSkip(skipreason.ReasonPendingDependency)
			continue
		}

		// GH-2242: Before dispatching, check if we already have a completed execution.
		// This prevents re-dispatch when pilot-done label failed to apply.
		if p.dispatch.execChecker != nil {
			taskID := fmt.Sprintf("GH-%d", issue.Number)
			completed, err := p.dispatch.execChecker.HasCompletedExecution(taskID, p.dispatch.projectPath)
			if err != nil {
				p.logger.Warn("Failed to check execution status",
					slog.Int("number", issue.Number),
					slog.Any("error", err))
			} else if completed {
				p.logger.Info("Skipping re-dispatch — completed execution exists",
					slog.Int("number", issue.Number),
					slog.String("task_id", taskID))
				p.markProcessed(issue.Number)
				p.recordSkip(skipreason.ReasonCompletedExecution)
				continue
			}
		}

		candidates = append(candidates, issue)
	}

	// Phase 2: Group candidates by overlapping scope, dispatch only oldest per group
	groups := groupByOverlappingScope(candidates)
	var toDispatch []*Issue
	for _, group := range groups {
		if len(group) == 1 {
			toDispatch = append(toDispatch, group[0])
		} else {
			// Sort by CreatedAt ascending; dispatch only the oldest
			sort.Slice(group, func(i, j int) bool {
				return group[i].CreatedAt.Before(group[j].CreatedAt)
			})
			toDispatch = append(toDispatch, group[0])
			for _, deferred := range group[1:] {
				p.logger.Info("Deferring issue due to overlapping scope with older issue",
					slog.Int("number", deferred.Number),
					slog.Int("dispatched", group[0].Number),
				)
				p.recordDeferredScopeOverlap()
			}
		}
	}

	// Phase 3: Dispatch selected issues
	for _, issue := range toDispatch {
		// GH-2341: Refresh labels via single-issue GET to bypass stale ListIssues snapshot.
		// If pilot-done or pilot-in-progress was added after the list was fetched,
		// the snapshot's Labels will not reflect it — fetch fresh state.
		if fresh, ferr := p.client.GetIssue(ctx, p.owner, p.repo, issue.Number); ferr == nil && fresh != nil {
			if HasLabel(fresh, LabelDone) || HasLabel(fresh, LabelInProgress) {
				p.logger.Info("Skipping dispatch — fresh labels show issue already handled",
					slog.Int("number", issue.Number),
					slog.Bool("done", HasLabel(fresh, LabelDone)),
					slog.Bool("in_progress", HasLabel(fresh, LabelInProgress)),
				)
				p.markProcessed(issue.Number)
				p.recordSkip(skipreason.ReasonFreshLabelCheck)
				continue
			}
		} else if ferr != nil {
			p.logger.Debug("Failed to refresh issue labels before dispatch — proceeding with snapshot",
				slog.Int("number", issue.Number),
				slog.Any("error", ferr),
			)
		}

		// GH-2802: Pre-flight judge — evaluate issue quality before burning a worker slot.
		if p.dispatch.preFlightJudge != nil {
			verdict, pfErr := p.dispatch.preFlightJudge.JudgeIssue(ctx, issue.Title, issue.Body, "")
			if pfErr != nil {
				p.logger.Warn("pre-flight judge error (fail-open)",
					slog.Int("issue", issue.Number),
					slog.Any("error", pfErr))
			} else if !verdict.Accepted {
				p.logger.Info("pre-flight rejected issue",
					slog.Int("issue", issue.Number),
					slog.String("decision", verdict.Decision),
					slog.String("reason", verdict.Reason))
				p.handlePreFlightReject(ctx, issue, verdict)
				p.recordSkip(skipreason.ReasonPreFlightReject)
				continue // skip markProcessed so label removal re-triggers dispatch
			}
		}

		// Mark processed immediately to prevent duplicate dispatch on next tick
		p.markProcessed(issue.Number)

		// Acquire semaphore slot (blocks if max_concurrent reached)
		select {
		case <-ctx.Done():
			return
		case p.semaphore <- struct{}{}:
		}

		p.recordDispatched()
		p.logger.Info("Dispatching issue for parallel execution",
			slog.Int("number", issue.Number),
			slog.String("title", issue.Title),
		)

		// Use mutex to coordinate stopping flag check with WaitGroup Add
		p.wgMu.Lock()
		if p.stopping.Load() {
			p.wgMu.Unlock()
			<-p.semaphore // release slot we acquired
			return
		}
		p.activeWg.Add(1)
		p.wgMu.Unlock()
		go func(issue *Issue) {
			defer p.activeWg.Done()
			defer func() { <-p.semaphore }() // release slot

			// Board sync: move card to in-progress on confirmed dispatch (GH-3252).
			p.syncBoardStatusInProgress(ctx, issue)

			if p.onIssueWithResult != nil {
				result, err := p.onIssueWithResult(ctx, issue)
				if err != nil {
					p.logger.Error("Failed to process issue",
						slog.Int("number", issue.Number),
						slog.Any("error", err),
					)
					// GH-2176: Unmark so retry path can re-pick after pilot-failed is removed
					p.unmarkProcessed(issue.Number)
					return
				}

				// GH-2176: Unmark if execution failed without creating a PR (unless permanent).
				// GH-3270: Permanent/no-op failures already carry pilot-blocked; retaining the
				// durable row is defense-in-depth so a daemon restart cannot re-dispatch until
				// the human removes pilot-blocked (which clears the mark via the retry path).
				if result != nil && !result.Success && result.PRNumber == 0 {
					if result.Error != nil && executor.IsPermanentFailure(result.Error.Error()) {
						p.logger.Info("Permanent failure — retaining adapter_processed marker",
							slog.Int("number", issue.Number),
							slog.String("error", result.Error.Error()),
						)
					} else {
						p.logger.Info("Execution failed without PR, unmarking for retry",
							slog.Int("number", issue.Number),
						)
						p.unmarkProcessed(issue.Number)
					}
				}

				// Diagnostic: surface why OnPRCreated may not fire (GH-2999 Phase 1)
				if result == nil {
					p.logger.Info("OnPRCreated skipped: result is nil",
						slog.Int("issue_number", issue.Number),
					)
				} else if result.PRNumber == 0 {
					p.logger.Info("OnPRCreated skipped: PRNumber=0",
						slog.Int("issue_number", issue.Number),
						slog.String("pr_url", result.PRURL),
						slog.String("branch", result.BranchName),
						slog.String("head_sha", result.HeadSHA),
					)
				} else if p.OnPRCreated == nil {
					p.logger.Info("OnPRCreated skipped: callback not wired",
						slog.Int("pr_number", result.PRNumber),
						slog.Int("issue_number", issue.Number),
					)
				}
				// Gate: PRNumber > 0 implies executor surfaced a valid PR URL via runner.go:3151. Empty PRUrl (no-commits guard, push-fail, title-rejection) leaves PRNumber=0 and we silently skip — see TASK-60 for the upstream chain.
				// Notify autopilot controller of new PR
				if result != nil && result.PRNumber > 0 && p.OnPRCreated != nil {
					p.logger.Info("Notifying autopilot of PR creation (parallel path)",
						slog.Int("pr_number", result.PRNumber),
						slog.Int("issue_number", issue.Number),
						slog.String("branch", result.BranchName),
					)
					p.OnPRCreated(result.PRNumber, result.PRURL, issue.Number, result.HeadSHA, result.BranchName, issue.NodeID)
				}
			} else if p.onIssue != nil {
				if err := p.onIssue(ctx, issue); err != nil {
					p.logger.Error("Failed to process issue",
						slog.Int("number", issue.Number),
						slog.Any("error", err),
					)
					// GH-2176: Unmark so retry path can re-pick
					p.unmarkProcessed(issue.Number)
				}
			}
		}(issue)
	}
}
