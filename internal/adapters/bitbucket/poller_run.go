package bitbucket

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/skipreason"
)

// startParallel runs the parallel execution mode with goroutine dispatch
func (p *Poller) startParallel(ctx context.Context) {
	// Do an initial check immediately
	p.checkForNewIssues(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Bitbucket poller stopping, waiting for active tasks...")
			p.wgMu.Lock()
			p.stopping.Store(true)
			p.wgMu.Unlock()
			p.activeWg.Wait()
			p.logger.Info("Bitbucket poller stopped")
			return
		case <-ticker.C:
			p.checkForNewIssues(ctx)
		}
	}
}

// startSequential runs the sequential execution mode.
// Processes one issue at a time, waits for PR merge before next.
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

		issue, err := p.findOldestUnprocessedIssue(ctx)
		if err != nil {
			p.logger.Warn("Failed to find issues", slog.Any("error", err))
			time.Sleep(p.interval)
			continue
		}

		if issue == nil {
			p.logger.Debug("No unprocessed issues found, waiting...")
			select {
			case <-ctx.Done():
				return
			case <-time.After(p.interval):
				continue
			}
		}

		p.logger.Info("Processing issue in sequential mode",
			slog.Int("id", issue.ID),
			slog.String("title", issue.Title),
		)

		result, err := p.processIssueSequential(ctx, issue)
		if err != nil {
			p.logger.Error("Failed to process issue",
				slog.Int("id", issue.ID),
				slog.Any("error", err),
			)
			// Mark as processed to avoid infinite retry loop.
			p.markProcessed(issue.ID)
			continue
		}

		// Notify autopilot controller of new PR (if callback registered)
		if result != nil && result.PRNumber > 0 && p.OnPRCreated != nil {
			p.logger.Info("Notifying autopilot of PR creation",
				slog.Int("pr_id", result.PRNumber),
				slog.Int("issue_id", issue.ID),
				slog.String("branch", result.BranchName),
			)
			p.OnPRCreated(result.PRNumber, result.PRURL, issue.ID, result.HeadSHA, result.BranchName)
		}

		// If we created a PR and should wait for merge
		if result != nil && result.PRNumber > 0 && p.waitForMerge && p.mergeWaiter != nil {
			p.logger.Info("Waiting for PR merge before next issue",
				slog.Int("pr_id", result.PRNumber),
				slog.String("pr_url", result.PRURL),
			)

			mergeResult, err := p.mergeWaiter.WaitWithCallback(ctx, result.PRNumber, func(r *MergeWaitResult) {
				p.logger.Debug("PR status check",
					slog.Int("pr_id", r.PRNumber),
					slog.String("status", r.Message),
				)
			})

			if err != nil {
				p.logger.Warn("Error waiting for PR merge, pausing sequential processing",
					slog.Int("pr_id", result.PRNumber),
					slog.Any("error", err),
				)
				// DON'T mark as processed - leave for retry after fix
				time.Sleep(5 * time.Minute)
				continue
			}

			p.logger.Info("PR merge wait completed",
				slog.Int("pr_id", result.PRNumber),
				slog.Bool("merged", mergeResult.Merged),
				slog.Bool("declined", mergeResult.Declined),
				slog.Bool("timed_out", mergeResult.TimedOut),
			)

			if mergeResult.TimedOut {
				p.logger.Warn("PR merge timed out, pausing sequential processing",
					slog.Int("pr_id", result.PRNumber),
					slog.String("pr_url", result.PRURL),
				)
				// DON'T mark as processed - needs investigation
				time.Sleep(5 * time.Minute)
				continue
			}

			if mergeResult.Merged {
				p.markProcessed(issue.ID)
				continue
			}

			if mergeResult.Declined {
				p.logger.Info("PR was declined without merge",
					slog.Int("pr_id", result.PRNumber),
				)
				// DON'T mark as processed - issue may need re-execution
				continue
			}
		}

		// Direct commit case: no PR to wait for, proceed to next issue
		if result != nil && result.Success && result.PRNumber == 0 {
			p.logger.Info("Direct commit completed, proceeding to next issue",
				slog.Int("issue_id", issue.ID),
				slog.String("commit_sha", result.HeadSHA),
			)
			p.markProcessed(issue.ID)
			continue
		}

		// PR was created but we're not waiting for merge, or no PR was created
		p.markProcessed(issue.ID)
	}
}

// findOldestUnprocessedIssue finds the oldest issue matching the pilot label
// that hasn't been processed yet.
func (p *Poller) findOldestUnprocessedIssue(ctx context.Context) (*Issue, error) {
	issues, err := p.client.ListIssues(ctx, &ListIssuesOptions{
		State: StateNew,
		Sort:  "created_on", // Oldest first
	})
	if err != nil {
		return nil, err
	}

	var candidates []*Issue
	for _, issue := range issues {
		if !p.matchesLabel(issue) {
			continue
		}

		p.mu.RLock()
		processed := p.processed[issue.ID]
		p.mu.RUnlock()

		if processed {
			continue
		}

		candidates = append(candidates, issue)
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CreatedOn.Before(candidates[j].CreatedOn)
	})

	return candidates[0], nil
}

// processIssueSequential processes a single issue and returns PR info
func (p *Poller) processIssueSequential(ctx context.Context, issue *Issue) (*IssueResult, error) {
	if p.onIssueWithResult != nil {
		return p.onIssueWithResult(ctx, issue)
	}

	if p.onIssue != nil {
		err := p.onIssue(ctx, issue)
		if err != nil {
			return &IssueResult{Success: false, Error: err}, err
		}
		return &IssueResult{Success: true}, nil
	}

	return nil, fmt.Errorf("no issue handler configured")
}

// matchesLabel reports whether the issue matches the configured pilot trigger.
// Bitbucket Cloud issues have no free-form labels, so the trigger is matched
// against the issue kind or the synthesized label set.
func (p *Poller) matchesLabel(issue *Issue) bool {
	if issue.Kind == p.label {
		return true
	}
	if HasLabel(issue, p.label) {
		return true
	}
	p.recordSkip(skipreason.ReasonStatusLabel)
	return false
}
