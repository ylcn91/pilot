package github

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Cleanup performs a single cleanup pass:
// 1. Lists all issues with pilot-in-progress label and removes stale ones
// 2. Lists all issues with pilot-failed label and removes stale ones
// 3. Cross-references with active executions in memory store
func (c *Cleaner) Cleanup(ctx context.Context) error {
	c.logger.Debug("Running stale label cleanup")

	// Get active executions from memory store
	activeExecutions, err := c.store.GetActiveExecutions()
	if err != nil {
		return fmt.Errorf("failed to get active executions: %w", err)
	}

	// Build a map of active task IDs for quick lookup
	activeTaskIDs := make(map[string]bool)
	for _, exec := range activeExecutions {
		activeTaskIDs[exec.TaskID] = true
	}

	c.logger.Debug("Active executions found", slog.Int("count", len(activeExecutions)))

	// Clean up stale pilot-in-progress labels
	inProgressCleaned, err := c.cleanupLabel(ctx, LabelInProgress, c.threshold, activeTaskIDs)
	if err != nil {
		return fmt.Errorf("failed to cleanup in-progress labels: %w", err)
	}

	// GH-2354: Also clean up pilot-in-progress labels left on CLOSED issues.
	// Externally closed issues (e.g. `gh issue close`) retain the label; the
	// dashboard monitor keeps them in its queue view until the task is pruned.
	closedCleaned, err := c.cleanupClosedInProgressLabels(ctx, activeTaskIDs)
	if err != nil {
		return fmt.Errorf("failed to cleanup closed in-progress labels: %w", err)
	}

	// Clean up stale pilot-failed labels
	failedCleaned, err := c.cleanupLabel(ctx, LabelFailed, c.failedThreshold, activeTaskIDs)
	if err != nil {
		return fmt.Errorf("failed to cleanup failed labels: %w", err)
	}

	// GH-2402: Clean up stale pilot-blocked labels. Blocked issues are paused
	// until human intervention, but the same staleness threshold as pilot-failed
	// is applied as a safety net so a forgotten label doesn't strand work forever.
	blockedCleaned, err := c.cleanupLabel(ctx, LabelBlocked, c.failedThreshold, activeTaskIDs)
	if err != nil {
		return fmt.Errorf("failed to cleanup blocked labels: %w", err)
	}

	totalCleaned := inProgressCleaned + closedCleaned + failedCleaned + blockedCleaned
	if totalCleaned > 0 {
		c.logger.Info("Stale label cleanup completed",
			slog.Int("in_progress_cleaned", inProgressCleaned),
			slog.Int("closed_in_progress_cleaned", closedCleaned),
			slog.Int("failed_cleaned", failedCleaned),
			slog.Int("blocked_cleaned", blockedCleaned),
		)
	}

	return nil
}

// cleanupLabel cleans up a specific label type and returns count of cleaned issues
func (c *Cleaner) cleanupLabel(ctx context.Context, label string, threshold time.Duration, activeTaskIDs map[string]bool) (int, error) {
	issues, err := c.client.ListIssues(ctx, c.owner, c.repo, &ListIssuesOptions{
		Labels: []string{label},
		State:  StateOpen,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to list issues with %s label: %w", label, err)
	}

	if len(issues) == 0 {
		c.logger.Debug("No issues found with label", slog.String("label", label))
		return 0, nil
	}

	c.logger.Debug("Found issues with label",
		slog.String("label", label),
		slog.Int("count", len(issues)),
	)

	cleanedCount := 0
	for _, issue := range issues {
		// Check if there's an active execution for this issue
		taskID := fmt.Sprintf("GH-%d", issue.Number)
		if activeTaskIDs[taskID] {
			c.logger.Debug("Issue has active execution, skipping",
				slog.Int("issue", issue.Number),
				slog.String("task_id", taskID),
				slog.String("label", label),
			)
			continue
		}

		// Check if the issue's label update is older than threshold
		if time.Since(issue.UpdatedAt) < threshold {
			c.logger.Debug("Issue recently updated, skipping",
				slog.Int("issue", issue.Number),
				slog.Duration("age", time.Since(issue.UpdatedAt)),
				slog.Duration("threshold", threshold),
				slog.String("label", label),
			)
			continue
		}

		// Remove the stale label
		c.logger.Info("Removing stale label",
			slog.String("label", label),
			slog.Int("issue", issue.Number),
			slog.String("title", issue.Title),
			slog.Duration("age", time.Since(issue.UpdatedAt)),
		)

		if err := c.client.RemoveLabel(ctx, c.owner, c.repo, issue.Number, label); err != nil {
			c.logger.Warn("Failed to remove stale label",
				slog.Int("issue", issue.Number),
				slog.String("label", label),
				slog.Any("error", err),
			)
			continue
		}

		// Add a comment explaining the cleanup
		var comment string
		switch label {
		case LabelInProgress:
			comment = "🧹 **Pilot cleanup**: Removed stale `pilot-in-progress` label.\n\n" +
				"This issue was marked as in-progress but no active Pilot execution was found. " +
				"This can happen if Pilot was interrupted or crashed. The issue is now available for processing again."
		case LabelFailed:
			comment = "🧹 **Pilot cleanup**: Removed stale `pilot-failed` label.\n\n" +
				"This issue was marked as failed but has been stale for over 24 hours. " +
				"The label has been removed to allow Pilot to retry this issue automatically."
		case LabelBlocked:
			comment = "🧹 **Pilot cleanup**: Removed stale `pilot-blocked` label.\n\n" +
				"This issue was paused on a deterministic failure (e.g. non-conventional title) but has " +
				"been stale for the configured threshold. The label has been removed so Pilot can retry."
		}

		if _, err := c.client.AddComment(ctx, c.owner, c.repo, issue.Number, comment); err != nil {
			c.logger.Warn("Failed to add cleanup comment",
				slog.Int("issue", issue.Number),
				slog.Any("error", err),
			)
		}

		// Notify callbacks so the poller can clear its processed map and pick up the issue again.
		switch label {
		case LabelFailed:
			if c.OnFailedCleaned != nil {
				c.OnFailedCleaned(issue.Number)
			}
		case LabelBlocked:
			if c.OnBlockedCleaned != nil {
				c.OnBlockedCleaned(issue.Number)
			}
		}

		cleanedCount++
	}

	return cleanedCount, nil
}

// cleanupClosedInProgressLabels removes the pilot-in-progress label from
// issues that are CLOSED on GitHub but still carry the label. This happens
// when an issue is closed externally (e.g. `gh issue close`) without the
// label being cleared. The dashboard monitor treats such tasks as live and
// keeps them in the queue view — GH-2354.
//
// No staleness threshold is applied: a closed issue should never carry the
// in-progress label, so we clean immediately on discovery. Active executions
// (tracked in the memory store) are still skipped so an in-flight run isn't
// silently stripped while it's still working.
func (c *Cleaner) cleanupClosedInProgressLabels(ctx context.Context, activeTaskIDs map[string]bool) (int, error) {
	issues, err := c.client.ListIssues(ctx, c.owner, c.repo, &ListIssuesOptions{
		Labels: []string{LabelInProgress},
		State:  StateClosed,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to list closed issues with %s label: %w", LabelInProgress, err)
	}

	if len(issues) == 0 {
		return 0, nil
	}

	c.logger.Debug("Found closed issues with in-progress label",
		slog.Int("count", len(issues)),
	)

	cleanedCount := 0
	for _, issue := range issues {
		// Defensive: only act on issues that are genuinely closed. GitHub's
		// real API honours the state=closed filter, but we don't want to
		// strip labels from open issues if the response is ambiguous.
		if issue.State != StateClosed {
			continue
		}

		taskID := fmt.Sprintf("GH-%d", issue.Number)
		if activeTaskIDs[taskID] {
			c.logger.Debug("Closed issue has active execution, skipping",
				slog.Int("issue", issue.Number),
				slog.String("task_id", taskID),
			)
			continue
		}

		c.logger.Info("Removing in-progress label from closed issue",
			slog.Int("issue", issue.Number),
			slog.String("title", issue.Title),
		)

		if err := c.client.RemoveLabel(ctx, c.owner, c.repo, issue.Number, LabelInProgress); err != nil {
			c.logger.Warn("Failed to remove in-progress label from closed issue",
				slog.Int("issue", issue.Number),
				slog.Any("error", err),
			)
			continue
		}

		if c.OnInProgressCleaned != nil {
			c.OnInProgressCleaned(issue.Number)
		}

		cleanedCount++
	}

	return cleanedCount, nil
}

// CleanupStaleLabels is a convenience method that performs a single cleanup
// without starting the periodic loop. Useful for one-off cleanup operations.
func (c *Cleaner) CleanupStaleLabels(ctx context.Context) error {
	return c.Cleanup(ctx)
}

// StartupRecover scans for open issues carrying the pilot-in-progress label
// that have no live execution row in the store. These issues are stuck from a
// previous daemon run (e.g. daemon was killed while a task was in flight) and
// must be unlabeled so they return to the queue on the next poll cycle.
//
// N=30min staleness floor: an execution row created less than 30 minutes ago is
// treated as live even if the daemon just restarted — the previous process may
// have exited seconds ago and the row status may not yet reflect completion.
// Rows older than 30 minutes with status=running are considered orphaned and
// the corresponding label is stripped.
//
// Returns the number of issues recovered and any error encountered.
// GH-2589.
func (c *Cleaner) StartupRecover(ctx context.Context) (int, error) {
	const staleThreshold = 30 * time.Minute

	activeExecutions, err := c.store.GetActiveExecutions()
	if err != nil {
		return 0, fmt.Errorf("startup recover: get active executions: %w", err)
	}

	// Index running executions that were created recently enough to be live.
	liveByTaskID := make(map[string]bool)
	for _, e := range activeExecutions {
		if time.Since(e.CreatedAt) < staleThreshold {
			liveByTaskID[e.TaskID] = true
		}
	}

	issues, err := c.client.ListIssues(ctx, c.owner, c.repo, &ListIssuesOptions{
		Labels: []string{LabelInProgress},
		State:  StateOpen,
	})
	if err != nil {
		return 0, fmt.Errorf("startup recover: list issues: %w", err)
	}

	cleaned := 0
	for _, issue := range issues {
		taskID := fmt.Sprintf("GH-%d", issue.Number)
		if liveByTaskID[taskID] {
			c.logger.Debug("startup recover: live execution found, skipping",
				slog.String("task_id", taskID),
			)
			continue
		}

		c.logger.Info("startup recover: removing stuck pilot-in-progress label",
			slog.Int("issue", issue.Number),
			slog.String("title", issue.Title),
		)

		if err := c.client.RemoveLabel(ctx, c.owner, c.repo, issue.Number, LabelInProgress); err != nil {
			c.logger.Warn("startup recover: failed to remove label",
				slog.Int("issue", issue.Number),
				slog.Any("error", err),
			)
			continue
		}

		if c.OnStartupRecovered != nil {
			c.OnStartupRecovered(issue.Number)
		}
		if c.OnInProgressCleaned != nil {
			c.OnInProgressCleaned(issue.Number)
		}

		cleaned++
	}

	c.logger.Info("daemon-startup recovery: cleaned up stuck pilot-in-progress labels",
		slog.Int("count", cleaned),
	)

	return cleaned, nil
}
