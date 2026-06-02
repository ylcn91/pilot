package alerts

import (
	"context"
	"fmt"
	"time"
)

// handleEvent processes a single event
func (e *Engine) handleEvent(ctx context.Context, event Event) {
	if event.testFlushResp != nil {
		close(event.testFlushResp)
		return
	}
	switch event.Type {
	case EventTypeTaskStarted:
		e.handleTaskStarted(event)
	case EventTypeTaskProgress:
		e.handleTaskProgress(event)
	case EventTypeTaskCompleted:
		e.handleTaskCompleted(ctx, event)
	case EventTypeTaskFailed, EventTypeOOMKilled:
		// GH-2332: OOM kills are a strict subset of failures — route through
		// the same handler so consecutive-failure counters and escalation
		// rules fire, but preserve the distinct type for logging/metadata.
		e.handleTaskFailed(ctx, event)
	case EventTypeCostUpdate:
		e.handleCostUpdate(ctx, event)
	case EventTypeSecurityEvent:
		e.handleSecurityEvent(ctx, event)
	case EventTypeBudgetExceeded, EventTypeBudgetWarning:
		e.handleBudgetEvent(ctx, event)
	case EventTypeAutopilotMetrics:
		e.handleAutopilotMetrics(ctx, event)
	case EventTypeEscalation:
		e.handleEscalation(ctx, event)
	}
}

func (e *Engine) handleTaskStarted(event Event) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.taskLastProgress[event.TaskID] = progressState{
		Progress:  0,
		UpdatedAt: event.Timestamp,
		Phase:     event.Phase,
	}
}

func (e *Engine) handleTaskProgress(event Event) {
	e.mu.Lock()
	defer e.mu.Unlock()

	current, exists := e.taskLastProgress[event.TaskID]
	if !exists || event.Progress > current.Progress || event.Phase != current.Phase {
		e.taskLastProgress[event.TaskID] = progressState{
			Progress:  event.Progress,
			UpdatedAt: event.Timestamp,
			Phase:     event.Phase,
			// Reset per-task alert cooldown when progress advances (GH-2204)
			LastAlertedAt: time.Time{},
		}
	}
}

func (e *Engine) handleTaskCompleted(ctx context.Context, event Event) {
	// Determine source for retry tracking (GH-848)
	source := event.TaskID
	if s, ok := event.Metadata["source"]; ok && s != "" {
		source = s
	}

	e.mu.Lock()
	// Reset consecutive failures on success
	e.consecutiveFailures[event.Project] = 0
	delete(e.taskLastProgress, event.TaskID)
	// Reset per-source retry counter on success (GH-848)
	delete(e.retryTracker, source)
	e.mu.Unlock()
}

func (e *Engine) handleTaskFailed(ctx context.Context, event Event) {
	// Determine source for retry tracking (GH-848)
	// Source can be passed in Metadata["source"] or default to TaskID
	source := event.TaskID
	if s, ok := event.Metadata["source"]; ok && s != "" {
		source = s
	}

	e.mu.Lock()
	delete(e.taskLastProgress, event.TaskID)
	e.consecutiveFailures[event.Project]++
	failCount := e.consecutiveFailures[event.Project]

	// Track per-source retries (GH-848)
	e.retryTracker[source]++
	retryCount := e.retryTracker[source]
	e.mu.Unlock()

	// Check task_failed rule
	for _, rule := range e.config.Rules {
		if !rule.Enabled {
			continue
		}

		switch rule.Type {
		case AlertTypeTaskFailed:
			if e.shouldFire(rule) {
				alert := e.createAlert(rule, event, fmt.Sprintf("Task %s failed: %s", event.TaskID, event.Error))
				e.fireAlert(ctx, rule, alert)
			}

		case AlertTypeConsecutiveFails:
			if failCount >= rule.Condition.ConsecutiveFailures && e.shouldFire(rule) {
				alert := e.createAlert(rule, event,
					fmt.Sprintf("%d consecutive task failures in project %s", failCount, event.Project))
				e.fireAlert(ctx, rule, alert)
			}

		case AlertTypeEscalation:
			// Escalate to PagerDuty after N consecutive failures for the same source (GH-848)
			threshold := rule.Condition.EscalationRetries
			if threshold == 0 {
				threshold = 3 // Default
			}
			if retryCount >= threshold && e.shouldFire(rule) {
				alert := e.createEscalationAlert(rule, event, source, retryCount)
				e.fireAlert(ctx, rule, alert)
			}
		}
	}
}

func (e *Engine) handleCostUpdate(ctx context.Context, event Event) {
	dailySpend := 0.0
	if v, ok := event.Metadata["daily_spend"]; ok {
		_, _ = fmt.Sscanf(v, "%f", &dailySpend)
	}

	for _, rule := range e.config.Rules {
		if !rule.Enabled {
			continue
		}

		switch rule.Type {
		case AlertTypeDailySpend:
			if dailySpend > rule.Condition.DailySpendThreshold && e.shouldFire(rule) {
				alert := e.createAlert(rule, event,
					fmt.Sprintf("Daily spend $%.2f exceeds threshold $%.2f",
						dailySpend, rule.Condition.DailySpendThreshold))
				e.fireAlert(ctx, rule, alert)
			}

		case AlertTypeBudgetDepleted:
			totalSpend := 0.0
			if v, ok := event.Metadata["total_spend"]; ok {
				_, _ = fmt.Sscanf(v, "%f", &totalSpend)
			}
			if totalSpend > rule.Condition.BudgetLimit && e.shouldFire(rule) {
				alert := e.createAlert(rule, event,
					fmt.Sprintf("Budget limit $%.2f exceeded (current: $%.2f)",
						rule.Condition.BudgetLimit, totalSpend))
				e.fireAlert(ctx, rule, alert)
			}
		}
	}
}

func (e *Engine) handleBudgetEvent(ctx context.Context, event Event) {
	// Route budget events through cost update handler so existing
	// AlertTypeDailySpend / AlertTypeBudgetDepleted rules fire
	e.handleCostUpdate(ctx, event)
}

func (e *Engine) handleSecurityEvent(ctx context.Context, event Event) {
	for _, rule := range e.config.Rules {
		if !rule.Enabled {
			continue
		}

		switch rule.Type {
		case AlertTypeUnauthorizedAccess:
			if e.shouldFire(rule) {
				alert := e.createAlert(rule, event, "Unauthorized access attempt detected")
				e.fireAlert(ctx, rule, alert)
			}
		case AlertTypeSensitiveFile:
			if e.shouldFire(rule) {
				filePath := event.Metadata["file_path"]
				alert := e.createAlert(rule, event,
					fmt.Sprintf("Sensitive file modified: %s", filePath))
				e.fireAlert(ctx, rule, alert)
			}
		}
	}
}

// checkStuckTasks periodically checks for stuck tasks
func (e *Engine) checkStuckTasks(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.done:
			return
		case <-ticker.C:
			e.evaluateStuckTasks(ctx)
		}
	}
}

func (e *Engine) evaluateStuckTasks(ctx context.Context) {
	now := time.Now()

	// Collect orphan IDs under read lock, then evict under write lock (GH-2204)
	e.mu.RLock()
	tasks := make(map[string]progressState)
	var orphans []string
	for k, v := range e.taskLastProgress {
		tasks[k] = v
	}
	e.mu.RUnlock()

	for _, rule := range e.config.Rules {
		if !rule.Enabled || rule.Type != AlertTypeTaskStuck {
			continue
		}

		threshold := rule.Condition.ProgressUnchangedFor
		if threshold == 0 {
			threshold = 10 * time.Minute
		}

		cooldown := rule.Cooldown
		orphanThreshold := 4 * threshold // Evict entries stuck for 4× the threshold (GH-2204)

		for taskID, state := range tasks {
			stuckDuration := now.Sub(state.UpdatedAt)

			// Orphan eviction: remove entries that have been stuck far too long (GH-2204)
			if stuckDuration > orphanThreshold {
				orphans = append(orphans, taskID)
				e.logger.Warn("evicting orphaned stuck-task entry",
					"task_id", taskID,
					"stuck_for", stuckDuration.Round(time.Minute),
					"orphan_threshold", orphanThreshold,
				)
				continue
			}

			if stuckDuration <= threshold {
				continue
			}

			// Per-task cooldown: skip if already alerted recently for THIS task (GH-2204)
			if !state.LastAlertedAt.IsZero() && cooldown > 0 && now.Sub(state.LastAlertedAt) < cooldown {
				continue
			}

			event := Event{
				Type:      EventTypeTaskProgress,
				TaskID:    taskID,
				Phase:     state.Phase,
				Progress:  state.Progress,
				Timestamp: now,
			}
			alert := e.createAlert(rule, event,
				fmt.Sprintf("Task %s stuck at %d%% (%s) for %v",
					taskID, state.Progress, state.Phase, stuckDuration.Round(time.Minute)))
			e.fireAlert(ctx, rule, alert)

			// Record per-task alert time (GH-2204)
			e.mu.Lock()
			if s, ok := e.taskLastProgress[taskID]; ok {
				s.LastAlertedAt = now
				e.taskLastProgress[taskID] = s
			}
			e.mu.Unlock()
		}
	}

	// Evict orphans
	if len(orphans) > 0 {
		e.mu.Lock()
		for _, id := range orphans {
			delete(e.taskLastProgress, id)
		}
		e.mu.Unlock()
	}
}
