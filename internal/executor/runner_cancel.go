package executor

import (
	"fmt"
	"log/slog"
	"os/exec"
	"syscall"
	"time"
)

// Cancel terminates a running task by killing its Claude Code process.
// Returns an error if the task is not currently running.
func (r *Runner) Cancel(taskID string) error {
	r.mu.Lock()
	cmd, ok := r.running[taskID]
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("task %s is not running", taskID)
	}

	return cmd.Process.Kill()
}

// CancelAll terminates all running subprocesses gracefully.
// It sends SIGTERM to allow processes to clean up, then forcefully kills
// any remaining processes after a 10-second grace period.
// This is called during graceful shutdown to prevent orphaned Claude Code processes.
func (r *Runner) CancelAll() {
	r.mu.Lock()
	// Copy running map to avoid holding lock during signals
	toCancel := make(map[string]*exec.Cmd, len(r.running))
	for id, cmd := range r.running {
		toCancel[id] = cmd
	}
	r.mu.Unlock()

	if len(toCancel) == 0 {
		return
	}

	r.log.Info("Cancelling all running tasks", slog.Int("count", len(toCancel)))

	// Send SIGTERM to all processes for graceful shutdown
	for id, cmd := range toCancel {
		if cmd.Process != nil {
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				r.log.Debug("Failed to send SIGTERM", slog.String("task_id", id), slog.Any("error", err))
			} else {
				r.log.Debug("Sent SIGTERM to process", slog.String("task_id", id), slog.Int("pid", cmd.Process.Pid))
			}
		}
	}

	// Wait 10s, then SIGKILL any remaining
	time.AfterFunc(10*time.Second, func() {
		r.mu.Lock()
		remaining := make(map[string]*exec.Cmd, len(r.running))
		for id, cmd := range r.running {
			remaining[id] = cmd
		}
		r.mu.Unlock()

		for id, cmd := range remaining {
			if cmd.Process != nil {
				if err := cmd.Process.Kill(); err != nil {
					r.log.Debug("Failed to kill process", slog.String("task_id", id), slog.Any("error", err))
				} else {
					r.log.Info("Force killed process after grace period", slog.String("task_id", id), slog.Int("pid", cmd.Process.Pid))
				}
			}
		}
	})
}

// IsRunning returns true if the specified task is currently being executed.
func (r *Runner) IsRunning(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.running[taskID]
	return ok
}
