package azuredevops

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/skipreason"
)

// startParallel runs the parallel execution mode with goroutine dispatch
func (p *Poller) startParallel(ctx context.Context) {
	// Do an initial check immediately
	p.checkForNewWorkItems(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Azure DevOps poller stopping, waiting for active tasks...")
			p.wgMu.Lock()
			p.stopping.Store(true)
			p.wgMu.Unlock()
			p.activeWg.Wait()
			p.logger.Info("Azure DevOps poller stopped")
			return
		case <-ticker.C:
			p.checkForNewWorkItems(ctx)
		}
	}
}

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

// checkForNewWorkItems fetches work items and dispatches them for parallel execution
func (p *Poller) checkForNewWorkItems(ctx context.Context) {
	workItems, err := p.client.ListWorkItems(ctx, &ListWorkItemsOptions{
		Tags:          []string{p.tag},
		States:        []string{StateNew, StateActive},
		WorkItemTypes: p.workItemTypes,
	})
	if err != nil {
		p.logger.Warn("Failed to fetch work items", slog.Any("error", err))
		return
	}

	// Sort by creation date (oldest first)
	sort.Slice(workItems, func(i, j int) bool {
		return workItems[i].GetCreatedDate().Before(workItems[j].GetCreatedDate())
	})

	for _, wi := range workItems {
		// Skip if already processed
		p.mu.RLock()
		processed := p.processed[wi.ID]
		p.mu.RUnlock()

		if processed {
			continue
		}

		// Skip if has in-progress, done, or failed tag
		if p.hasStatusTag(wi) {
			p.markProcessed(wi.ID)
			p.recordSkip(skipreason.ReasonStatusTag)
			continue
		}

		// Mark processed immediately to prevent duplicate dispatch on next tick
		p.markProcessed(wi.ID)

		// Acquire semaphore slot (blocks if max_concurrent reached)
		select {
		case <-ctx.Done():
			return
		case p.semaphore <- struct{}{}:
		}

		p.recordDispatched()
		p.logger.Info("Dispatching Azure DevOps work item for parallel execution",
			slog.Int("id", wi.ID),
			slog.String("title", wi.GetTitle()),
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

		go p.processWorkItemAsync(ctx, wi)
	}
}

// processWorkItemAsync handles a single work item in a goroutine.
// GH-1358: Extracted to enable parallel execution.
func (p *Poller) processWorkItemAsync(ctx context.Context, wi *WorkItem) {
	defer p.activeWg.Done()
	defer func() { <-p.semaphore }() // release slot

	if p.onWorkItem == nil {
		return
	}

	// Add in-progress tag
	if err := p.client.AddWorkItemTag(ctx, wi.ID, TagInProgress); err != nil {
		p.logger.Warn("Failed to add in-progress tag",
			slog.Int("id", wi.ID),
			slog.Any("error", err),
		)
	}

	err := p.onWorkItem(ctx, wi)
	if err != nil {
		p.logger.Error("Failed to process work item",
			slog.Int("id", wi.ID),
			slog.Any("error", err),
		)
		// Remove in-progress tag, add failed tag
		_ = p.client.RemoveWorkItemTag(ctx, wi.ID, TagInProgress)
		_ = p.client.AddWorkItemTag(ctx, wi.ID, TagFailed)
		return
	}

	// Remove in-progress tag
	_ = p.client.RemoveWorkItemTag(ctx, wi.ID, TagInProgress)

	// Add done tag on success
	_ = p.client.AddWorkItemTag(ctx, wi.ID, TagDone)
}

func (p *Poller) hasStatusTag(wi *WorkItem) bool {
	return HasTag(wi, TagInProgress) ||
		HasTag(wi, TagDone) ||
		HasTag(wi, TagFailed)
}
