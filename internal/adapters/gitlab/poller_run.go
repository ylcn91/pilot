package gitlab

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/skipreason"
	"github.com/ylcn91/pilot/internal/executor"
)

// recoverOrphanedIssues finds issues with pilot-in-progress label from a previous run
// and removes the label so they can be picked up again.
// GH-1355: This handles restart/crash scenarios where issues were left orphaned.
func (p *Poller) recoverOrphanedIssues(ctx context.Context) {
	issues, err := p.client.ListIssues(ctx, &ListIssuesOptions{
		Labels: []string{p.label, LabelInProgress},
		State:  StateOpened,
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
		if err := p.client.RemoveIssueLabel(ctx, issue.IID, LabelInProgress); err != nil {
			p.logger.Warn("Failed to remove in-progress label from orphaned issue",
				slog.Int("iid", issue.IID),
				slog.Any("error", err),
			)
			continue
		}
		// GH-2301: Also clear from processed map/store so the first poll cycle picks it up.
		p.ClearProcessed(issue.IID)
		p.logger.Info("Recovered orphaned issue",
			slog.Int("iid", issue.IID),
			slog.String("title", issue.Title),
		)
	}
}

// startParallel runs the parallel execution mode with goroutine dispatch
func (p *Poller) startParallel(ctx context.Context) {
	// Do an initial check immediately
	p.checkForNewIssues(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("GitLab poller stopping, waiting for active tasks...")
			p.wgMu.Lock()
			p.stopping.Store(true)
			p.wgMu.Unlock()
			p.activeWg.Wait()
			p.logger.Info("GitLab poller stopped")
			return
		case <-ticker.C:
			p.checkForNewIssues(ctx)
		}
	}
}

// startSequential runs the sequential execution mode
// Processes one issue at a time, waits for MR merge before next
func (p *Poller) startSequential(ctx context.Context) {
	p.logger.Info("Running in sequential mode",
		slog.Bool("wait_for_merge", p.waitForMerge),
		slog.Duration("mr_timeout", p.mrTimeout),
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
			slog.Int("iid", issue.IID),
			slog.String("title", issue.Title),
		)

		result, err := p.processIssueSequential(ctx, issue)
		if err != nil {
			// GH-3252 parity: a rate-limit error is transient. Marking the issue
			// processed would drop it until restart, so leave it unprocessed and
			// let the next poll cycle pick it up once the limit resets.
			if executor.IsRateLimitError(err.Error()) {
				p.logger.Warn("Rate limited processing issue, deferring for retry",
					slog.Int("iid", issue.IID),
					slog.Any("error", err),
				)
				p.recordSkip(skipreason.ReasonTaskQueued)
				// DON'T mark as processed - retry on the next poll cycle.
				continue
			}

			p.logger.Error("Failed to process issue",
				slog.Int("iid", issue.IID),
				slog.Any("error", err),
			)
			// Mark as processed to avoid infinite retry loop
			// The issue will have pilot-failed label
			p.markProcessed(issue.IID)
			continue
		}

		// Notify autopilot controller of new MR (if callback registered)
		if result != nil && result.MRNumber > 0 && p.OnMRCreated != nil {
			p.logger.Info("Notifying autopilot of MR creation",
				slog.Int("mr_iid", result.MRNumber),
				slog.Int("issue_iid", issue.IID),
				slog.String("branch", result.BranchName),
			)
			p.OnMRCreated(result.MRNumber, result.MRURL, issue.IID, result.HeadSHA, result.BranchName)
		}

		// If we created an MR and should wait for merge
		if result != nil && result.MRNumber > 0 && p.waitForMerge && p.mergeWaiter != nil {
			p.logger.Info("Waiting for MR merge before next issue",
				slog.Int("mr_iid", result.MRNumber),
				slog.String("mr_url", result.MRURL),
			)

			mergeResult, err := p.mergeWaiter.WaitWithCallback(ctx, result.MRNumber, func(r *MergeWaitResult) {
				p.logger.Debug("MR status check",
					slog.Int("mr_iid", r.MRNumber),
					slog.String("status", r.Message),
				)
			})

			if err != nil {
				p.logger.Warn("Error waiting for MR merge, pausing sequential processing",
					slog.Int("mr_iid", result.MRNumber),
					slog.Any("error", err),
				)
				// DON'T mark as processed - leave for retry after fix
				time.Sleep(5 * time.Minute)
				continue
			}

			p.logger.Info("MR merge wait completed",
				slog.Int("mr_iid", result.MRNumber),
				slog.Bool("merged", mergeResult.Merged),
				slog.Bool("closed", mergeResult.Closed),
				slog.Bool("has_conflicts", mergeResult.HasConflicts),
				slog.Bool("timed_out", mergeResult.TimedOut),
			)

			// Check if MR has conflicts - stop processing
			if mergeResult.HasConflicts {
				p.logger.Warn("MR has conflicts, pausing sequential processing",
					slog.Int("mr_iid", result.MRNumber),
					slog.String("mr_url", result.MRURL),
				)
				// DON'T mark as processed - needs manual resolution or rebase
				time.Sleep(5 * time.Minute)
				continue
			}

			// Check if MR timed out
			if mergeResult.TimedOut {
				p.logger.Warn("MR merge timed out, pausing sequential processing",
					slog.Int("mr_iid", result.MRNumber),
					slog.String("mr_url", result.MRURL),
				)
				// DON'T mark as processed - needs investigation
				time.Sleep(5 * time.Minute)
				continue
			}

			// Only mark as processed if actually merged
			if mergeResult.Merged {
				p.markProcessed(issue.IID)
				continue
			}

			// MR was closed without merge
			if mergeResult.Closed {
				p.logger.Info("MR was closed without merge",
					slog.Int("mr_iid", result.MRNumber),
				)
				// DON'T mark as processed - issue may need re-execution
				continue
			}
		}

		// Direct commit case: no MR to wait for, proceed to next issue
		if result != nil && result.Success && result.MRNumber == 0 {
			p.logger.Info("Direct commit completed, proceeding to next issue",
				slog.Int("issue_iid", issue.IID),
				slog.String("commit_sha", result.HeadSHA),
			)
			p.markProcessed(issue.IID)
			continue
		}

		// MR was created but we're not waiting for merge, or no MR was created
		p.markProcessed(issue.IID)
	}
}

// findOldestUnprocessedIssue finds the oldest issue with the pilot label
// that hasn't been processed yet
func (p *Poller) findOldestUnprocessedIssue(ctx context.Context) (*Issue, error) {
	issues, err := p.client.ListIssues(ctx, &ListIssuesOptions{
		Labels:  []string{p.label},
		State:   StateOpened,
		Sort:    "asc", // Oldest first
		OrderBy: "created_at",
	})
	if err != nil {
		return nil, err
	}

	// Filter out already processed and in-progress issues
	var candidates []*Issue
	for _, issue := range issues {
		p.mu.RLock()
		processed := p.processed[issue.IID]
		p.mu.RUnlock()

		if processed {
			continue
		}

		if HasLabel(issue, LabelInProgress) || HasLabel(issue, LabelDone) {
			p.recordSkip(p.statusLabelSkipReason(issue))
			continue
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

	return candidates[0], nil
}

// processIssueSequential processes a single issue and returns MR info
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
