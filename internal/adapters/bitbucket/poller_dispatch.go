package bitbucket

import (
	"context"
	"log/slog"
	"sort"
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
		State: StateNew,
		Sort:  "created_on", // Oldest first
	})
	if err != nil {
		p.logger.Warn("Failed to fetch issues", slog.Any("error", err))
		return
	}

	sort.Slice(issues, func(i, j int) bool {
		return issues[i].CreatedOn.Before(issues[j].CreatedOn)
	})

	for _, issue := range issues {
		if !p.matchesLabel(issue) {
			continue
		}

		// Skip if already processed
		p.mu.RLock()
		processed := p.processed[issue.ID]
		p.mu.RUnlock()

		if processed {
			continue
		}

		// Mark processed immediately to prevent duplicate dispatch on next tick
		p.markProcessed(issue.ID)

		// Acquire semaphore slot (blocks if max_concurrent reached)
		select {
		case <-ctx.Done():
			return
		case p.semaphore <- struct{}{}:
		}

		p.recordDispatched()
		p.logger.Info("Dispatching Bitbucket issue for parallel execution",
			slog.Int("id", issue.ID),
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
func (p *Poller) processIssueAsync(ctx context.Context, issue *Issue) {
	defer p.activeWg.Done()
	defer func() { <-p.semaphore }() // release slot

	if p.onIssueWithResult == nil && p.onIssue == nil {
		return
	}

	if p.onIssueWithResult != nil {
		result, err := p.onIssueWithResult(ctx, issue)
		if err != nil {
			p.logger.Error("Failed to process issue",
				slog.Int("id", issue.ID),
				slog.Any("error", err),
			)
			p.ClearProcessed(issue.ID)
			return
		}

		// Unmark if execution failed without creating a PR
		if result != nil && !result.Success && result.PRNumber == 0 {
			p.logger.Info("Execution failed without PR, unmarking for retry",
				slog.Int("id", issue.ID),
			)
			p.ClearProcessed(issue.ID)
		}

		// Notify autopilot controller of new PR
		if result != nil && result.PRNumber > 0 && p.OnPRCreated != nil {
			p.OnPRCreated(result.PRNumber, result.PRURL, issue.ID, result.HeadSHA, result.BranchName)
		}
		return
	}

	// Legacy fallback: onIssue
	if err := p.onIssue(ctx, issue); err != nil {
		p.logger.Error("Failed to process issue",
			slog.Int("id", issue.ID),
			slog.Any("error", err),
		)
		p.ClearProcessed(issue.ID)
	}
}
