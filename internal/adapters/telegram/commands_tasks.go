package telegram

import (
	"context"
	"fmt"
	"time"

	"github.com/ylcn91/pilot/internal/comms"
)

// handleTasks shows task backlog
func (c *CommandHandler) handleTasks(ctx context.Context, chatID string) {
	taskList := c.handler.fastListTasks()
	if taskList == "" {
		_, _ = c.handler.client.SendMessage(ctx, chatID, "📋 No tasks found in .agent/tasks/", "")
		return
	}
	var text string
	if c.handler.plainTextMode {
		text = "📋 Task Backlog\n\n" + taskList
	} else {
		text = "📋 *Task Backlog*\n\n" + taskList
	}
	_, _ = c.handler.client.SendMessage(ctx, chatID, text, c.handler.getParseMode())
}

// handleNoPR executes a task without creating a PR
func (c *CommandHandler) handleNoPR(ctx context.Context, chatID, description string) {
	taskID := fmt.Sprintf("TG-%d", time.Now().Unix())
	_, _ = c.handler.client.SendMessage(ctx, chatID,
		fmt.Sprintf("🚀 Executing without PR: %s", truncateForDisplay(description, 50)), "")
	if c.handler.commsHandler != nil {
		forcePR := false
		c.handler.commsHandler.ExecuteDirectTask(ctx, chatID, "", taskID, description, &comms.DirectTaskOpts{
			ForcePR: &forcePR,
		})
	}
}

// handleForcePR executes a task and forces PR creation
func (c *CommandHandler) handleForcePR(ctx context.Context, chatID, description string) {
	taskID := fmt.Sprintf("TG-%d", time.Now().Unix())
	_, _ = c.handler.client.SendMessage(ctx, chatID,
		fmt.Sprintf("🚀 Executing with PR: %s", truncateForDisplay(description, 50)), "")
	if c.handler.commsHandler != nil {
		forcePR := true
		c.handler.commsHandler.ExecuteDirectTask(ctx, chatID, "", taskID, description, &comms.DirectTaskOpts{
			ForcePR: &forcePR,
		})
	}
}

// truncateForDisplay truncates a string for display purposes
func truncateForDisplay(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
