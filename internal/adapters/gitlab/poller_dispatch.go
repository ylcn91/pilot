package gitlab

import (
	"context"
	"log/slog"
	"sort"

	"github.com/ylcn91/pilot/internal/adapters/skipreason"
)

// recordSkip increments the skip counter when pollerMetrics is configured.
func (p *Poller) recordSkip(reason string) {
	if p.pollerMetrics != nil {
		p.pollerMetrics.RecordPollerSkipped(p.repoKey, reason)
	}
}

// recordDispatched increments the dispatch counter when pollerMetrics is configured.
func (p *Poller) recordDispatched() {
	if p.pollerMetrics != nil {
		p.pollerMetrics.RecordPollerDispatched(p.repoKey)
	}
}

// checkForNewIssues fetches issues and dispatches them for parallel execution
func (p *Poller) checkForNewIssues(ctx context.Context) {
	issues, err := p.client.ListIssues(ctx, &ListIssuesOptions{
		Labels:  []string{p.label},
		State:   StateOpened,
		Sort:    "asc", // Oldest first
		OrderBy: "created_at",
	})
	if err != nil {
		p.logger.Warn("Failed to fetch issues", slog.Any("error", err))
		return
	}

	// Sort by creation date (oldest first)
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].CreatedAt.Before(issues[j].CreatedAt)
	})

	for _, issue := range issues {
		// Skip if already processed
		p.mu.RLock()
		processed := p.processed[issue.IID]
		p.mu.RUnlock()

		if processed {
			continue
		}

		// Skip if has in-progress, done, or failed label
		if p.hasStatusLabel(issue) {
			p.markProcessed(issue.IID)
			p.recordSkip(skipreason.ReasonStatusLabel)
			continue
		}

		// Mark processed immediately to prevent duplicate dispatch on next tick
		p.markProcessed(issue.IID)

		// Acquire semaphore slot (blocks if max_concurrent reached)
		select {
		case <-ctx.Done():
			return
		case p.semaphore <- struct{}{}:
		}

		p.recordDispatched()
		p.logger.Info("Dispatching GitLab issue for parallel execution",
			slog.Int("iid", issue.IID),
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

		go p.processIssueAsync(ctx, issue)
	}
}

// processIssueAsync handles a single issue in a goroutine.
// GH-1358: Extracted to enable parallel execution.
func (p *Poller) processIssueAsync(ctx context.Context, issue *Issue) {
	defer p.activeWg.Done()
	defer func() { <-p.semaphore }() // release slot

	if p.onIssueWithResult == nil && p.onIssue == nil {
		return
	}

	// Add in-progress label
	if err := p.client.AddIssueLabels(ctx, issue.IID, []string{LabelInProgress}); err != nil {
		p.logger.Warn("Failed to add in-progress label",
			slog.Int("iid", issue.IID),
			slog.Any("error", err),
		)
	}

	// GH-2232: Check onIssueWithResult first (matches GitHub parallel dispatch pattern)
	if p.onIssueWithResult != nil {
		result, err := p.onIssueWithResult(ctx, issue)
		if err != nil {
			p.logger.Error("Failed to process issue",
				slog.Int("iid", issue.IID),
				slog.Any("error", err),
			)
			_ = p.client.RemoveIssueLabel(ctx, issue.IID, LabelInProgress)
			_ = p.client.AddIssueLabels(ctx, issue.IID, []string{LabelFailed})
			p.ClearProcessed(issue.IID)
			return
		}

		// Unmark if execution failed without creating an MR
		if result != nil && !result.Success && result.MRNumber == 0 {
			p.logger.Info("Execution failed without MR, unmarking for retry",
				slog.Int("iid", issue.IID),
			)
			p.ClearProcessed(issue.IID)
		}

		// Notify autopilot controller of new MR
		if result != nil && result.MRNumber > 0 && p.OnMRCreated != nil {
			p.OnMRCreated(result.MRNumber, result.MRURL, issue.IID, result.HeadSHA, result.BranchName)
		}

		_ = p.client.RemoveIssueLabel(ctx, issue.IID, LabelInProgress)
		_ = p.client.AddIssueLabels(ctx, issue.IID, []string{LabelDone})
		return
	}

	// Legacy fallback: onIssue
	err := p.onIssue(ctx, issue)
	if err != nil {
		p.logger.Error("Failed to process issue",
			slog.Int("iid", issue.IID),
			slog.Any("error", err),
		)
		_ = p.client.RemoveIssueLabel(ctx, issue.IID, LabelInProgress)
		_ = p.client.AddIssueLabels(ctx, issue.IID, []string{LabelFailed})
		return
	}

	_ = p.client.RemoveIssueLabel(ctx, issue.IID, LabelInProgress)
	_ = p.client.AddIssueLabels(ctx, issue.IID, []string{LabelDone})
}

func (p *Poller) hasStatusLabel(issue *Issue) bool {
	return HasLabel(issue, LabelInProgress) ||
		HasLabel(issue, LabelDone) ||
		HasLabel(issue, LabelFailed)
}
