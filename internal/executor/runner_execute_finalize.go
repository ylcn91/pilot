package executor

import (
	"log/slog"
	"time"

	"github.com/ylcn91/pilot/internal/webhooks"
)

// executeFinalize runs after executeCopyResult. It handles the failure branch
// and the success branch (delegating success sub-phases to helper methods), then
// records learning/graph/model-routing outcomes (original lines ~1341-2492).
//
// It returns a non-nil result/error on any success-branch abort path; otherwise
// it falls through to the final outcome recording and returns (s.result, nil).
func (r *Runner) executeFinalize(s *executeState) (*ExecutionResult, error) {
	task := s.task
	ctx := s.ctx
	log := s.log
	result := s.result
	backendResult := s.backendResult
	recorder := s.recorder
	state := s.state
	duration := s.duration
	complexity := s.complexity
	timeout := s.timeout

	if !result.Success {
		log.Error("Task execution failed",
			slog.String("error", result.Error),
			slog.Duration("duration", duration),
		)
		r.reportProgress(task.ID, "Failed", 100, result.Error)
		r.saveLogEntry(task.ID, "error", "Task failed: "+result.Error)

		// GH-2328: persist stderr + final assistant message + error type.
		r.persistBackendDiagnostics(task.ID, backendResult)

		// Emit task failed event
		r.emitAlertEvent(AlertEvent{
			Type:      AlertEventTypeTaskFailed,
			TaskID:    task.ID,
			TaskTitle: task.Title,
			Project:   task.ProjectPath,
			Error:     result.Error,
			Timestamp: time.Now(),
		})

		// Dispatch webhook for task failed
		r.dispatchWebhook(ctx, webhooks.EventTaskFailed, webhooks.TaskFailedData{
			TaskID:   task.ID,
			Title:    task.Title,
			Project:  task.ProjectPath,
			Duration: duration,
			Error:    result.Error,
			Phase:    state.phase,
		})

		// Finish recording with failed status
		if recorder != nil {
			recorder.SetModel(result.ModelName)
			recorder.SetNavigator(state.hasNavigator)
			if finErr := recorder.Finish("failed"); finErr != nil {
				log.Warn("Failed to finish recording", slog.Any("error", finErr))
			}
		}
	} else {
		result.Success = true

		// Log execution metrics for observability (GH-54 speed optimization)
		metrics := NewExecutionMetrics(
			task.ID,
			complexity,
			result.ModelName,
			duration,
			state,
			timeout,
			false, // not timed out
		)
		log.Info("Task completed",
			slog.String("task_id", metrics.TaskID),
			slog.String("complexity", metrics.Complexity.String()),
			slog.String("model", metrics.Model),
			slog.Duration("duration", metrics.Duration),
			slog.Bool("navigator_skipped", metrics.NavigatorSkipped),
			slog.Int64("tokens_in", metrics.TokensIn),
			slog.Int64("tokens_out", metrics.TokensOut),
			slog.Float64("cost_usd", metrics.EstimatedCostUSD),
			slog.Int("files_read", metrics.FilesRead),
			slog.Int("files_written", metrics.FilesWritten),
		)
		r.reportProgress(task.ID, "Completed", 90, "Execution completed")

		if res, err := r.executeNoCommitRetry(s); res != nil || err != nil {
			return res, err
		}

		if res, err := r.executeQualityGates(s); res != nil || err != nil {
			return res, err
		}

		if res, err := r.executeSelfReviewIntent(s); res != nil || err != nil {
			return res, err
		}

		if res, err := r.executeLintPushPR(s); res != nil || err != nil {
			return res, err
		}

		r.executeCompletedEvents(s)
	}

	// GH-1813: Record execution outcome for pattern learning (self-improvement)
	r.recordLearning(ctx, task, result)

	// GH-2015: Record execution into knowledge graph for cross-project learnings
	r.recordGraphLearning(task, result)

	// GH-1991: Record outcome for model routing escalation
	r.recordOutcome(task, result, complexity, duration)

	return result, nil
}
