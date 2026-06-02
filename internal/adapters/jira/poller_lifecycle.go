package jira

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// Start begins polling for issues
func (p *Poller) Start(ctx context.Context) error {
	p.logger.Info("Starting Jira poller",
		slog.String("label", p.pilotLabel),
		slog.String("project", p.config.ProjectKey),
		slog.Duration("interval", p.interval),
		slog.Int("max_concurrent", p.maxConcurrent),
	)

	// GH-1355: Recover orphaned in-progress issues from previous run before starting poll loop
	p.recoverOrphanedIssues(ctx)

	// Initial check
	p.checkForNewIssues(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Jira poller stopping, waiting for active tasks...")
			p.wgMu.Lock()
			p.stopping.Store(true)
			p.wgMu.Unlock()
			p.activeWg.Wait()
			p.logger.Info("Jira poller stopped")
			return nil
		case <-ticker.C:
			p.checkForNewIssues(ctx)
		}
	}
}

// buildJQL constructs the JQL query for finding pilot issues
func (p *Poller) buildJQL() string {
	var parts []string

	// Filter by label
	parts = append(parts, fmt.Sprintf("labels = \"%s\"", p.pilotLabel))

	// Filter by project if configured
	if p.config.ProjectKey != "" {
		parts = append(parts, fmt.Sprintf("project = \"%s\"", p.config.ProjectKey))
	}

	// Exclude done/closed statuses (using status category for broader coverage)
	parts = append(parts, "statusCategory != Done")

	// Order by created date (oldest first)
	jql := strings.Join(parts, " AND ") + " ORDER BY created ASC"

	return jql
}

// recoverOrphanedIssues finds issues with pilot-in-progress label from a previous run
// and removes the label so they can be picked up again.
// GH-1355: This handles restart/crash scenarios where issues were left orphaned.
func (p *Poller) recoverOrphanedIssues(ctx context.Context) {
	// Build JQL to find in-progress issues
	var parts []string
	parts = append(parts, fmt.Sprintf("labels = \"%s\"", LabelInProgress))
	if p.config.ProjectKey != "" {
		parts = append(parts, fmt.Sprintf("project = \"%s\"", p.config.ProjectKey))
	}
	parts = append(parts, "statusCategory != Done")
	jql := strings.Join(parts, " AND ")

	issues, err := p.client.SearchIssues(ctx, jql, 50)
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
		if err := p.client.RemoveLabel(ctx, issue.Key, LabelInProgress); err != nil {
			p.logger.Warn("Failed to remove in-progress label from orphaned issue",
				slog.String("key", issue.Key),
				slog.Any("error", err),
			)
			continue
		}
		// GH-2301: Also clear from processed map/store so the first poll cycle picks it up.
		p.ClearProcessed(issue.Key)
		p.logger.Info("Recovered orphaned issue",
			slog.String("key", issue.Key),
			slog.String("summary", issue.Fields.Summary),
		)
	}
}

func (p *Poller) checkForNewIssues(ctx context.Context) {
	jql := p.buildJQL()
	issues, err := p.client.SearchIssues(ctx, jql, 50)
	if err != nil {
		p.logger.Warn("Failed to fetch issues", slog.Any("error", err))
		return
	}

	// Sort by creation date (oldest first) - API should return sorted, but ensure it
	sort.Slice(issues, func(i, j int) bool {
		// Parse Jira's datetime format
		ti, _ := time.Parse("2006-01-02T15:04:05.000-0700", issues[i].Fields.Created)
		tj, _ := time.Parse("2006-01-02T15:04:05.000-0700", issues[j].Fields.Created)
		return ti.Before(tj)
	})

	for _, issue := range issues {
		// Skip if already processed
		p.mu.RLock()
		processed := p.processed[issue.Key]
		p.mu.RUnlock()

		if processed {
			continue
		}

		// Skip if has in-progress, done, or failed label
		if p.hasStatusLabel(issue) {
			// Only mark as processed if it has done label (allow retry of failed)
			if p.client.HasLabel(issue, LabelDone) {
				p.markProcessed(issue.Key)
			}
			continue
		}

		// Mark processed immediately to prevent duplicate dispatch on next tick
		p.markProcessed(issue.Key)

		// Acquire semaphore slot (blocks if max_concurrent reached)
		select {
		case <-ctx.Done():
			return
		case p.semaphore <- struct{}{}:
		}

		p.logger.Info("Dispatching Jira issue for parallel execution",
			slog.String("key", issue.Key),
			slog.String("summary", issue.Fields.Summary),
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

	// Add in-progress label
	if err := p.client.AddLabel(ctx, issue.Key, LabelInProgress); err != nil {
		p.logger.Warn("Failed to add in-progress label",
			slog.String("key", issue.Key),
			slog.Any("error", err),
		)
	}

	result, err := p.onIssue(ctx, issue)
	if err != nil {
		p.logger.Error("Failed to process issue",
			slog.String("key", issue.Key),
			slog.Any("error", err),
		)
		// Remove in-progress label, add failed label
		_ = p.client.RemoveLabel(ctx, issue.Key, LabelInProgress)
		_ = p.client.AddLabel(ctx, issue.Key, LabelFailed)
		return
	}

	// Remove in-progress label
	_ = p.client.RemoveLabel(ctx, issue.Key, LabelInProgress)

	// Add done label on success
	if result != nil && result.Success {
		_ = p.client.AddLabel(ctx, issue.Key, LabelDone)
	}
}

func (p *Poller) hasStatusLabel(issue *Issue) bool {
	return p.client.HasLabel(issue, LabelInProgress) ||
		p.client.HasLabel(issue, LabelDone) ||
		p.client.HasLabel(issue, LabelFailed)
}
