package executor

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/ylcn91/pilot/internal/webhooks"
)

// Execute runs a task using the configured backend and returns the execution result.
// It handles the complete task lifecycle: branch creation, prompt building,
// backend invocation, progress tracking, and optional PR creation.
// The context can be used to cancel execution. Returns an error only for
// setup failures; execution failures are reported in ExecutionResult.
//
// When a decomposer is configured and enabled, complex tasks are automatically
// split into subtasks that run sequentially (GH-218). Only the final subtask
// creates a PR, accumulating all changes from previous subtasks.
func (r *Runner) Execute(ctx context.Context, task *Task) (*ExecutionResult, error) {
	return r.executeWithOptions(ctx, task, true)
}

// executeWithOptions is the internal implementation that allows controlling worktree creation.
// When allowWorktree is false, it skips worktree creation even if configured.
// This prevents recursive worktree creation in sub-issues and decomposed tasks.
func (r *Runner) executeWithOptions(ctx context.Context, task *Task, allowWorktree bool) (*ExecutionResult, error) {
	start := time.Now()
	s := &executeState{
		start:         start,
		task:          task,
		ctx:           ctx,
		executionPath: task.ProjectPath,
	}
	defer func() {
		if r.metricsRecorder != nil {
			r.metricsRecorder.RecordExecutionDuration(time.Since(start))
		}
	}()

	// Pre-worktree validation + isolated worktree creation (~47-151).
	if res, err := r.executeSetup(s, allowWorktree); res != nil || err != nil {
		return res, err
	}

	// Ensure worktree cleanup on exit (handles panic, early return, success).
	// before_remove hook fires just before worktree teardown (TASK-305).
	if s.cleanupWorktree != nil {
		defer func() {
			if s.beforeRemoveHookFn != nil {
				s.beforeRemoveHookFn()
			}
			s.cleanupWorktree()
		}()
	}

	// Pre-flight checks, Navigator auto-init, complexity detection (~165-199).
	if res, err := r.executePreflight(s); res != nil || err != nil {
		return res, err
	}

	// Epic detection / planning mode + task decomposition (~201-454).
	if res, err := r.executeEpic(s); res != nil || err != nil {
		return res, err
	}

	executionPath := s.executionPath
	complexity := s.complexity

	// Apply timeout based on task complexity.
	// LocalMode: override to complex timeout (60m minimum) since sandbox tasks
	// can't be reliably classified from short descriptions alone. A "trivial"
	// classification giving 15m timeout caused filter-js-from-html to fail.
	timeout := r.modelRouter.SelectTimeout(task)
	if task.LocalMode {
		complexTimeout := r.modelRouter.GetTimeoutForComplexity(ComplexityComplex)
		if timeout < complexTimeout {
			timeout = complexTimeout
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	s.ctx = ctx
	s.cancel = cancel
	s.timeout = timeout

	// Logger, model/effort, git/branch, research, workflow, prompt, recorder,
	// hooks (~470-737).
	if res, err := r.executePrepare(s); res != nil || err != nil {
		return res, err
	}

	// Ensure cleanup happens regardless of execution outcome
	defer func() {
		if s.hookRestoreFunc != nil {
			_ = s.hookRestoreFunc() // Error already logged inside hookRestoreFunc
		}
	}()

	log := s.log
	selectedModel := s.selectedModel
	selectedEffort := s.selectedEffort
	repoWorkflow := s.repoWorkflow
	hookEnv := s.hookEnv
	prompt := s.prompt
	state := s.state
	recorder := s.recorder

	// GH-1599: Log implementation phase
	r.saveLogEntry(task.ID, "info", "Implementing changes...")

	// TASK-308: Stall detection — track last event time and spawn a watchdog.
	var (
		lastEventAt       atomic.Int64
		stallDetectedFlag atomic.Bool
		stallDone         = make(chan struct{})
	)
	lastEventAt.Store(time.Now().UnixNano())
	stallTimeout := r.effectiveStallTimeout()
	var stallExecutionCtx context.Context
	var stallCancel context.CancelFunc
	if stallTimeout > 0 {
		stallExecutionCtx, stallCancel = context.WithCancel(ctx)
		go r.runStallWatchdog(task.ID, &lastEventAt, &stallDetectedFlag, stallTimeout, stallDone, stallCancel)
	} else {
		stallExecutionCtx = ctx
		stallCancel = func() {}
	}

	// Execute via backend with watchdog (GH-882)
	// Watchdog kills subprocess after 2x timeout as a safety net for processes
	// that ignore context cancellation.
	// TASK-305: before_run hook fires just before agent execution.
	if repoWorkflow != nil {
		runWorkflowHook(ctx, "before_run", repoWorkflow.Hooks.BeforeRun, executionPath, hookEnv, log)
	}

	watchdogTimeout := 2 * timeout
	allowedTools, mcpConfigPath := r.executionToolOptions()

	var backendResult *BackendResult
	var err error
	if r.config != nil && r.config.TDD != nil && r.config.TDD.Enabled {
		// Opt-in TDD mode: ARCHITECT -> TEST-AUTHOR -> RED -> IMPLEMENTER -> GREEN.
		// Returns the IMPLEMENTER result so the finalize tail (QA/PR) runs unchanged.
		backendResult, err = r.runTDDSequence(s)
	} else {
		backendResult, err = r.executePrimaryBackend(s, stallExecutionCtx, timeout, watchdogTimeout, allowedTools, mcpConfigPath, &lastEventAt)
	}

	// Stop stall watchdog and release stall context resources.
	close(stallDone)
	stallCancel()

	// TASK-305: after_run hook fires as soon as the agent finishes (success or error).
	if repoWorkflow != nil {
		runWorkflowHook(ctx, "after_run", repoWorkflow.Hooks.AfterRun, executionPath, hookEnv, log)
	}

	// Transfer stallDetected flag to progressState for post-Execute checks.
	if stallDetectedFlag.Load() {
		state.stallDetected = true
	}

	duration := time.Since(start)

	// Build execution result
	result := &ExecutionResult{
		TaskID:          task.ID,
		Duration:        duration,
		EffortLevel:     selectedEffort,
		ComplexityLevel: complexity.String(),
	}
	s.result = result
	s.duration = duration

	if err != nil {
		result.Success = false

		// GH-539: Check if this was a per-task budget limit breach (~231-277).
		if res := r.executeBudgetGuard(s); res != nil {
			return res, nil
		}

		// TASK-308: Check if this was a stall (no event activity) (~279-326).
		if res := r.executeStallGuard(s, stallTimeout); res != nil {
			return res, nil
		}

		// Check if this was a timeout
		timedOut := ctx.Err() == context.DeadlineExceeded
		if timedOut {
			r.executeTimeoutHandler(s)
		} else {
			// GH-917: Classify the backend error (~366-460).
			alertType, errorCategory, stderrOutput := r.executeClassifyBackendError(s, err)

			// GH-920: Check for smart retry before emitting alerts
			// Note: state.smartRetryAttempt tracks retry attempts for this error path
			if r.retrier != nil {
				decision := r.retrier.Evaluate(err, state.smartRetryAttempt, timeout)
				if decision.ShouldRetry {
					// GH-1030: Record correction for drift detection
					if r.driftDetector != nil {
						r.driftDetector.RecordCorrection("retry_triggered", fmt.Sprintf("Error: %s, Retry attempt: %d", err.Error(), state.smartRetryAttempt+1))
					}
					state.smartRetryAttempt++
					log.Info("Smart retry triggered",
						slog.String("task_id", task.ID),
						slog.String("error_category", errorCategory),
						slog.Int("attempt", state.smartRetryAttempt),
						slog.Duration("backoff", decision.BackoffDuration),
					)
					r.reportProgress(task.ID, "Retrying", 50, fmt.Sprintf("Waiting %v before retry (attempt %d)...", decision.BackoffDuration, state.smartRetryAttempt))

					// Sleep for backoff duration
					if sleepErr := r.retrier.Sleep(ctx, decision.BackoffDuration); sleepErr != nil {
						log.Warn("Retry sleep interrupted", slog.Any("error", sleepErr))
						// Fall through to emit alerts
					} else {
						// Re-execute with potentially extended timeout
						retryTimeout := timeout
						if decision.ExtendedTimeout > 0 {
							retryTimeout = decision.ExtendedTimeout
						}
						retryCtx, retryCancel := context.WithTimeout(context.Background(), retryTimeout)

						r.reportProgress(task.ID, "Re-executing", 55, fmt.Sprintf("Retry attempt %d with %v timeout...", state.smartRetryAttempt, retryTimeout))

						smartAllowed, smartMCP := r.executionToolOptions()
						retryResult, retryErr := r.execBackend.Execute(retryCtx, ExecuteOptions{
							Prompt:          prompt,
							ProjectPath:     executionPath, // TASK-323: retry in the worktree, not the user's real repo
							Verbose:         task.Verbose,
							Model:           selectedModel,
							Effort:          selectedEffort,
							WatchdogTimeout: 2 * retryTimeout,
							AllowedTools:    smartAllowed,
							MCPConfigPath:   smartMCP,
							EventHandler: func(event BackendEvent) {
								if recorder != nil {
									_ = recorder.RecordEvent(event.Raw)
								}
								r.processBackendEvent(task.ID, event, state)
							},
						})
						retryCancel()

						if retryErr == nil && retryResult != nil && retryResult.Success {
							// Retry succeeded! Update backendResult and continue
							log.Info("Smart retry succeeded",
								slog.String("task_id", task.ID),
								slog.Int("attempt", state.smartRetryAttempt),
							)
							r.reportProgress(task.ID, "Retry Success", 90, "Retry completed successfully")

							// Update results from retry
							backendResult = retryResult
							err = nil
							goto retrySucceeded
						}
						// Retry failed, continue to emit alerts
						log.Warn("Smart retry failed",
							slog.String("task_id", task.ID),
							slog.Int("attempt", state.smartRetryAttempt),
							slog.Any("error", retryErr),
						)
					}
				}
			}

			// GH-1716: If execution was killed and decompose_on_kill is enabled,
			// attempt decomposition as last resort before failing.
			if r.retrier != nil && r.retrier.config.DecomposeOnKill && r.decomposer != nil {
				if beErr, ok := err.(BackendError); ok && beErr.ErrorType() == "timeout" {
					log.Info("Execution killed, attempting decomposition fallback",
						slog.String("task_id", task.ID))

					decompResult := r.decomposer.DecomposeForRetry(ctx, task)
					if decompResult.Decomposed && len(decompResult.Subtasks) > 1 {
						log.Info("Decomposition fallback succeeded",
							slog.String("task_id", task.ID),
							slog.Int("subtask_count", len(decompResult.Subtasks)))
						return r.executeDecomposedTask(ctx, task, decompResult.Subtasks, executionPath)
					}
				}
			}

			// GH-917-5: Include stderr in alert metadata for debugging
			metadata := map[string]string{
				"error_category": errorCategory,
			}
			if stderrOutput != "" {
				metadata["stderr"] = stderrOutput
			}

			// Emit alert event with error category metadata
			r.emitAlertEvent(AlertEvent{
				Type:      alertType,
				TaskID:    task.ID,
				TaskTitle: task.Title,
				Project:   task.ProjectPath,
				Error:     result.Error,
				Metadata:  metadata,
				Timestamp: time.Now(),
			})

			// Dispatch webhook for task failed (non-timeout)
			r.dispatchWebhook(ctx, webhooks.EventTaskFailed, webhooks.TaskFailedData{
				TaskID:   task.ID,
				Title:    task.Title,
				Project:  task.ProjectPath,
				Duration: duration,
				Error:    result.Error,
				Phase:    state.phase,
			})
		}

		// GH-1599: Log task failed milestone
		r.saveLogEntry(task.ID, "error", "Task failed: "+result.Error)

		// GH-2328: persist stderr + final assistant message + error type so
		// "unknown: exit status 1" is actually diagnosable. Without this,
		// failures look identical regardless of whether Claude refused, hit a
		// rate limit, was OOM-killed, or crashed silently.
		r.persistBackendDiagnostics(task.ID, backendResult)

		// Finish recording with failed status
		if recorder != nil {
			recorder.SetModel(state.modelName)
			recorder.SetNavigator(state.hasNavigator)
			if finErr := recorder.Finish("failed"); finErr != nil {
				log.Warn("Failed to finish recording", slog.Any("error", finErr))
			}
		}
		return result, nil
	}

retrySucceeded:
	s.backendResult = backendResult
	s.duration = duration
	s.result = result
	s.watchdogTimeout = watchdogTimeout

	// Copy backend result, recover/guard commit SHA, fill metrics + Prometheus
	// counters (~1207-1340).
	r.executeCopyResult(s)

	// Failure/success finalization + outcome recording (~1341-2492).
	return r.executeFinalize(s)
}
