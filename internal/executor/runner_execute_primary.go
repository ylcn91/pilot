package executor

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
)

// executePrimaryBackend runs the single-pass primary backend (the non-TDD path)
// with the watchdog kill callback and the stall/recording EventHandler. It is the
// original inline r.execBackend.Execute call, extracted verbatim so the dispatch
// site in Execute stays a clean two-branch decision (primary vs. TDD sequence)
// and runner.go stays under the 400-LOC ceiling. Behavior is unchanged.
func (r *Runner) executePrimaryBackend(
	s *executeState,
	stallExecutionCtx context.Context,
	timeout, watchdogTimeout time.Duration,
	allowedTools []string,
	mcpConfigPath string,
	lastEventAt *atomic.Int64,
) (*BackendResult, error) {
	task := s.task
	log := s.log
	recorder := s.recorder
	state := s.state
	complexity := s.complexity

	return r.execBackend.Execute(stallExecutionCtx, ExecuteOptions{
		Prompt:          s.prompt,
		ProjectPath:     s.executionPath, // Use worktree path if active
		Verbose:         task.Verbose,
		Model:           s.selectedModel,
		Effort:          s.selectedEffort,
		MaxTurns:        s.workflowMaxTurns, // TASK-304: per-repo .pilot/workflow.yaml override
		FromPR:          task.FromPR,        // GH-1267: session resumption from PR context
		WatchdogTimeout: watchdogTimeout,
		AllowedTools:    allowedTools,
		MCPConfigPath:   mcpConfigPath,
		WatchdogCallback: func(pid int, watchdogDuration time.Duration) {
			log.Warn("Watchdog killed subprocess",
				slog.Int("pid", pid),
				slog.Duration("watchdog_timeout", watchdogDuration),
				slog.Duration("configured_timeout", timeout),
			)
			r.reportProgress(task.ID, "Watchdog Kill", 100, fmt.Sprintf("Process killed by watchdog after %v (2x timeout)", watchdogDuration))

			// Emit watchdog kill alert
			r.emitAlertEvent(AlertEvent{
				Type:      AlertEventTypeWatchdogKill,
				TaskID:    task.ID,
				TaskTitle: task.Title,
				Project:   task.ProjectPath,
				Error:     fmt.Sprintf("subprocess killed by watchdog after %v", watchdogDuration),
				Metadata: map[string]string{
					"pid":                fmt.Sprintf("%d", pid),
					"watchdog_timeout":   watchdogDuration.String(),
					"configured_timeout": timeout.String(),
					"complexity":         complexity.String(),
				},
				Timestamp: time.Now(),
			})
		},
		EventHandler: func(event BackendEvent) {
			// TASK-308: touch the last-event timestamp so the stall watchdog resets.
			lastEventAt.Store(time.Now().UnixNano())

			// Record the event
			if recorder != nil {
				if recErr := recorder.RecordEvent(event.Raw); recErr != nil {
					log.Warn("Failed to record event", slog.Any("error", recErr))
				}
			}

			// Process event for progress tracking
			r.processBackendEvent(task.ID, event, state)
		},
	})
}
