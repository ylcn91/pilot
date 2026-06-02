package plane

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
)

// recoverOrphanedIssues finds work items with pilot-in-progress label from a previous run
// and removes the label so they can be picked up again.
// GH-1830: This handles restart/crash scenarios where items were left orphaned.
func (p *Poller) recoverOrphanedIssues(ctx context.Context) {
	if p.inProgressLabelID == "" {
		return
	}

	for _, projectID := range p.config.ProjectIDs {
		items, err := p.client.ListWorkItems(ctx, p.config.WorkspaceSlug, projectID, p.inProgressLabelID)
		if err != nil {
			p.logger.Warn("Failed to check for orphaned issues",
				slog.String("project_id", projectID),
				slog.Any("error", err),
			)
			continue
		}

		if len(items) == 0 {
			continue
		}

		p.logger.Info("Recovering orphaned in-progress issues",
			slog.String("project_id", projectID),
			slog.Int("count", len(items)),
		)

		for _, item := range items {
			if err := p.client.RemoveLabel(ctx, p.config.WorkspaceSlug, projectID, item.ID, p.inProgressLabelID); err != nil {
				p.logger.Warn("Failed to remove in-progress label from orphaned issue",
					slog.String("id", item.ID),
					slog.Any("error", err),
				)
				continue
			}
			// GH-2301: Also clear from processed map/store so the first poll cycle picks it up.
			p.ClearProcessed(item.ID)
			p.logger.Info("Recovered orphaned issue",
				slog.String("id", item.ID),
				slog.String("name", item.Name),
			)
		}
	}
}

func (p *Poller) checkForNewIssues(ctx context.Context) {
	var allItems []WorkItem

	for _, projectID := range p.config.ProjectIDs {
		items, err := p.client.ListWorkItems(ctx, p.config.WorkspaceSlug, projectID, p.pilotLabelID)
		if err != nil {
			p.logger.Warn("Failed to fetch work items",
				slog.String("project_id", projectID),
				slog.Any("error", err),
			)
			continue
		}
		allItems = append(allItems, items...)
	}

	// Sort by creation date (oldest first)
	sort.Slice(allItems, func(i, j int) bool {
		return allItems[i].CreatedAt.Before(allItems[j].CreatedAt)
	})

	for _, item := range allItems {
		// Skip if already processed
		p.mu.RLock()
		processed := p.processed[item.ID]
		p.mu.RUnlock()

		if processed {
			continue
		}

		// Skip if has status label (in-progress, done, or failed)
		if p.hasStatusLabel(&item) {
			// Only mark as processed if it has done label (allow retry of failed)
			if HasLabelID(&item, p.doneLabelID) {
				p.markProcessed(item.ID)
			}
			continue
		}

		// Mark processed immediately to prevent duplicate dispatch on next tick
		p.markProcessed(item.ID)

		// Acquire semaphore slot (blocks if max_concurrent reached)
		select {
		case <-ctx.Done():
			return
		case p.semaphore <- struct{}{}:
		}

		p.logger.Info("Dispatching Plane work item for parallel execution",
			slog.String("id", item.ID),
			slog.String("name", item.Name),
			slog.String("project", item.ProjectID),
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

		go p.processIssueAsync(ctx, item)
	}
}

// processIssueAsync handles a single work item in a goroutine.
// GH-1830: Extracted to enable parallel execution.
func (p *Poller) processIssueAsync(ctx context.Context, item WorkItem) {
	defer p.activeWg.Done()
	defer func() { <-p.semaphore }() // release slot

	if p.onIssue == nil {
		return
	}

	// Strip invisible Unicode from untrusted fields before downstream
	// consumers see them. See sanitize.go for the shared helper.
	sanitizeWorkItemInPlace(&item)

	// Add in-progress label
	if p.inProgressLabelID != "" {
		_ = p.client.AddLabel(ctx, p.config.WorkspaceSlug, item.ProjectID, item.ID, p.inProgressLabelID)
	}

	// GH-1832: Transition to started state on dispatch
	if stateID := p.startedStateIDs[item.ProjectID]; stateID != "" {
		if err := p.client.UpdateIssueState(ctx, p.config.WorkspaceSlug, item.ProjectID, item.ID, stateID); err != nil {
			p.logger.Warn("Failed to transition work item to started state",
				slog.String("id", item.ID),
				slog.Any("error", err),
			)
		}
	}

	result, err := p.onIssue(ctx, &item)
	if err != nil {
		p.logger.Error("Failed to process work item",
			slog.String("id", item.ID),
			slog.Any("error", err),
		)
		// Remove in-progress label, add failed label
		if p.inProgressLabelID != "" {
			_ = p.client.RemoveLabel(ctx, p.config.WorkspaceSlug, item.ProjectID, item.ID, p.inProgressLabelID)
		}
		if p.failedLabelID != "" {
			_ = p.client.AddLabel(ctx, p.config.WorkspaceSlug, item.ProjectID, item.ID, p.failedLabelID)
		}
		// GH-1832: On failure, leave state as-is (user decides)
		return
	}

	// Remove in-progress label
	if p.inProgressLabelID != "" {
		_ = p.client.RemoveLabel(ctx, p.config.WorkspaceSlug, item.ProjectID, item.ID, p.inProgressLabelID)
	}

	// Add done label on success
	if result != nil && result.Success && p.doneLabelID != "" {
		_ = p.client.AddLabel(ctx, p.config.WorkspaceSlug, item.ProjectID, item.ID, p.doneLabelID)
	}

	// GH-1832: Transition to completed state on success
	if result != nil && result.Success {
		if stateID := p.completedStateIDs[item.ProjectID]; stateID != "" {
			if err := p.client.UpdateIssueState(ctx, p.config.WorkspaceSlug, item.ProjectID, item.ID, stateID); err != nil {
				p.logger.Warn("Failed to transition work item to completed state",
					slog.String("id", item.ID),
					slog.Any("error", err),
				)
			}
		}
	}

	// GH-1832: Post PR URL as comment with execution metrics and dedup
	if result != nil && result.Success && result.PRNumber > 0 {
		commentHTML := fmt.Sprintf(
			`<p>✅ PR created: <a href="%s">#%d</a></p>`,
			result.PRURL, result.PRNumber,
		)
		externalID := fmt.Sprintf("pilot-pr-%d-%s", result.PRNumber, item.ID)
		if err := p.client.AddCommentWithTracking(
			ctx, p.config.WorkspaceSlug, item.ProjectID, item.ID,
			commentHTML, "pilot", externalID,
		); err != nil {
			p.logger.Warn("Failed to post PR comment on work item",
				slog.String("id", item.ID),
				slog.Int("pr_number", result.PRNumber),
				slog.Any("error", err),
			)
		}
	}

	// Fire OnPRCreated callback
	if result != nil && result.PRNumber > 0 && p.onPRCreated != nil {
		p.onPRCreated(result.PRNumber, result.PRURL, item.ID, result.HeadSHA, result.BranchName)
	}
}

// hasStatusLabel checks if a work item has any status label UUID.
func (p *Poller) hasStatusLabel(item *WorkItem) bool {
	if p.inProgressLabelID != "" && HasLabelID(item, p.inProgressLabelID) {
		return true
	}
	if p.doneLabelID != "" && HasLabelID(item, p.doneLabelID) {
		return true
	}
	if p.failedLabelID != "" && HasLabelID(item, p.failedLabelID) {
		return true
	}
	return false
}
