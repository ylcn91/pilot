package executor

import (
	"context"
	"log/slog"
	"time"
)

// checkTelemetryGap inspects recent completed executions and logs a warning
// when token telemetry is mostly missing. Threshold: ≥50% of the last 50
// completed runs (with a real commit) reporting tokens_total=0. GH-2428.
func (d *Dispatcher) checkTelemetryGap() {
	const sampleSize = 50
	const threshold = 0.5
	stats, err := d.store.RecentCompletedTelemetryStats(sampleSize)
	if err != nil {
		d.log.Debug("Skipping telemetry gap check", slog.Any("error", err))
		return
	}
	if stats.CompletedRuns < 10 {
		return // Not enough data
	}
	ratio := float64(stats.ZeroTokenRuns) / float64(stats.CompletedRuns)
	if ratio >= threshold {
		backend := "claude-code"
		if d.runner != nil {
			backend = d.runner.backendType()
		}
		d.log.Warn("Token telemetry gap detected — recent completed runs report 0 tokens",
			slog.String("backend", backend),
			slog.Int("completed_runs", stats.CompletedRuns),
			slog.Int("zero_token_runs", stats.ZeroTokenRuns),
			slog.Float64("zero_token_ratio", ratio),
			slog.String("hint", "verify backend usage events are being parsed (GH-2428)"),
		)
	}
}

// runStaleRecoveryLoop ticks every StaleRecoveryInterval and calls
// recoverStaleTasks. It stops when ctx is cancelled or the dispatcher stops.
func (d *Dispatcher) runStaleRecoveryLoop(ctx context.Context) {
	defer d.wg.Done()

	interval := d.config.StaleRecoveryInterval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	d.log.Info("Stale recovery loop started", slog.Duration("interval", interval))

	for {
		select {
		case <-ctx.Done():
			d.log.Debug("Stale recovery loop stopped (context cancelled)")
			return
		case <-d.ctx.Done():
			d.log.Debug("Stale recovery loop stopped (dispatcher stopped)")
			return
		case <-ticker.C:
			d.recoverStaleTasks()
		}
	}
}

// recoverStaleTasks marks orphaned running and queued tasks as failed.
// Re-queuing without a worker just recreates the orphan, so we fail them.
func (d *Dispatcher) recoverStaleTasks() int {
	var resetCount int

	// Recover stale running tasks (crashed workers).
	staleRunning, err := d.store.GetStaleRunningExecutions(d.config.StaleRunningThreshold)
	if err != nil {
		d.log.Warn("Failed to fetch stale running executions", slog.Any("error", err))
	}
	for _, exec := range staleRunning {
		// If this task already completed successfully, delete the orphan row
		// instead of marking it failed (avoids dashboard showing false failures).
		completed, hceErr := d.store.HasCompletedExecution(exec.TaskID, exec.ProjectPath)
		if hceErr != nil {
			d.log.Warn("HasCompletedExecution error during stale-running reap; treating as not completed",
				slog.String("execution_id", exec.ID),
				slog.String("task_id", exec.TaskID),
				slog.Any("error", hceErr))
		}
		if completed {
			d.log.Info("Deleting orphan running row (task already completed)",
				slog.String("execution_id", exec.ID),
				slog.String("task_id", exec.TaskID),
			)
			if err := d.store.DeleteExecution(exec.ID); err != nil {
				d.log.Error("Failed to delete orphan running row", slog.String("id", exec.ID), slog.Any("error", err))
			}
			continue
		}
		if d.hasLiveWorker(exec.ProjectPath) {
			d.log.Debug("Skipping stale running reap — live worker for project exists",
				slog.String("execution_id", exec.ID),
				slog.String("task_id", exec.TaskID),
				slog.String("project", exec.ProjectPath),
			)
			continue
		}
		d.log.Warn("Marking stale running task as failed",
			slog.String("execution_id", exec.ID),
			slog.String("task_id", exec.TaskID),
			slog.Time("created_at", exec.CreatedAt),
		)
		if err := d.store.UpdateExecutionStatus(exec.ID, "failed", "stale running task recovered (orphaned worker)"); err != nil {
			d.log.Error("Failed to mark stale running task", slog.String("id", exec.ID), slog.Any("error", err))
		} else {
			resetCount++
		}
	}

	// Recover stale queued tasks (stuck in queue with no worker).
	// GH-2331: Don't mark queued tasks stale when a live worker exists for the
	// project — they're just waiting their turn. Pilot runs tasks serially per
	// project; when one task takes 8+ minutes (common for epic/Navigator work),
	// its siblings exceed the 5-minute threshold purely by waiting, and get
	// killed mid-queue. Only orphans (no worker alive) should be reaped.
	staleQueued, err := d.store.GetStaleQueuedExecutions(d.config.StaleQueuedThreshold)
	if err != nil {
		d.log.Warn("Failed to fetch stale queued executions", slog.Any("error", err))
	}
	for _, exec := range staleQueued {
		completed, hceErr := d.store.HasCompletedExecution(exec.TaskID, exec.ProjectPath)
		if hceErr != nil {
			d.log.Warn("HasCompletedExecution error during stale-queued reap; treating as not completed",
				slog.String("execution_id", exec.ID),
				slog.String("task_id", exec.TaskID),
				slog.Any("error", hceErr))
		}
		if completed {
			d.log.Info("Deleting orphan queued row (task already completed)",
				slog.String("execution_id", exec.ID),
				slog.String("task_id", exec.TaskID),
			)
			if err := d.store.DeleteExecution(exec.ID); err != nil {
				d.log.Error("Failed to delete orphan queued row", slog.String("id", exec.ID), slog.Any("error", err))
			}
			continue
		}

		if d.hasLiveWorker(exec.ProjectPath) {
			d.log.Debug("Skipping stale queued reap — live worker for project exists",
				slog.String("execution_id", exec.ID),
				slog.String("task_id", exec.TaskID),
				slog.String("project", exec.ProjectPath),
			)
			continue
		}

		d.log.Warn("Marking stale queued task as failed",
			slog.String("execution_id", exec.ID),
			slog.String("task_id", exec.TaskID),
			slog.Time("created_at", exec.CreatedAt),
		)
		if err := d.store.UpdateExecutionStatus(exec.ID, "failed", "stale queued task recovered (no worker picked up)"); err != nil {
			d.log.Error("Failed to mark stale queued task", slog.String("id", exec.ID), slog.Any("error", err))
		} else {
			resetCount++
		}
	}

	d.log.Info("stale recovery complete, reset N tasks", slog.Int("count", resetCount))
	return resetCount
}

// hasLiveWorker reports whether a worker goroutine exists for the given
// project path. Used by stale recovery to avoid killing queued tasks that
// are simply waiting their turn behind a long-running sibling. GH-2331.
func (d *Dispatcher) hasLiveWorker(projectPath string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.workers[projectPath]
	return ok
}
