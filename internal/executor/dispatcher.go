package executor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/memory"
)

// DispatcherConfig configures the task dispatcher behavior.
type DispatcherConfig struct {
	// StaleTaskDuration is a backwards-compat alias for StaleRunningThreshold.
	// Deprecated: use StaleRunningThreshold instead.
	StaleTaskDuration time.Duration

	// StaleRunningThreshold is how long a "running" task can remain before
	// it is considered orphaned and marked failed. Default: 30 minutes.
	StaleRunningThreshold time.Duration

	// StaleQueuedThreshold is how long a "queued" task can remain without
	// being picked up before it is considered stuck and marked failed.
	// Default: 5 minutes.
	StaleQueuedThreshold time.Duration

	// StaleRecoveryInterval is how often the periodic stale-recovery loop
	// runs. Default: 5 minutes.
	StaleRecoveryInterval time.Duration
}

// DefaultDispatcherConfig returns default dispatcher settings.
func DefaultDispatcherConfig() *DispatcherConfig {
	return &DispatcherConfig{
		StaleRunningThreshold: 30 * time.Minute,
		StaleQueuedThreshold:  5 * time.Minute,
		StaleRecoveryInterval: 5 * time.Minute,
	}
}

// resolveDefaults fills zero-valued fields with sensible defaults and
// applies the StaleTaskDuration backwards-compat alias.
func (c *DispatcherConfig) resolveDefaults() {
	// Backwards compat: if only the deprecated field is set, use it.
	if c.StaleRunningThreshold == 0 && c.StaleTaskDuration > 0 {
		c.StaleRunningThreshold = c.StaleTaskDuration
	}
	if c.StaleRecoveryInterval == 0 {
		c.StaleRecoveryInterval = 5 * time.Minute
	}
}

// Dispatcher manages task queuing and per-project workers.
// It ensures that tasks for the same project are executed serially
// while allowing parallel execution across different projects.
// Progress updates are emitted via runner.EmitProgress() so they
// flow through the same callback path as execution progress.
type Dispatcher struct {
	config     *DispatcherConfig
	store      *memory.Store
	runner     *Runner
	decomposer *TaskDecomposer           // Optional task decomposer
	workers    map[string]*ProjectWorker // key: project path
	mu         sync.RWMutex
	log        *slog.Logger
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// NewDispatcher creates a new task dispatcher.
func NewDispatcher(store *memory.Store, runner *Runner, config *DispatcherConfig) *Dispatcher {
	if config == nil {
		config = DefaultDispatcherConfig()
	}
	config.resolveDefaults()

	ctx, cancel := context.WithCancel(context.Background())

	return &Dispatcher{
		config:  config,
		store:   store,
		runner:  runner,
		workers: make(map[string]*ProjectWorker),
		log:     logging.WithComponent("dispatcher"),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// SetDecomposer sets the task decomposer for auto-splitting complex tasks.
// If set, complex tasks meeting the decomposition criteria will be split
// into subtasks before queuing.
func (d *Dispatcher) SetDecomposer(decomposer *TaskDecomposer) {
	d.decomposer = decomposer
}

// Start initializes the dispatcher, recovers stale tasks, and launches the
// periodic stale-recovery loop. The provided context controls the loop lifetime.
func (d *Dispatcher) Start(ctx context.Context) error {
	d.log.Info("Starting dispatcher")

	// Initial recovery pass on startup.
	d.recoverStaleTasks()

	// GH-2428: warn when the last batch of completed runs has no token
	// telemetry. A persistent gap means the backend's usage events aren't
	// being parsed — cost reporting and per-task budgets silently degrade.
	d.checkTelemetryGap()

	// Launch periodic recovery loop.
	d.wg.Add(1)
	go d.runStaleRecoveryLoop(ctx)

	return nil
}

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

// Stop gracefully stops all workers and the dispatcher.
func (d *Dispatcher) Stop() {
	d.log.Info("Stopping dispatcher")
	d.cancel()

	// Stop all workers
	d.mu.Lock()
	for _, worker := range d.workers {
		worker.Stop()
	}
	d.mu.Unlock()

	// Wait for all workers to finish
	d.wg.Wait()
	d.log.Info("Dispatcher stopped")
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

// QueueTask adds a task to the execution queue and returns the execution ID.
// The task will be executed by the project's worker in FIFO order.
// If a decomposer is configured and the task is complex, it will be split
// into subtasks that are queued instead of the parent task.
func (d *Dispatcher) QueueTask(ctx context.Context, task *Task) (string, error) {
	// Check for duplicate tasks
	exists, err := d.store.IsTaskQueued(task.ID)
	if err != nil {
		d.log.Warn("Failed to check for duplicate task", slog.Any("error", err))
	} else if exists {
		return "", fmt.Errorf("task %s is already queued or running", task.ID)
	}

	// Try decomposition if decomposer is configured
	if d.decomposer != nil {
		result := d.decomposer.Decompose(task)
		if result.Decomposed && len(result.Subtasks) > 1 {
			return d.queueDecomposedTask(ctx, task, result)
		}
	}

	// Queue single task
	return d.queueSingleTask(ctx, task)
}

// queueDecomposedTask handles queuing a decomposed task and its subtasks.
// The parent task is marked as "decomposed" and subtasks are queued in order.
func (d *Dispatcher) queueDecomposedTask(ctx context.Context, parent *Task, result *DecomposeResult) (string, error) {
	// Generate parent execution ID
	parentExecID := uuid.New().String()

	// Save parent as "decomposed" status
	parentExec := &memory.Execution{
		ID:                parentExecID,
		TaskID:            parent.ID,
		ProjectPath:       parent.ProjectPath,
		Status:            "decomposed",
		TaskTitle:         parent.Title,
		TaskDescription:   parent.Description,
		TaskBranch:        parent.Branch,
		TaskBaseBranch:    parent.BaseBranch,
		TaskCreatePR:      parent.CreatePR,
		TaskVerbose:       parent.Verbose,
		TaskSourceAdapter: parent.SourceAdapter,
		TaskSourceIssueID: parent.SourceIssueID,
		TaskLabels:        parent.Labels, // GH-2326: persist labels for no-decompose/autopilot-fix gates
	}

	if err := d.store.SaveExecution(parentExec); err != nil {
		return "", fmt.Errorf("failed to save decomposed parent: %w", err)
	}

	d.log.Info("Task decomposed",
		slog.String("parent_id", parent.ID),
		slog.Int("subtask_count", len(result.Subtasks)),
		slog.String("reason", result.Reason),
	)

	// Emit progress for parent
	d.runner.EmitProgress(parent.ID, "Decomposed", 0,
		fmt.Sprintf("Split into %d subtasks", len(result.Subtasks)))

	// Queue each subtask
	var lastExecID string
	for i, subtask := range result.Subtasks {
		execID, err := d.queueSingleTask(ctx, subtask)
		if err != nil {
			d.log.Error("Failed to queue subtask",
				slog.String("subtask_id", subtask.ID),
				slog.Int("index", i),
				slog.Any("error", err),
			)
			continue
		}
		lastExecID = execID
	}

	// Return parent execution ID
	if lastExecID == "" {
		return parentExecID, nil
	}
	return parentExecID, nil
}

// queueSingleTask queues a single task (no decomposition).
func (d *Dispatcher) queueSingleTask(ctx context.Context, task *Task) (string, error) {
	// Generate execution ID
	execID := uuid.New().String()

	// Save to SQLite with status='queued' and full task details
	exec := &memory.Execution{
		ID:                execID,
		TaskID:            task.ID,
		ProjectPath:       task.ProjectPath,
		Status:            "queued",
		TaskTitle:         task.Title,
		TaskDescription:   task.Description,
		TaskBranch:        task.Branch,
		TaskBaseBranch:    task.BaseBranch,
		TaskCreatePR:      task.CreatePR,
		TaskVerbose:       task.Verbose,
		TaskSourceAdapter: task.SourceAdapter,
		TaskSourceIssueID: task.SourceIssueID,
		TaskLabels:        task.Labels, // GH-2326: persist labels for no-decompose/autopilot-fix gates
	}

	if err := d.store.SaveExecution(exec); err != nil {
		return "", fmt.Errorf("failed to save execution: %w", err)
	}

	d.log.Info("Task queued",
		slog.String("execution_id", execID),
		slog.String("task_id", task.ID),
		slog.String("project", task.ProjectPath),
	)

	// Emit progress callback for task queued
	d.runner.EmitProgress(task.ID, "Queued", 0, fmt.Sprintf("Task queued (exec: %s)", execID[:8]))

	// Ensure worker exists and signal it
	d.ensureWorker(task.ProjectPath)

	return execID, nil
}

// ensureWorker creates a worker for the project if it doesn't exist and starts it.
func (d *Dispatcher) ensureWorker(projectPath string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.workers[projectPath]; exists {
		// Worker exists, signal it to check queue
		d.workers[projectPath].Signal()
		return
	}

	// Create new worker
	worker := NewProjectWorker(projectPath, d.store, d.runner, d.log)
	d.workers[projectPath] = worker

	// Start worker in background
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		worker.Run(d.ctx)
	}()

	d.log.Info("Started project worker", slog.String("project", projectPath))

	// Signal to process any queued tasks
	worker.Signal()
}

// GetWorkerStatus returns the status of all active workers.
func (d *Dispatcher) GetWorkerStatus() map[string]WorkerStatus {
	d.mu.RLock()
	defer d.mu.RUnlock()

	status := make(map[string]WorkerStatus)
	for path, worker := range d.workers {
		status[path] = worker.Status()
	}
	return status
}

// GetExecutionStatus returns the current status of an execution.
func (d *Dispatcher) GetExecutionStatus(execID string) (*memory.Execution, error) {
	return d.store.GetExecution(execID)
}

// WaitForExecution waits for an execution to complete and returns the result.
// Returns error if context is cancelled or execution not found.
func (d *Dispatcher) WaitForExecution(ctx context.Context, execID string, pollInterval time.Duration) (*memory.Execution, error) {
	if pollInterval == 0 {
		pollInterval = 500 * time.Millisecond
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			exec, err := d.store.GetExecution(execID)
			if err != nil {
				return nil, fmt.Errorf("failed to get execution: %w", err)
			}

			// Check if terminal state
			switch exec.Status {
			case "completed", "failed", "cancelled":
				return exec, nil
			}
		}
	}
}
