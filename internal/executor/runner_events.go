package executor

import (
	"context"
	"log/slog"
	"time"

	"github.com/ylcn91/pilot/internal/webhooks"
)

// emitAlertEvent sends an event to the alert processor if configured
func (r *Runner) emitAlertEvent(event AlertEvent) {
	if r.alertProcessor == nil {
		return
	}
	r.alertProcessor.ProcessEvent(event)
}

// dispatchWebhook sends a webhook event if webhook manager is configured
func (r *Runner) dispatchWebhook(ctx context.Context, eventType webhooks.EventType, data any) {
	if r.webhooks == nil {
		return
	}
	event := webhooks.NewEvent(eventType, data)
	r.webhooks.Dispatch(ctx, event)
}

// reportProgress sends a progress update to all registered callbacks.
// Progress is monotonic — values lower than the current high-water mark
// for a task are clamped upward to prevent dashboard progress regression.
func (r *Runner) reportProgress(taskID, phase string, progress int, message string) {
	// Enforce monotonic progress per task (never go backwards)
	if progress < 100 { // Allow 100 from any state (completion/failure)
		r.taskProgressMu.Lock()
		if r.taskProgress == nil {
			r.taskProgress = make(map[string]int)
		}
		if prev, ok := r.taskProgress[taskID]; ok && progress < prev {
			progress = prev // Clamp to high-water mark
		}
		r.taskProgress[taskID] = progress
		r.taskProgressMu.Unlock()
	} else {
		// Task done — clean up tracking
		r.taskProgressMu.Lock()
		delete(r.taskProgress, taskID)
		r.taskProgressMu.Unlock()
	}

	// Emit task progress to alerts engine so stuck-task detection sees updates (GH-2204)
	r.emitAlertEvent(AlertEvent{
		Type:      AlertEventTypeTaskProgress,
		TaskID:    taskID,
		Phase:     phase,
		Progress:  progress,
		Timestamp: time.Now(),
	})

	// Log progress unless suppressed (e.g., when visual progress display is active)
	if !r.suppressProgressLogs {
		r.log.Info("Task progress",
			slog.String("task_id", taskID),
			slog.String("phase", phase),
			slog.Int("progress", progress),
			slog.String("message", message),
		)
	}

	// Send to legacy callback (e.g., Telegram) if registered
	if r.onProgress != nil {
		r.onProgress(taskID, phase, progress, message)
	}

	// Send to all named callbacks (e.g., dashboard, monitors)
	r.progressMu.RLock()
	callbacks := make([]ProgressCallback, 0, len(r.progressCallbacks))
	for _, cb := range r.progressCallbacks {
		callbacks = append(callbacks, cb)
	}
	r.progressMu.RUnlock()

	for _, cb := range callbacks {
		cb(taskID, phase, progress, message)
	}
}
