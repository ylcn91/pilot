package telegram

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/comms"
)

// handleStatus shows current status with running/pending/queue info
func (c *CommandHandler) handleStatus(ctx context.Context, chatID string) {
	var pending *comms.PendingTask
	var running *comms.RunningTask
	if c.handler.commsHandler != nil {
		pending = c.handler.commsHandler.GetPendingTask(chatID)
		running = c.handler.commsHandler.GetRunningTask(chatID)
	}

	activeProjectPath := c.handler.getActiveProjectPath(chatID)
	projName := filepath.Base(activeProjectPath)
	if info := c.handler.getActiveProjectInfo(chatID); info != nil {
		projName = info.Name
	}

	var sb strings.Builder
	plainText := c.handler.plainTextMode

	if plainText {
		sb.WriteString("📊 Status\n\n")
		sb.WriteString(fmt.Sprintf("📁 Project: %s\n", projName))
	} else {
		sb.WriteString("📊 *Status*\n\n")
		sb.WriteString(fmt.Sprintf("📁 Project: %s\n", escapeMarkdown(projName)))
	}

	// Running task
	if running != nil {
		elapsed := time.Since(running.StartedAt).Round(time.Second)
		if plainText {
			sb.WriteString(fmt.Sprintf("\n🔄 Running: %s\n", running.TaskID))
		} else {
			sb.WriteString(fmt.Sprintf("\n🔄 *Running*: `%s`\n", running.TaskID))
		}
		sb.WriteString(fmt.Sprintf("   ⏱ %s\n", elapsed))
	}

	// Pending task
	if pending != nil {
		age := time.Since(pending.CreatedAt).Round(time.Second)
		if plainText {
			sb.WriteString(fmt.Sprintf("\n⏳ Pending: %s\n", pending.TaskID))
		} else {
			sb.WriteString(fmt.Sprintf("\n⏳ *Pending*: `%s`\n", pending.TaskID))
		}
		sb.WriteString(fmt.Sprintf("   Awaiting confirmation (%s)\n", age))
	}

	// Queue info from memory store
	if c.store != nil {
		queued, err := c.store.GetQueuedTasks(10)
		if err == nil && len(queued) > 0 {
			if plainText {
				sb.WriteString(fmt.Sprintf("\n📋 Queue: %d task(s)\n", len(queued)))
			} else {
				sb.WriteString(fmt.Sprintf("\n📋 *Queue*: %d task(s)\n", len(queued)))
			}
		}
	}

	// No activity
	if running == nil && pending == nil {
		sb.WriteString("\n✅ Ready for tasks")
	}

	_, _ = c.handler.client.SendMessage(ctx, chatID, sb.String(), c.handler.getParseMode())
}

// handleCancel cancels pending or running task
func (c *CommandHandler) handleCancel(ctx context.Context, chatID string) {
	if c.handler.commsHandler != nil {
		if err := c.handler.commsHandler.CancelTask(ctx, chatID); err != nil {
			_, _ = c.handler.client.SendMessage(ctx, chatID, "No task to cancel.", "")
		}
		return
	}
	_, _ = c.handler.client.SendMessage(ctx, chatID, "No task to cancel.", "")
}

// handleQueue shows queued tasks
func (c *CommandHandler) handleQueue(ctx context.Context, chatID string) {
	if c.store == nil {
		_, _ = c.handler.client.SendMessage(ctx, chatID, "📋 Queue not available (no memory store)", "")
		return
	}

	queued, err := c.store.GetQueuedTasks(10)
	if err != nil {
		_, _ = c.handler.client.SendMessage(ctx, chatID, "❌ Failed to fetch queue", "")
		return
	}

	if len(queued) == 0 {
		// Show pending task as fallback
		if c.handler.commsHandler != nil && c.handler.commsHandler.GetPendingTask(chatID) != nil {
			_, _ = c.handler.client.SendMessage(ctx, chatID,
				"📋 No queued tasks\n⏳ 1 pending confirmation", "")
		} else {
			_, _ = c.handler.client.SendMessage(ctx, chatID, "📋 Queue is empty", "")
		}
		return
	}

	var sb strings.Builder
	plainText := c.handler.plainTextMode

	if plainText {
		sb.WriteString("📋 Task Queue\n\n")
	} else {
		sb.WriteString("📋 *Task Queue*\n\n")
	}

	for i, task := range queued {
		age := time.Since(task.CreatedAt).Round(time.Minute)
		if plainText {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, task.TaskID))
		} else {
			sb.WriteString(fmt.Sprintf("%d. `%s`\n", i+1, task.TaskID))
		}
		sb.WriteString(fmt.Sprintf("   📁 %s • ⏱ %s ago\n\n", filepath.Base(task.ProjectPath), age))
	}

	_, _ = c.handler.client.SendMessage(ctx, chatID, sb.String(), c.handler.getParseMode())
}

// handleStop stops a running task
func (c *CommandHandler) handleStop(ctx context.Context, chatID string) {
	if c.handler.commsHandler != nil {
		running := c.handler.commsHandler.GetRunningTask(chatID)
		if running == nil {
			_, _ = c.handler.client.SendMessage(ctx, chatID, "No task is currently running.", "")
			return
		}
		_ = c.handler.commsHandler.CancelTask(ctx, chatID)
		var text string
		if c.handler.plainTextMode {
			text = fmt.Sprintf("🛑 Stopped task %s", running.TaskID)
		} else {
			text = fmt.Sprintf("🛑 Stopped task `%s`", running.TaskID)
		}
		_, _ = c.handler.client.SendMessage(ctx, chatID, text, c.handler.getParseMode())
		return
	}
	_, _ = c.handler.client.SendMessage(ctx, chatID, "No task is currently running.", "")
}
