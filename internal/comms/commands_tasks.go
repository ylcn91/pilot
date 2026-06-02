package comms

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

// handleCancel cancels pending or running task.
func (c *CommandHandler) handleCancel(ctx context.Context, contextID string) {
	if c.cancelTaskFunc != nil {
		if err := c.cancelTaskFunc(ctx, contextID); err == nil {
			return
		}
	}
	_ = c.messenger.SendText(ctx, contextID, "No task to cancel.")
}

// handleQueue shows queued tasks.
func (c *CommandHandler) handleQueue(ctx context.Context, contextID string) {
	if c.store == nil {
		_ = c.messenger.SendText(ctx, contextID, "📋 Queue not available (no memory store)")
		return
	}

	queued, err := c.store.GetQueuedTasks(10)
	if err != nil {
		_ = c.messenger.SendText(ctx, contextID, "❌ Failed to fetch queue")
		return
	}

	if len(queued) == 0 {
		_ = c.messenger.SendText(ctx, contextID, "📋 Queue is empty")
		return
	}

	var sb strings.Builder
	sb.WriteString("📋 Task Queue\n\n")

	for i, task := range queued {
		age := time.Since(task.CreatedAt).Round(time.Minute)
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, task.TaskID))
		sb.WriteString(fmt.Sprintf("   📁 %s • ⏱ %s ago\n\n", filepath.Base(task.ProjectPath), age))
	}

	_ = c.messenger.SendText(ctx, contextID, sb.String())
}

// handleHistory shows recent task history.
func (c *CommandHandler) handleHistory(ctx context.Context, contextID string) {
	if c.store == nil {
		_ = c.messenger.SendText(ctx, contextID, "📜 History not available (no memory store)")
		return
	}

	executions, err := c.store.GetRecentExecutions(10)
	if err != nil {
		_ = c.messenger.SendText(ctx, contextID, "❌ Failed to fetch history")
		return
	}

	if len(executions) == 0 {
		_ = c.messenger.SendText(ctx, contextID, "📜 No task history yet")
		return
	}

	var sb strings.Builder
	sb.WriteString("📜 Recent Tasks\n\n")

	for _, exec := range executions {
		// Status emoji
		emoji := "⏳"
		switch exec.Status {
		case "completed":
			emoji = "✅"
		case "failed":
			emoji = "❌"
		case "running":
			emoji = "🔄"
		}

		// Format duration
		duration := ""
		if exec.DurationMs > 0 {
			d := time.Duration(exec.DurationMs) * time.Millisecond
			duration = fmt.Sprintf(" • %s", d.Round(time.Second))
		}

		// Format time
		age := FormatTimeAgo(exec.CreatedAt)

		sb.WriteString(fmt.Sprintf("%s %s\n", emoji, exec.TaskID))
		sb.WriteString(fmt.Sprintf("   %s%s\n", age, duration))

		// Add PR link if present
		if exec.PRUrl != "" {
			sb.WriteString(fmt.Sprintf("   PR: %s\n", exec.PRUrl))
		}
		sb.WriteString("\n")
	}

	_ = c.messenger.SendText(ctx, contextID, sb.String())
}

// handleBudget shows usage and costs.
func (c *CommandHandler) handleBudget(ctx context.Context, contextID string) {
	if c.store == nil {
		_ = c.messenger.SendText(ctx, contextID, "💰 Budget not available (no memory store)")
		return
	}

	// Get current month's usage
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)

	summary, err := c.store.GetUsageSummary(memory.UsageQuery{
		Start: monthStart,
		End:   now,
	})
	if err != nil {
		_ = c.messenger.SendText(ctx, contextID, "❌ Failed to fetch usage data")
		return
	}

	var sb strings.Builder
	sb.WriteString("💰 Usage This Month\n\n")

	// Task count
	sb.WriteString(fmt.Sprintf("🎯 Tasks: %d\n", summary.TaskCount))

	// Token usage
	if summary.TokensTotal > 0 {
		tokensK := float64(summary.TokensTotal) / 1000
		sb.WriteString(fmt.Sprintf("🔤 Tokens: %.1fK\n", tokensK))
	}

	// Compute time
	if summary.ComputeMinutes > 0 {
		sb.WriteString(fmt.Sprintf("⏱ Compute: %d min\n", summary.ComputeMinutes))
	}

	// Costs breakdown
	sb.WriteString("\nCosts\n")
	if summary.TaskCost > 0 {
		sb.WriteString(fmt.Sprintf("• Tasks: $%.2f\n", summary.TaskCost))
	}
	if summary.TokenCost > 0 {
		sb.WriteString(fmt.Sprintf("• Tokens: $%.2f\n", summary.TokenCost))
	}
	if summary.ComputeCost > 0 {
		sb.WriteString(fmt.Sprintf("• Compute: $%.2f\n", summary.ComputeCost))
	}

	// Total
	sb.WriteString(fmt.Sprintf("\nTotal: $%.2f\n", summary.TotalCost))

	// Period info
	sb.WriteString(fmt.Sprintf("Period: %s - %s", monthStart.Format("Jan 2"), now.Format("Jan 2")))

	_ = c.messenger.SendText(ctx, contextID, sb.String())
}

// handleTasks shows task backlog.
func (c *CommandHandler) handleTasks(ctx context.Context, contextID string) {
	if c.listTasksFunc == nil {
		_ = c.messenger.SendText(ctx, contextID, "📋 No tasks found in .agent/tasks/")
		return
	}

	taskList := c.listTasksFunc()
	if taskList == "" {
		_ = c.messenger.SendText(ctx, contextID, "📋 No tasks found in .agent/tasks/")
		return
	}

	text := "📋 Task Backlog\n\n" + taskList
	_ = c.messenger.SendText(ctx, contextID, text)
}

// handleStop stops a running task.
func (c *CommandHandler) handleStop(ctx context.Context, contextID string) {
	if c.stopTaskFunc != nil {
		if err := c.stopTaskFunc(ctx, contextID); err == nil {
			return
		}
	}
	_ = c.messenger.SendText(ctx, contextID, "No task is currently running.")
}

// handleBrief generates and sends a daily brief on demand.
func (c *CommandHandler) handleBrief(ctx context.Context, contextID string) {
	if c.briefGeneratorFunc != nil {
		// Use the platform-specific brief generator (e.g., from Telegram adapter)
		_ = c.briefGeneratorFunc(ctx, contextID)
		return
	}

	if c.store == nil {
		_ = c.messenger.SendText(ctx, contextID, "📋 Brief not available (no memory store)")
		return
	}

	_ = c.messenger.SendText(ctx, contextID, "📊 Brief generation not configured")
}

// handleNoPR executes a task without creating a PR.
func (c *CommandHandler) handleNoPR(ctx context.Context, contextID, description string) {
	if c.runCommandFunc != nil {
		taskID := fmt.Sprintf("CMD-%d", time.Now().Unix())
		_ = c.messenger.SendText(ctx, contextID,
			fmt.Sprintf("🚀 Executing without PR: %s", TruncateText(description, 50)))
		// Note: In Telegram, this calls executeTaskWithOptions with forcePR=false
		// For shared handler, we just invoke the run command and let the adapter handle noPR semantics
		c.runCommandFunc(ctx, contextID, taskID)
	} else {
		_ = c.messenger.SendText(ctx, contextID,
			fmt.Sprintf("🚀 Executing without PR: %s", TruncateText(description, 50)))
	}
}

// handleForcePR executes a task and forces PR creation.
func (c *CommandHandler) handleForcePR(ctx context.Context, contextID, description string) {
	if c.runCommandFunc != nil {
		taskID := fmt.Sprintf("CMD-%d", time.Now().Unix())
		_ = c.messenger.SendText(ctx, contextID,
			fmt.Sprintf("🚀 Executing with PR: %s", TruncateText(description, 50)))
		// Note: In Telegram, this calls executeTaskWithOptions with forcePR=true
		// For shared handler, we just invoke the run command and let the adapter handle forcePR semantics
		c.runCommandFunc(ctx, contextID, taskID)
	} else {
		_ = c.messenger.SendText(ctx, contextID,
			fmt.Sprintf("🚀 Executing with PR: %s", TruncateText(description, 50)))
	}
}
