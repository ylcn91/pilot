package orchestrator

import (
	"github.com/ylcn91/pilot/internal/executor"
)

// OnProgress registers an external callback for task progress updates
func (o *Orchestrator) OnProgress(callback func(taskID, phase string, progress int, message string)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.progressCallback = callback
}

// OnCompletion registers an external callback for task completion events.
// The callback receives taskID, prURL (empty if failed), success flag, and error message (empty if success).
func (o *Orchestrator) OnCompletion(callback func(taskID, prURL string, success bool, errMsg string)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.completionCallback = callback
}

// OnToken registers a callback for token usage updates on the underlying runner.
func (o *Orchestrator) OnToken(name string, callback func(taskID string, inputTokens, outputTokens int64, modelName string)) {
	o.runner.AddTokenCallback(name, callback)
}

// GetTaskStates returns current task states
func (o *Orchestrator) GetTaskStates() []*executor.TaskState {
	return o.monitor.GetAll()
}

// GetRunningTasks returns currently running tasks
func (o *Orchestrator) GetRunningTasks() []*executor.TaskState {
	return o.monitor.GetRunning()
}

// SetAlertProcessor sets the alert processor on the underlying runner for task lifecycle events.
// The processor interface is satisfied by alerts.Engine.
func (o *Orchestrator) SetAlertProcessor(processor executor.AlertEventProcessor) {
	o.runner.SetAlertProcessor(processor)
}

// SuppressProgressLogs disables slog output for progress updates.
// Use this when a visual progress display is active to prevent log spam.
func (o *Orchestrator) SuppressProgressLogs(suppress bool) {
	o.runner.SuppressProgressLogs(suppress)
}

// SetQualityCheckerFactory sets the factory for creating quality checkers.
// The factory is called for each task to create a task-specific quality checker.
// Quality gates run after task execution to validate code quality before PR creation.
func (o *Orchestrator) SetQualityCheckerFactory(factory executor.QualityCheckerFactory) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.qualityCheckerFactory = factory
	// Also set on the runner for direct execution
	o.runner.SetQualityCheckerFactory(factory)
}
