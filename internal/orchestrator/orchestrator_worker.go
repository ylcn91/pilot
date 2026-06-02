package orchestrator

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
)

// QueueTask adds a task to the processing queue
func (o *Orchestrator) QueueTask(task *Task) {
	o.monitor.Register(task.ID, task.Document.Title, "")

	select {
	case o.taskQueue <- task:
		logging.WithTask(task.ID).Info("Task queued")
	default:
		logging.WithTask(task.ID).Warn("Task queue full, dropping task")
	}
}

// worker processes tasks from the queue
func (o *Orchestrator) worker(id int) {
	defer o.wg.Done()

	for task := range o.taskQueue {
		select {
		case <-o.ctx.Done():
			return
		default:
			o.processTask(task)
		}
	}
}

// processTask processes a single task
func (o *Orchestrator) processTask(task *Task) {
	o.mu.Lock()
	if o.running[task.ID] {
		o.mu.Unlock()
		return
	}
	o.running[task.ID] = true
	o.mu.Unlock()

	defer func() {
		o.mu.Lock()
		delete(o.running, task.ID)
		o.mu.Unlock()
	}()

	logging.WithTask(task.ID).Info("Processing task", slog.String("title", task.Document.Title))
	o.monitor.Start(task.ID)

	// Notify Slack
	if o.notifier != nil {
		_ = o.notifier.TaskStarted(o.ctx, task.ID, task.Document.Title)
	}

	// Execute task
	// Priority resolution (GH-2386):
	// - Non-Linear adapters (GitHub/GitLab/Jira/Asana/Plane) set task.Priority directly.
	// - Linear adapter populates task.Ticket; prefer Ticket.Priority when present.
	// - task.Ticket is nil for all non-Linear adapters — must guard against deref (GH-2384).
	priority := int(task.Priority)
	if task.Ticket != nil {
		priority = task.Ticket.Priority
	}
	execTask := &executor.Task{
		ID:          task.ID,
		Title:       task.Document.Title,
		Description: task.Document.Markdown,
		Priority:    priority,
		ProjectPath: task.ProjectPath,
		Branch:      task.Branch,
	}

	result, err := o.runner.Execute(o.ctx, execTask)
	if err != nil {
		logging.WithTask(task.ID).Error("Task execution error", slog.Any("error", err))
		o.monitor.Fail(task.ID, err.Error())
		if o.notifier != nil {
			_ = o.notifier.TaskFailed(o.ctx, task.ID, task.Document.Title, err.Error())
		}
		o.fireCompletion(task.ID, "", false, err.Error())
		return
	}

	if !result.Success {
		logging.WithTask(task.ID).Error("Task failed", slog.String("error", result.Error))
		o.monitor.Fail(task.ID, result.Error)
		if o.notifier != nil {
			_ = o.notifier.TaskFailed(o.ctx, task.ID, task.Document.Title, result.Error)
		}
		o.fireCompletion(task.ID, "", false, result.Error)
		return
	}

	logging.WithTask(task.ID).Info("Task completed", slog.Duration("duration", result.Duration))
	o.monitor.Complete(task.ID, result.PRUrl)

	// Notify Slack
	if o.notifier != nil {
		_ = o.notifier.TaskCompleted(o.ctx, task.ID, task.Document.Title, result.PRUrl)
	}

	o.fireCompletion(task.ID, result.PRUrl, true, "")
}

// handleProgress handles progress updates from the executor
func (o *Orchestrator) handleProgress(taskID, phase string, progress int, message string) {
	o.monitor.UpdateProgress(taskID, phase, progress, message)

	// Optionally notify Slack on significant progress
	if progress > 0 && progress%25 == 0 && o.notifier != nil {
		_ = o.notifier.TaskProgress(o.ctx, taskID, phase, progress)
	}

	// Forward to external callback if registered
	o.mu.Lock()
	cb := o.progressCallback
	o.mu.Unlock()
	if cb != nil {
		cb(taskID, phase, progress, message)
	}
}

// fireCompletion calls the completion callback if registered
func (o *Orchestrator) fireCompletion(taskID, prURL string, success bool, errMsg string) {
	o.mu.Lock()
	cb := o.completionCallback
	o.mu.Unlock()
	if cb != nil {
		cb(taskID, prURL, success, errMsg)
	}
}

// saveTaskDocument saves a task document to the project
func (o *Orchestrator) saveTaskDocument(projectPath string, doc *TaskDocument) error {
	taskDir := filepath.Join(projectPath, ".agent", "tasks")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		return err
	}

	filename := filepath.Join(taskDir, fmt.Sprintf("%s.md", doc.ID))
	return os.WriteFile(filename, []byte(doc.Markdown), 0644)
}
