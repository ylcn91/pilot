package asana

import (
	"context"
	"log/slog"
	"sort"
	"strings"
)

// recoverOrphanedTasks finds tasks with pilot-in-progress tag from a previous run
// and removes the tag so they can be picked up again.
// GH-1355: This handles restart/crash scenarios where tasks were left orphaned.
func (p *Poller) recoverOrphanedTasks(ctx context.Context) {
	if p.inProgressTagGID == "" {
		return
	}

	// Get tasks with in-progress tag
	tasks, err := p.client.GetActiveTasksByTag(ctx, p.inProgressTagGID)
	if err != nil {
		p.logger.Warn("Failed to check for orphaned tasks", slog.Any("error", err))
		return
	}

	if len(tasks) == 0 {
		return
	}

	p.logger.Info("Recovering orphaned in-progress tasks",
		slog.Int("count", len(tasks)),
	)

	for _, task := range tasks {
		if err := p.client.RemoveTag(ctx, task.GID, p.inProgressTagGID); err != nil {
			p.logger.Warn("Failed to remove in-progress tag from orphaned task",
				slog.String("gid", task.GID),
				slog.Any("error", err),
			)
			continue
		}
		// GH-2301: Also clear from processed map/store so the first poll cycle picks it up.
		p.ClearProcessed(task.GID)
		p.logger.Info("Recovered orphaned task",
			slog.String("gid", task.GID),
			slog.String("name", task.Name),
		)
	}
}

func (p *Poller) checkForNewTasks(ctx context.Context) {
	// Get tasks with pilot tag
	tasks, err := p.client.GetActiveTasksByTag(ctx, p.pilotTagGID)
	if err != nil {
		p.logger.Warn("Failed to fetch tasks", slog.Any("error", err))
		return
	}

	// Sort by creation date (oldest first)
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})

	for _, task := range tasks {
		// Skip if already processed
		p.mu.RLock()
		processed := p.processed[task.GID]
		p.mu.RUnlock()

		if processed {
			continue
		}

		// Skip if has status tag (in-progress, done, or failed)
		if p.hasStatusTag(&task) {
			p.markProcessed(task.GID)
			continue
		}

		// Mark processed immediately to prevent duplicate dispatch on next tick
		p.markProcessed(task.GID)

		// Acquire semaphore slot (blocks if max_concurrent reached)
		select {
		case <-ctx.Done():
			return
		case p.semaphore <- struct{}{}:
		}

		p.logger.Info("Dispatching Asana task for parallel execution",
			slog.String("gid", task.GID),
			slog.String("name", task.Name),
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

		go p.processTaskAsync(ctx, &task)
	}
}

// processTaskAsync handles a single task in a goroutine.
// GH-1359: Extracted to enable parallel execution.
func (p *Poller) processTaskAsync(ctx context.Context, task *Task) {
	defer p.activeWg.Done()
	defer func() { <-p.semaphore }() // release slot

	if p.onTask == nil {
		return
	}

	// Strip invisible Unicode from untrusted fields before downstream
	// consumers see them. See sanitize.go for the shared helper.
	sanitizeTaskInPlace(task)

	// Add in-progress tag
	if p.inProgressTagGID != "" {
		_ = p.client.AddTag(ctx, task.GID, p.inProgressTagGID)
	}

	result, err := p.onTask(ctx, task)
	if err != nil {
		p.logger.Error("Failed to process task",
			slog.String("gid", task.GID),
			slog.Any("error", err),
		)
		// Remove in-progress tag, add failed tag
		if p.inProgressTagGID != "" {
			_ = p.client.RemoveTag(ctx, task.GID, p.inProgressTagGID)
		}
		if p.failedTagGID != "" {
			_ = p.client.AddTag(ctx, task.GID, p.failedTagGID)
		}
		return
	}

	// Remove in-progress tag
	if p.inProgressTagGID != "" {
		_ = p.client.RemoveTag(ctx, task.GID, p.inProgressTagGID)
	}

	// Add done tag on success
	if result != nil && result.Success && p.doneTagGID != "" {
		_ = p.client.AddTag(ctx, task.GID, p.doneTagGID)
	}
}

// hasStatusTag checks if task has any status tag
func (p *Poller) hasStatusTag(task *Task) bool {
	return p.hasTag(task, TagInProgress) ||
		p.hasTag(task, TagDone) ||
		p.hasTag(task, TagFailed)
}

// hasTag checks if task has a specific tag by name (case-insensitive)
func (p *Poller) hasTag(task *Task, tagName string) bool {
	for _, tag := range task.Tags {
		if strings.EqualFold(tag.Name, tagName) {
			return true
		}
	}
	return false
}
