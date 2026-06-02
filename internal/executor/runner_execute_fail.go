package executor

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/ylcn91/pilot/internal/webhooks"
)

// executeBudgetGuard handles the per-task budget-limit breach path (GH-539,
// original lines ~231-277). It returns a non-nil result when the budget guard
// fires (the caller returns it); otherwise nil to continue.
func (r *Runner) executeBudgetGuard(s *executeState) *ExecutionResult {
	task := s.task
	log := s.log
	result := s.result
	state := s.state
	recorder := s.recorder
	duration := s.duration

	// GH-539: Check if this was a per-task budget limit breach
	if state.budgetExceeded {
		result.Outcome = "budget_exceeded" // TASK-358: not a code failure
		result.Error = fmt.Sprintf("per-task budget limit exceeded: %s", state.budgetReason)
		result.TokensInput = state.tokensInput
		result.TokensOutput = state.tokensOutput
		result.TokensTotal = state.tokensInput + state.tokensOutput
		result.CacheCreationInputTokens = state.cacheCreationInputTokens
		result.CacheReadInputTokens = state.cacheReadInputTokens
		result.ModelName = state.modelName
		if result.ModelName == "" {
			result.ModelName = r.fallbackModelName()
		}
		result.EstimatedCostUSD = estimateCostWithCache(result.TokensInput, result.TokensOutput, result.CacheCreationInputTokens, result.CacheReadInputTokens, result.ModelName)
		log.Warn("Task cancelled due to per-task budget limit",
			slog.String("task_id", task.ID),
			slog.String("reason", state.budgetReason),
			slog.Int64("input_tokens", state.tokensInput),
			slog.Int64("output_tokens", state.tokensOutput),
			slog.Duration("duration", duration),
		)
		r.reportProgress(task.ID, "Budget Exceeded", 100, result.Error)

		// Emit budget exceeded alert event
		r.emitAlertEvent(AlertEvent{
			Type:      AlertEventTypeTaskFailed,
			TaskID:    task.ID,
			TaskTitle: task.Title,
			Project:   task.ProjectPath,
			Error:     result.Error,
			Metadata: map[string]string{
				"reason":        "budget_exceeded",
				"input_tokens":  fmt.Sprintf("%d", state.tokensInput),
				"output_tokens": fmt.Sprintf("%d", state.tokensOutput),
			},
			Timestamp: time.Now(),
		})

		if recorder != nil {
			recorder.SetModel(state.modelName)
			recorder.SetNavigator(state.hasNavigator)
			if finErr := recorder.Finish("budget_exceeded"); finErr != nil {
				log.Warn("Failed to finish recording", slog.Any("error", finErr))
			}
		}
		return result
	}

	return nil
}

// executeStallGuard handles the stall-detection path (TASK-308, original lines
// ~279-326). It returns a non-nil result when the stall guard fires; otherwise
// nil to continue.
func (r *Runner) executeStallGuard(s *executeState, stallTimeout time.Duration) *ExecutionResult {
	task := s.task
	log := s.log
	result := s.result
	state := s.state
	recorder := s.recorder
	duration := s.duration

	// TASK-308: Check if this was a stall (no event activity for stall_timeout).
	if state.stallDetected {
		result.Outcome = "stalled" // TASK-358: incomplete run, not a code failure
		result.Error = fmt.Sprintf("session stalled: no agent event for >%v", stallTimeout)
		result.TokensInput = state.tokensInput
		result.TokensOutput = state.tokensOutput
		result.TokensTotal = state.tokensInput + state.tokensOutput
		result.CacheCreationInputTokens = state.cacheCreationInputTokens
		result.CacheReadInputTokens = state.cacheReadInputTokens
		result.ModelName = state.modelName
		if result.ModelName == "" {
			result.ModelName = r.fallbackModelName()
		}
		result.EstimatedCostUSD = estimateCostWithCache(result.TokensInput, result.TokensOutput, result.CacheCreationInputTokens, result.CacheReadInputTokens, result.ModelName)
		log.Warn("Task stalled: no agent event activity",
			slog.String("task_id", task.ID),
			slog.Duration("stall_timeout", stallTimeout),
			slog.Duration("duration", duration),
		)
		r.reportProgress(task.ID, "Stalled", 100, result.Error)
		if r.monitor != nil {
			r.monitor.Stall(task.ID, result.Error)
		}
		r.emitAlertEvent(AlertEvent{
			Type:      AlertEventTypeTaskFailed,
			TaskID:    task.ID,
			TaskTitle: task.Title,
			Project:   task.ProjectPath,
			Error:     result.Error,
			Metadata: map[string]string{
				"reason":        "stalled",
				"stall_timeout": stallTimeout.String(),
				"duration_ms":   fmt.Sprintf("%d", duration.Milliseconds()),
			},
			Timestamp: time.Now(),
		})
		if r.metricsRecorder != nil {
			r.metricsRecorder.RecordExecution(result.ModelName, "stalled")
		}
		if recorder != nil {
			recorder.SetModel(state.modelName)
			recorder.SetNavigator(state.hasNavigator)
			if finErr := recorder.Finish("stalled"); finErr != nil {
				log.Warn("Failed to finish recording", slog.Any("error", finErr))
			}
		}
		return result
	}

	return nil
}

// executeTimeoutHandler handles the deadline-exceeded branch (original lines
// ~330-364). It only mutates state and emits alerts/webhook; the common failure
// tail continues inline in the caller.
func (r *Runner) executeTimeoutHandler(s *executeState) {
	task := s.task
	ctx := s.ctx
	log := s.log
	result := s.result
	state := s.state
	duration := s.duration
	complexity := s.complexity
	timeout := s.timeout

	result.Error = fmt.Sprintf("task timed out after %v", timeout)
	log.Error("Task timed out",
		slog.String("task_id", task.ID),
		slog.String("complexity", complexity.String()),
		slog.Duration("timeout", timeout),
		slog.Duration("duration", duration),
	)
	r.reportProgress(task.ID, "Timeout", 100, result.Error)

	// Emit task timeout event with complexity metadata
	r.emitAlertEvent(AlertEvent{
		Type:      AlertEventTypeTaskTimeout,
		TaskID:    task.ID,
		TaskTitle: task.Title,
		Project:   task.ProjectPath,
		Error:     result.Error,
		Metadata: map[string]string{
			"complexity":  complexity.String(),
			"timeout":     timeout.String(),
			"duration_ms": fmt.Sprintf("%d", duration.Milliseconds()),
		},
		Timestamp: time.Now(),
	})

	// Dispatch webhook for task timeout
	r.dispatchWebhook(ctx, webhooks.EventTaskTimeout, webhooks.TaskTimeoutData{
		TaskID:     task.ID,
		Title:      task.Title,
		Project:    task.ProjectPath,
		Duration:   duration,
		Timeout:    timeout,
		Complexity: complexity.String(),
		Phase:      state.phase,
	})
}

// executeClassifyBackendError maps a non-timeout backend error to its alert
// type / category / stderr and reports progress (GH-917, original lines
// ~366-460). It sets result.Error and returns the alert classification used by
// the inline smart-retry + alert-emission path.
func (r *Runner) executeClassifyBackendError(s *executeState, err error) (AlertEventType, string, string) {
	task := s.task
	log := s.log
	result := s.result
	duration := s.duration

	// GH-917: Check for classified Claude Code error types
	alertType := AlertEventTypeTaskFailed
	errorCategory := "unknown"
	var stderrOutput string // GH-917-5: Always capture stderr for logging

	if beErr, ok := err.(BackendError); ok {
		result.Error = beErr.Error()
		stderrOutput = beErr.ErrorStderr() // Capture stderr from classified error

		// Map error type to alert event type and category
		switch beErr.ErrorType() {
		case "rate_limit":
			alertType = AlertEventTypeRateLimit
			errorCategory = "rate_limit"
			log.Warn("Backend hit rate limit",
				slog.String("task_id", task.ID),
				slog.String("stderr", beErr.ErrorStderr()),
				slog.Duration("duration", duration),
			)
			r.reportProgress(task.ID, "Rate Limited", 100, "Backend hit rate limit - retry later")

		case "invalid_config":
			alertType = AlertEventTypeConfigError
			errorCategory = "invalid_config"
			log.Error("Invalid backend configuration",
				slog.String("task_id", task.ID),
				slog.String("message", beErr.ErrorMessage()),
				slog.String("stderr", beErr.ErrorStderr()),
			)
			r.reportProgress(task.ID, "Config Error", 100, beErr.ErrorMessage())

		case "api_error":
			alertType = AlertEventTypeAPIError
			errorCategory = "api_error"
			log.Error("Backend API error",
				slog.String("task_id", task.ID),
				slog.String("message", beErr.ErrorMessage()),
				slog.String("stderr", beErr.ErrorStderr()),
			)
			r.reportProgress(task.ID, "API Error", 100, beErr.ErrorMessage())

		case "oom_killed":
			// GH-2332: distinct alert so operators can spot memory-pressure
			// patterns instead of burying OOM kills in the generic "unknown" bucket.
			alertType = AlertEventTypeOOMKilled
			errorCategory = "oom_killed"
			log.Error("Backend OOM-killed",
				slog.String("task_id", task.ID),
				slog.String("message", beErr.ErrorMessage()),
				slog.String("stderr", beErr.ErrorStderr()),
				slog.Duration("duration", duration),
			)
			r.reportProgress(task.ID, "OOM Killed", 100, beErr.ErrorMessage())

		default:
			// GH-917-5: Log stderr for process errors and unknown errors too
			log.Error("Backend execution failed",
				slog.String("error", result.Error),
				slog.String("error_type", beErr.ErrorType()),
				slog.String("stderr", beErr.ErrorStderr()),
				slog.Duration("duration", duration),
			)
			r.reportProgress(task.ID, "Failed", 100, result.Error)
		}
	} else {
		result.Error = err.Error()
		// GH-917-5: Log even when error is not a classified backend error
		log.Error("Backend execution failed",
			slog.String("error", result.Error),
			slog.String("error_type", "unclassified"),
			slog.Duration("duration", duration),
		)
		r.reportProgress(task.ID, "Failed", 100, result.Error)
	}

	return alertType, errorCategory, stderrOutput
}
