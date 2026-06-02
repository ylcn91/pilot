package github

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/text"
)

// recoverOrphanedIssues finds issues with pilot-in-progress label from a previous run
// and removes the label so they can be picked up again.
// GH-1355: This handles restart/crash scenarios where issues were left orphaned.
func (p *Poller) recoverOrphanedIssues(ctx context.Context) {
	issues, err := p.client.ListIssues(ctx, p.owner, p.repo, &ListIssuesOptions{
		Labels: []string{p.label, LabelInProgress},
		State:  StateOpen,
	})
	if err != nil {
		p.logger.Warn("Failed to check for orphaned issues", slog.Any("error", err))
		return
	}

	if len(issues) == 0 {
		return
	}

	p.logger.Info("Recovering orphaned in-progress issues",
		slog.Int("count", len(issues)),
	)

	for _, issue := range issues {
		if err := p.client.RemoveLabel(ctx, p.owner, p.repo, issue.Number, LabelInProgress); err != nil {
			p.logger.Warn("Failed to remove in-progress label from orphaned issue",
				slog.Int("number", issue.Number),
				slog.Any("error", err),
			)
			continue
		}
		// GH-2301: Also clear from processed map/store so the first poll cycle picks it up.
		p.unmarkProcessed(issue.Number)
		p.logger.Info("Recovered orphaned issue",
			slog.Int("number", issue.Number),
			slog.String("title", issue.Title),
		)
	}
}

// startParallel runs concurrent issue execution with a semaphore limiter.
// Used by both "parallel" and "auto" modes. In "auto" mode, checkForNewIssues
// applies the scope-overlap guard so that overlapping issues are held back.
func (p *Poller) startParallel(ctx context.Context) {
	p.logger.Info("Running in parallel mode",
		slog.String("mode", string(p.executionMode)),
		slog.Int("max_concurrent", p.maxConcurrent),
	)

	// Do an initial check immediately
	p.checkForNewIssues(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Parallel poller stopping, waiting for active tasks...")
			p.wgMu.Lock()
			p.stopping.Store(true)
			p.wgMu.Unlock()
			p.activeWg.Wait()
			p.logger.Info("Parallel poller stopped")
			return
		case <-ticker.C:
			p.checkForNewIssues(ctx)
		}
	}
}

// startSequential runs the sequential execution mode
// Processes one issue at a time, waits for PR merge before next
func (p *Poller) startSequential(ctx context.Context) {
	p.logger.Info("Running in sequential mode",
		slog.Bool("wait_for_merge", p.waitForMerge),
		slog.Duration("pr_timeout", p.prTimeout),
	)

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Sequential poller stopped")
			return
		default:
		}

		// Find oldest unprocessed issue
		issue, err := p.findOldestUnprocessedIssue(ctx)
		if err != nil {
			p.logger.Warn("Failed to find issues", slog.Any("error", err))
			time.Sleep(p.interval)
			continue
		}

		if issue == nil {
			// No issues to process, wait before checking again
			p.logger.Debug("No unprocessed issues found, waiting...")
			select {
			case <-ctx.Done():
				return
			case <-time.After(p.interval):
				continue
			}
		}

		// Process the issue
		p.logger.Info("Processing issue in sequential mode",
			slog.Int("number", issue.Number),
			slog.String("title", issue.Title),
		)

		// GH-2802: Pre-flight judge — evaluate issue quality before burning a worker slot.
		if p.preFlightJudge != nil {
			verdict, pfErr := p.preFlightJudge.JudgeIssue(ctx, issue.Title, issue.Body, "")
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
				continue // skip dispatch; label removal re-triggers on next poll
			}
		}

		// Board sync: move card to in-progress on confirmed dispatch (GH-3252).
		p.syncBoardStatusInProgress(ctx, issue)

		result, err := p.processIssueSequential(ctx, issue)
		if err != nil {
			// Check if this is a rate limit error that can be retried
			if executor.IsRateLimitError(err.Error()) {
				rlInfo, ok := executor.ParseRateLimitError(err.Error())
				if ok && p.scheduler != nil {
					// Second converter path (rate-limit retry) — bypasses
					// ConvertIssueToTask, so sanitize explicitly here.
					cleanTitle, titleStripped := text.SanitizeUntrusted(issue.Title)
					cleanBody, bodyStripped := text.SanitizeUntrusted(issue.Body)
					if titleStripped+bodyStripped > 0 {
						p.logger.Warn("invisible_unicode_stripped",
							slog.String("path", "rate_limit_retry"),
							slog.Int("issue", issue.Number),
							slog.Int("title_stripped", titleStripped),
							slog.Int("body_stripped", bodyStripped),
						)
					}
					task := &executor.Task{
						ID:          fmt.Sprintf("GH-%d", issue.Number),
						Title:       cleanTitle,
						Description: cleanBody,
						ProjectPath: "", // Will be set by retry callback
					}
					p.scheduler.QueueTask(task, rlInfo)
					p.logger.Info("Task queued for retry after rate limit",
						slog.Int("issue", issue.Number),
						slog.Time("retry_at", rlInfo.ResetTime.Add(5*time.Minute)),
						slog.String("reset_time", rlInfo.ResetTimeFormatted()),
					)
					if p.metricsRecorder != nil {
						p.metricsRecorder.RecordIssueProcessed("rate_limited")
					}
					// Don't mark as processed - will retry via scheduler
					continue
				}
			}

			p.logger.Error("Failed to process issue",
				slog.Int("number", issue.Number),
				slog.Any("error", err),
			)
			// Don't mark as processed - the pilot-failed label is the source of truth
			// Removing the label will make the issue retryable without restart
			continue
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
		// Notify autopilot controller of new PR (if callback registered)
		// Gate: PRNumber > 0 implies executor surfaced a valid PR URL via runner.go:3151. Empty PRUrl (no-commits guard, push-fail, title-rejection) leaves PRNumber=0 and we silently skip — see TASK-60 for the upstream chain.
		if result != nil && result.PRNumber > 0 && p.OnPRCreated != nil {
			p.logger.Info("Notifying autopilot of PR creation",
				slog.Int("pr_number", result.PRNumber),
				slog.Int("issue_number", issue.Number),
				slog.String("branch", result.BranchName),
			)
			p.OnPRCreated(result.PRNumber, result.PRURL, issue.Number, result.HeadSHA, result.BranchName, issue.NodeID)
		}

		// If we created a PR and should wait for merge
		if result != nil && result.PRNumber > 0 && p.waitForMerge && p.mergeWaiter != nil {
			p.logger.Info("Waiting for PR merge before next issue",
				slog.Int("pr_number", result.PRNumber),
				slog.String("pr_url", result.PRURL),
			)

			mergeResult, err := p.mergeWaiter.WaitWithCallback(ctx, result.PRNumber, func(r *MergeWaitResult) {
				p.logger.Debug("PR status check",
					slog.Int("pr_number", r.PRNumber),
					slog.String("status", r.Message),
				)
			})

			if err != nil {
				p.logger.Warn("Error waiting for PR merge, pausing sequential processing",
					slog.Int("pr_number", result.PRNumber),
					slog.Any("error", err),
				)
				// DON'T mark as processed - leave for retry after fix
				time.Sleep(5 * time.Minute)
				continue
			}

			p.logger.Info("PR merge wait completed",
				slog.Int("pr_number", result.PRNumber),
				slog.Bool("merged", mergeResult.Merged),
				slog.Bool("closed", mergeResult.Closed),
				slog.Bool("conflicting", mergeResult.Conflicting),
				slog.Bool("timed_out", mergeResult.TimedOut),
			)

			// Check if PR has conflicts - stop processing
			if mergeResult.Conflicting {
				p.logger.Warn("PR has conflicts, pausing sequential processing",
					slog.Int("pr_number", result.PRNumber),
					slog.String("pr_url", result.PRURL),
				)
				// DON'T mark as processed - needs manual resolution or rebase
				time.Sleep(5 * time.Minute)
				continue
			}

			// Check if PR timed out
			if mergeResult.TimedOut {
				p.logger.Warn("PR merge timed out, pausing sequential processing",
					slog.Int("pr_number", result.PRNumber),
					slog.String("pr_url", result.PRURL),
				)
				// DON'T mark as processed - needs investigation
				time.Sleep(5 * time.Minute)
				continue
			}

			// Only mark as processed if actually merged
			if mergeResult.Merged {
				p.markProcessed(issue.Number)
				continue
			}

			// PR was closed without merge
			if mergeResult.Closed {
				p.logger.Info("PR was closed without merge",
					slog.Int("pr_number", result.PRNumber),
				)
				// DON'T mark as processed - issue may need re-execution
				continue
			}
		}

		// Direct commit case: no PR to wait for, proceed to next issue
		if result != nil && result.Success && result.PRNumber == 0 {
			p.logger.Info("Direct commit completed, proceeding to next issue",
				slog.Int("issue_number", issue.Number),
				slog.String("commit_sha", result.HeadSHA),
			)
			p.markProcessed(issue.Number)
			continue
		}

		// GH-2176: Don't mark as processed if execution failed (no PR created, not successful)
		// This allows the retry path in findOldestUnprocessedIssue to re-pick the issue
		// after pilot-failed label is removed (manually or by stale label cleanup)
		if result != nil && !result.Success && result.PRNumber == 0 {
			p.logger.Info("Execution failed without PR, not marking as processed (retryable)",
				slog.Int("issue_number", issue.Number),
			)
			continue
		}

		// PR was created but we're not waiting for merge, or no PR was created
		p.markProcessed(issue.Number)
	}
}

// processIssueSequential processes a single issue and returns PR info
func (p *Poller) processIssueSequential(ctx context.Context, issue *Issue) (*IssueResult, error) {
	// Use the new callback if available
	if p.onIssueWithResult != nil {
		return p.onIssueWithResult(ctx, issue)
	}

	// Fall back to legacy callback
	if p.onIssue != nil {
		err := p.onIssue(ctx, issue)
		if err != nil {
			return &IssueResult{Success: false, Error: err}, err
		}
		return &IssueResult{Success: true}, nil
	}

	return nil, fmt.Errorf("no issue handler configured")
}
