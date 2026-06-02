package linear

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
)

func (p *Poller) cacheLabelIDs(ctx context.Context) error {
	var err error

	p.pilotLabelID, err = p.client.GetLabelByName(ctx, p.config.TeamID, p.config.PilotLabel)
	if err != nil {
		return fmt.Errorf("pilot label: %w", err)
	}

	// GH-1351: Auto-create status labels if they don't exist.
	// These labels are required for deduplication after hot upgrade.
	// Colors chosen to match typical status semantics: blue=in-progress, green=done, red=failed
	p.inProgressLabelID, err = p.client.GetOrCreateLabel(ctx, p.config.TeamID, "pilot-in-progress", "#0066FF")
	if err != nil {
		p.logger.Warn("Failed to get/create pilot-in-progress label", slog.Any("error", err))
	}

	p.doneLabelID, err = p.client.GetOrCreateLabel(ctx, p.config.TeamID, "pilot-done", "#00AA55")
	if err != nil {
		p.logger.Warn("Failed to get/create pilot-done label", slog.Any("error", err))
	}

	p.failedLabelID, err = p.client.GetOrCreateLabel(ctx, p.config.TeamID, "pilot-failed", "#DD0000")
	if err != nil {
		p.logger.Warn("Failed to get/create pilot-failed label", slog.Any("error", err))
	}

	return nil
}

// recoverOrphanedIssues finds issues with pilot-in-progress label from a previous run
// and removes the label so they can be picked up again.
// GH-1355: This handles restart/crash scenarios where issues were left orphaned.
func (p *Poller) recoverOrphanedIssues(ctx context.Context) {
	if p.inProgressLabelID == "" {
		return
	}

	// List issues that have both the pilot label and in-progress label
	issues, err := p.client.ListIssues(ctx, &ListIssuesOptions{
		TeamID:     p.config.TeamID,
		Label:      "pilot-in-progress",
		ProjectIDs: p.config.ProjectIDs,
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
		if err := p.client.RemoveLabel(ctx, issue.ID, p.inProgressLabelID); err != nil {
			p.logger.Warn("Failed to remove in-progress label from orphaned issue",
				slog.String("identifier", issue.Identifier),
				slog.Any("error", err),
			)
			continue
		}
		// GH-2301: Also clear from processed map/store so the first poll cycle picks it up.
		p.ClearProcessed(issue.ID)
		p.logger.Info("Recovered orphaned issue",
			slog.String("identifier", issue.Identifier),
			slog.String("title", issue.Title),
		)
	}
}

func (p *Poller) checkForNewIssues(ctx context.Context) {
	issues, err := p.client.ListIssues(ctx, &ListIssuesOptions{
		TeamID:     p.config.TeamID,
		Label:      p.config.PilotLabel,
		ProjectIDs: p.config.ProjectIDs,
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
		processed := p.processed[issue.ID]
		p.mu.RUnlock()

		if processed {
			continue
		}

		// Skip if has in-progress, done, or failed label
		if p.hasStatusLabel(issue) {
			p.markProcessed(issue.ID)
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

		p.logger.Info("Dispatching Linear issue for parallel execution",
			slog.String("identifier", issue.Identifier),
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
// GH-1357: Extracted to enable parallel execution.
func (p *Poller) processIssueAsync(ctx context.Context, issue *Issue) {
	defer p.activeWg.Done()
	defer func() { <-p.semaphore }() // release slot

	if p.onIssue == nil {
		return
	}

	// Strip invisible Unicode from untrusted fields before downstream
	// consumers see them. See sanitize.go for the shared helper.
	sanitizeIssueInPlace(issue)

	// Add in-progress label
	if p.inProgressLabelID != "" {
		_ = p.client.AddLabel(ctx, issue.ID, p.inProgressLabelID)
	}

	result, err := p.onIssue(ctx, issue)
	if err != nil {
		p.logger.Error("Failed to process issue",
			slog.String("identifier", issue.Identifier),
			slog.Any("error", err),
		)
		// Remove in-progress label, add failed label
		if p.inProgressLabelID != "" {
			_ = p.client.RemoveLabel(ctx, issue.ID, p.inProgressLabelID)
		}
		if p.failedLabelID != "" {
			_ = p.client.AddLabel(ctx, issue.ID, p.failedLabelID)
		}
		return
	}

	// Remove in-progress label
	if p.inProgressLabelID != "" {
		_ = p.client.RemoveLabel(ctx, issue.ID, p.inProgressLabelID)
	}

	// Add done label on success
	if result != nil && result.Success && p.doneLabelID != "" {
		_ = p.client.AddLabel(ctx, issue.ID, p.doneLabelID)
	}

	// GH-1700: Notify autopilot controller about new PR
	if result != nil && result.PRNumber > 0 && p.OnPRCreated != nil {
		p.OnPRCreated(result.PRNumber, result.PRURL, 0, result.HeadSHA, result.BranchName, "")
	}
}

func (p *Poller) hasStatusLabel(issue *Issue) bool {
	for _, label := range issue.Labels {
		switch label.Name {
		case "pilot-in-progress", "pilot-done", "pilot-failed":
			return true
		}
	}
	return false
}
