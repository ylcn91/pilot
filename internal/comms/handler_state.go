package comms

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ---------- project management ----------

func (h *Handler) getActiveProjectPath(contextID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if path, ok := h.activeProject[contextID]; ok {
		return path
	}
	return h.projectPath
}

// SetActiveProject sets the active project for a context by name.
func (h *Handler) SetActiveProject(contextID, projectName string) error {
	if h.projects == nil {
		return fmt.Errorf("no projects configured")
	}
	proj := h.projects.GetProjectByName(projectName)
	if proj == nil {
		return fmt.Errorf("project '%s' not found", projectName)
	}
	h.mu.Lock()
	h.activeProject[contextID] = proj.Path
	h.mu.Unlock()
	return nil
}

// GetActiveProject returns (name, path) for the active project in a given context.
func (h *Handler) GetActiveProject(contextID string) (string, string) {
	path := h.getActiveProjectPath(contextID)
	if h.projects != nil {
		if proj := h.projects.GetProjectByPath(path); proj != nil {
			return proj.Name, proj.Path
		}
	}
	return "", path
}

// ---------- RBAC ----------

func (h *Handler) resolveMemberID(contextID string) string {
	if h.memberResolver == nil {
		return ""
	}

	h.mu.Lock()
	senderID := h.lastSender[contextID]
	h.mu.Unlock()

	if senderID == "" {
		return ""
	}

	memberID, err := h.memberResolver.ResolveIdentity(senderID)
	if err != nil {
		h.log.Warn("failed to resolve identity",
			slog.String("sender_id", senderID),
			slog.Any("error", err))
		return ""
	}
	return memberID
}

// ---------- state accessors (for CommandHandler wiring) ----------

// GetPendingTask returns the pending task for a context, if any.
func (h *Handler) GetPendingTask(contextID string) *PendingTask {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pendingTasks[contextID]
}

// GetRunningTask returns the running task for a context, if any.
func (h *Handler) GetRunningTask(contextID string) *RunningTask {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.runningTasks[contextID]
}

// CancelTask cancels any pending or running task for a context.
func (h *Handler) CancelTask(ctx context.Context, contextID string) error {
	h.mu.Lock()
	pending, hasPending := h.pendingTasks[contextID]
	if hasPending {
		delete(h.pendingTasks, contextID)
	}
	running, hasRunning := h.runningTasks[contextID]
	if hasRunning {
		running.Cancel()
		delete(h.runningTasks, contextID)
	}
	h.mu.Unlock()

	if hasPending {
		_ = h.messenger.SendText(ctx, contextID, fmt.Sprintf("❌ Cancelled pending task %s", pending.TaskID))
		return nil
	}
	if hasRunning {
		_ = h.messenger.SendText(ctx, contextID, fmt.Sprintf("🛑 Stopping task %s", running.TaskID))
		return nil
	}
	return fmt.Errorf("no task to cancel")
}

// ---------- cleanup ----------

// CleanupLoop runs a background goroutine that removes expired pending tasks.
// Call with go h.CleanupLoop(ctx) and track with a WaitGroup externally.
func (h *Handler) CleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.cleanupExpiredTasks(ctx)
		}
	}
}

func (h *Handler) cleanupExpiredTasks(ctx context.Context) {
	h.mu.Lock()
	var expired []string
	for id, task := range h.pendingTasks {
		if time.Since(task.CreatedAt) > 5*time.Minute {
			expired = append(expired, id)
		}
	}
	for _, id := range expired {
		delete(h.pendingTasks, id)
	}
	h.mu.Unlock()

	for _, id := range expired {
		_ = h.messenger.SendText(ctx, id, "⏰ Pending task expired (5 min timeout). Send a new request.")
	}
}
