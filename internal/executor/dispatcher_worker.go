package executor

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

// WorkerStatus represents the current state of a project worker.
type WorkerStatus struct {
	ProjectPath   string
	IsProcessing  bool
	CurrentTaskID string
	QueuedCount   int
}

// ProjectWorker processes tasks for a single project serially.
// Only one task runs at a time per project to prevent git conflicts.
type ProjectWorker struct {
	projectPath   string
	store         *memory.Store
	runner        *Runner
	log           *slog.Logger
	signal        chan struct{}
	processing    atomic.Bool
	currentTaskID atomic.Value // stores string
	stopCh        chan struct{}
	mu            sync.Mutex
}

// NewProjectWorker creates a new project worker.
func NewProjectWorker(projectPath string, store *memory.Store, runner *Runner, log *slog.Logger) *ProjectWorker {
	return &ProjectWorker{
		projectPath: projectPath,
		store:       store,
		runner:      runner,
		log:         log.With(slog.String("project", projectPath)),
		signal:      make(chan struct{}, 1), // Buffered to avoid blocking
		stopCh:      make(chan struct{}),
	}
}

// Run starts the worker loop. Blocks until context is cancelled.
func (w *ProjectWorker) Run(ctx context.Context) {
	w.log.Debug("Worker started")

	for {
		select {
		case <-ctx.Done():
			w.log.Debug("Worker stopped (context cancelled)")
			return
		case <-w.stopCh:
			w.log.Debug("Worker stopped (stop signal)")
			return
		case <-w.signal:
			w.processQueue(ctx)
		}
	}
}

// Stop signals the worker to stop.
func (w *ProjectWorker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	select {
	case <-w.stopCh:
		// Already stopped
	default:
		close(w.stopCh)
	}
}

// Signal notifies the worker to check the queue.
func (w *ProjectWorker) Signal() {
	select {
	case w.signal <- struct{}{}:
	default:
		// Signal already pending
	}
}

// Status returns the current worker status.
func (w *ProjectWorker) Status() WorkerStatus {
	taskID := ""
	if v := w.currentTaskID.Load(); v != nil {
		taskID = v.(string)
	}

	// Get queue count
	queuedCount := 0
	if tasks, err := w.store.GetQueuedTasksForProject(w.projectPath, 100); err == nil {
		queuedCount = len(tasks)
	}

	return WorkerStatus{
		ProjectPath:   w.projectPath,
		IsProcessing:  w.processing.Load(),
		CurrentTaskID: taskID,
		QueuedCount:   queuedCount,
	}
}

// processQueue processes all queued tasks for this project.
func (w *ProjectWorker) processQueue(ctx context.Context) {
	// Only one goroutine can process at a time
	if !w.processing.CompareAndSwap(false, true) {
		return // Already processing
	}
	defer w.processing.Store(false)

	for {
		// Check if we should stop
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		default:
		}

		// Get next queued task for THIS project
		tasks, err := w.store.GetQueuedTasksForProject(w.projectPath, 1)
		if err != nil {
			w.log.Error("Failed to get queued tasks", slog.Any("error", err))
			return
		}

		if len(tasks) == 0 {
			return // Queue empty
		}

		exec := tasks[0]
		w.currentTaskID.Store(exec.TaskID)

		w.log.Info("Processing task",
			slog.String("execution_id", exec.ID),
			slog.String("task_id", exec.TaskID),
			slog.String("title", exec.TaskTitle),
		)

		// Update status to running
		if err := w.store.UpdateExecutionStatus(exec.ID, "running"); err != nil {
			w.log.Error("Failed to update status to running", slog.Any("error", err))
			continue
		}

		// Emit progress callback for task started
		w.runner.EmitProgress(exec.TaskID, "Running", 2, fmt.Sprintf("Worker started: %s", truncateForLog(exec.TaskTitle, 40)))

		// Build task from execution record (full details stored when queued)
		// GH-2326: restore Labels so runner-side no-decompose / autopilot-fix
		// gates see the same labels the dispatch-time Decompose() saw.
		task := &Task{
			ID:            exec.TaskID,
			Title:         exec.TaskTitle,
			Description:   exec.TaskDescription,
			ProjectPath:   exec.ProjectPath,
			Branch:        exec.TaskBranch,
			BaseBranch:    exec.TaskBaseBranch,
			CreatePR:      exec.TaskCreatePR,
			Verbose:       exec.TaskVerbose,
			SourceAdapter: exec.TaskSourceAdapter,
			SourceIssueID: exec.TaskSourceIssueID,
			Labels:        exec.TaskLabels,
			State:         exec.TaskState, // CS-2 (#32): restore state for the parent-actionable gate
		}

		// Execute (blocking)
		start := time.Now()
		result, execErr := w.runner.Execute(ctx, task)
		duration := time.Since(start)

		// Update execution record with result
		if execErr != nil {
			w.log.Error("Task execution failed",
				slog.String("task_id", exec.TaskID),
				slog.Any("error", execErr),
				slog.Duration("duration", duration),
			)
			if err := w.store.UpdateExecutionStatus(exec.ID, "failed", execErr.Error()); err != nil {
				w.log.Error("Failed to update status to failed", slog.Any("error", err))
			}
			// Emit progress callback for task failed
			w.runner.EmitProgress(exec.TaskID, "Failed", 100, fmt.Sprintf("Execution error: %s", truncateForLog(execErr.Error(), 60)))
		} else if !result.Success {
			// TASK-358: classify the terminal outcome instead of collapsing every
			// non-success into "failed". declined / no-op / stalled get their own
			// status so the dashboard's "failed" count reflects genuine failures.
			status := TerminalStatus(result)
			w.log.Warn("Task ended without success",
				slog.String("task_id", exec.TaskID),
				slog.String("status", status),
				slog.String("error", result.Error),
				slog.Duration("duration", duration),
			)
			if err := w.store.UpdateExecutionStatus(exec.ID, status, result.Error); err != nil {
				w.log.Error("Failed to update execution status",
					slog.String("status", status), slog.Any("error", err))
			}
			// Emit progress callback with a phase that matches the classified outcome.
			w.runner.EmitProgress(exec.TaskID, terminalPhaseLabel(status), 100,
				fmt.Sprintf("%s: %s", status, truncateForLog(result.Error, 60)))
		} else {
			w.log.Info("Task completed successfully",
				slog.String("task_id", exec.TaskID),
				slog.Duration("duration", duration),
				slog.String("pr_url", result.PRUrl),
			)
			if err := w.store.UpdateExecutionStatus(exec.ID, "completed"); err != nil {
				w.log.Error("Failed to update status to completed", slog.Any("error", err))
			}
			// Update result fields (PR URL, commit SHA, duration)
			if err := w.store.UpdateExecutionResult(exec.ID, result.PRUrl, result.CommitSHA, duration.Milliseconds()); err != nil {
				w.log.Error("Failed to update execution result", slog.Any("error", err))
			}
			// Emit progress callback for task completed
			msg := fmt.Sprintf("Completed in %s", duration.Round(time.Second))
			if result.PRUrl != "" {
				msg = fmt.Sprintf("Completed with PR: %s", result.PRUrl)
			}
			w.runner.EmitProgress(exec.TaskID, "Completed", 100, msg)
		}

		// Persist execution metrics (tokens, cost, code changes) so they survive restarts.
		// This is needed for GetLifetimeTokens() to return real data (GH-533).
		if result != nil {
			// GH-2807: persist effort/complexity tier for cost-by-tier observability.
			if result.EffortLevel != "" || result.ComplexityLevel != "" {
				if err := w.store.UpdateExecutionEffort(exec.ID, result.EffortLevel, result.ComplexityLevel); err != nil {
					w.log.Error("Failed to update execution effort", slog.Any("error", err))
				}
			}
			if err := w.store.SaveExecutionMetrics(&memory.ExecutionMetrics{
				ExecutionID:      exec.ID,
				TokensInput:      result.TokensInput,
				TokensOutput:     result.TokensOutput,
				TokensTotal:      result.TokensTotal,
				EstimatedCostUSD: result.EstimatedCostUSD,
				FilesChanged:     result.FilesChanged,
				LinesAdded:       result.LinesAdded,
				LinesRemoved:     result.LinesRemoved,
				ModelName:        result.ModelName,
				PeakRSSMB:        result.PeakRSSMB,
				FinalRSSMB:       result.FinalRSSMB,
			}); err != nil {
				w.log.Error("Failed to save execution metrics", slog.Any("error", err))
			}

			// GH-2429: emit per-execution usage events (task + token + compute) so the
			// `usage_events` table reflects real activity. UserID is single-tenant for
			// now (empty); when multi-user lands, plumb the real ID through Execution.
			if err := w.store.RecordTaskUsage(
				exec.ID,
				exec.UserID,
				exec.ProjectPath,
				duration.Milliseconds(),
				result.TokensInput,
				result.TokensOutput,
			); err != nil {
				w.log.Error("Failed to record usage event", slog.Any("error", err))
			}
		}

		w.currentTaskID.Store("")
	}
}

// truncateForLog truncates a string for log messages, removing newlines and adding ellipsis
func truncateForLog(s string, maxLen int) string {
	// Replace newlines with spaces
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// terminalPhaseLabel maps a classified execution status to the human-readable
// progress phase shown in the dashboard. TASK-358.
func terminalPhaseLabel(status string) string {
	switch status {
	case "no_op":
		return "No-op"
	case "stalled":
		return "Stalled"
	case "declined":
		return "Declined"
	default:
		return "Failed"
	}
}
