package azuredevops

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/skipreason"
)

// recoverOrphanedWorkItems finds work items with pilot-in-progress tag from a previous run
// and removes the tag so they can be picked up again.
// GH-1355: This handles restart/crash scenarios where work items were left orphaned.
func (p *Poller) recoverOrphanedWorkItems(ctx context.Context) {
	workItems, err := p.client.ListWorkItems(ctx, &ListWorkItemsOptions{
		Tags:          []string{TagInProgress},
		States:        []string{StateNew, StateActive},
		WorkItemTypes: p.workItemTypes,
	})
	if err != nil {
		p.logger.Warn("Failed to check for orphaned work items", slog.Any("error", err))
		return
	}

	if len(workItems) == 0 {
		return
	}

	p.logger.Info("Recovering orphaned in-progress work items",
		slog.Int("count", len(workItems)),
	)

	for _, wi := range workItems {
		if err := p.client.RemoveWorkItemTag(ctx, wi.ID, TagInProgress); err != nil {
			p.logger.Warn("Failed to remove in-progress tag from orphaned work item",
				slog.Int("id", wi.ID),
				slog.Any("error", err),
			)
			continue
		}
		// GH-2301: Also clear from processed map/store so the first poll cycle picks it up.
		p.ClearProcessed(wi.ID)
		p.logger.Info("Recovered orphaned work item",
			slog.Int("id", wi.ID),
			slog.String("title", wi.GetTitle()),
		)
	}
}

// startSequential runs the sequential execution mode
// Processes one work item at a time, waits for PR merge before next
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

		// Find oldest unprocessed work item
		wi, err := p.findOldestUnprocessedWorkItem(ctx)
		if err != nil {
			p.logger.Warn("Failed to find work items", slog.Any("error", err))
			time.Sleep(p.interval)
			continue
		}

		if wi == nil {
			// No work items to process, wait before checking again
			p.logger.Debug("No unprocessed work items found, waiting...")
			select {
			case <-ctx.Done():
				return
			case <-time.After(p.interval):
				continue
			}
		}

		// Process the work item
		p.logger.Info("Processing work item in sequential mode",
			slog.Int("id", wi.ID),
			slog.String("title", wi.GetTitle()),
		)

		result, err := p.processWorkItemSequential(ctx, wi)
		if err != nil {
			p.logger.Error("Failed to process work item",
				slog.Int("id", wi.ID),
				slog.Any("error", err),
			)
			// Mark as processed to avoid infinite retry loop
			// The work item will have pilot-failed tag
			p.markProcessed(wi.ID)
			continue
		}

		// Notify autopilot controller of new PR (if callback registered)
		if result != nil && result.PRNumber > 0 && p.OnPRCreated != nil {
			p.logger.Info("Notifying autopilot of PR creation",
				slog.Int("pr_id", result.PRNumber),
				slog.Int("work_item_id", wi.ID),
				slog.String("branch", result.BranchName),
			)
			p.OnPRCreated(result.PRNumber, result.PRURL, wi.ID, result.HeadSHA, result.BranchName)
		}

		// If we created a PR and should wait for merge
		if result != nil && result.PRNumber > 0 && p.waitForMerge && p.mergeWaiter != nil {
			p.logger.Info("Waiting for PR merge before next work item",
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
				slog.Bool("abandoned", mergeResult.Abandoned),
				slog.Bool("has_conflicts", mergeResult.HasConflicts),
				slog.Bool("timed_out", mergeResult.TimedOut),
			)

			// Check if PR has conflicts - stop processing
			if mergeResult.HasConflicts {
				p.logger.Warn("PR has conflicts, pausing sequential processing",
					slog.Int("pr_id", result.PRNumber),
					slog.String("pr_url", result.PRURL),
				)
				// DON'T mark as processed - needs manual resolution or rebase
				time.Sleep(5 * time.Minute)
				continue
			}

			// Check if PR timed out
			if mergeResult.TimedOut {
				p.logger.Warn("PR merge timed out, pausing sequential processing",
					slog.Int("pr_id", result.PRNumber),
					slog.String("pr_url", result.PRURL),
				)
				// DON'T mark as processed - needs investigation
				time.Sleep(5 * time.Minute)
				continue
			}

			// Only mark as processed if actually merged
			if mergeResult.Merged {
				p.markProcessed(wi.ID)
				continue
			}

			// PR was abandoned without merge
			if mergeResult.Abandoned {
				p.logger.Info("PR was abandoned without merge",
					slog.Int("pr_id", result.PRNumber),
				)
				// DON'T mark as processed - work item may need re-execution
				continue
			}
		}

		// Direct commit case: no PR to wait for, proceed to next work item
		if result != nil && result.Success && result.PRNumber == 0 {
			p.logger.Info("Direct commit completed, proceeding to next work item",
				slog.Int("work_item_id", wi.ID),
				slog.String("commit_sha", result.HeadSHA),
			)
			p.markProcessed(wi.ID)
			continue
		}

		// PR was created but we're not waiting for merge, or no PR was created
		p.markProcessed(wi.ID)
	}
}

// findOldestUnprocessedWorkItem finds the oldest work item with the pilot tag
// that hasn't been processed yet
func (p *Poller) findOldestUnprocessedWorkItem(ctx context.Context) (*WorkItem, error) {
	workItems, err := p.client.ListWorkItems(ctx, &ListWorkItemsOptions{
		Tags:          []string{p.tag},
		States:        []string{StateNew, StateActive},
		WorkItemTypes: p.workItemTypes,
	})
	if err != nil {
		return nil, err
	}

	// Filter out already processed and in-progress work items
	var candidates []*WorkItem
	for _, wi := range workItems {
		p.mu.RLock()
		processed := p.processed[wi.ID]
		p.mu.RUnlock()

		if processed {
			continue
		}

		if HasTag(wi, TagInProgress) || HasTag(wi, TagDone) {
			p.recordSkip(skipreason.ReasonStatusTag)
			continue
		}

		candidates = append(candidates, wi)
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Sort by creation date (oldest first)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].GetCreatedDate().Before(candidates[j].GetCreatedDate())
	})

	return candidates[0], nil
}

// processWorkItemSequential processes a single work item and returns PR info
func (p *Poller) processWorkItemSequential(ctx context.Context, wi *WorkItem) (*WorkItemResult, error) {
	// Use the new callback if available
	if p.onWorkItemWithResult != nil {
		return p.onWorkItemWithResult(ctx, wi)
	}

	// Fall back to legacy callback
	if p.onWorkItem != nil {
		err := p.onWorkItem(ctx, wi)
		if err != nil {
			return &WorkItemResult{Success: false, Error: err}, err
		}
		return &WorkItemResult{Success: true}, nil
	}

	return nil, fmt.Errorf("no work item handler configured")
}
