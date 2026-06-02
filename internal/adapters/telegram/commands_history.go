package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/briefs"
	"github.com/ylcn91/pilot/internal/memory"
)

// handleHistory shows recent task history
func (c *CommandHandler) handleHistory(ctx context.Context, chatID string) {
	if c.store == nil {
		_, _ = c.handler.client.SendMessage(ctx, chatID, "📜 History not available (no memory store)", "")
		return
	}

	executions, err := c.store.GetRecentExecutions(10)
	if err != nil {
		_, _ = c.handler.client.SendMessage(ctx, chatID, "❌ Failed to fetch history", "")
		return
	}

	if len(executions) == 0 {
		_, _ = c.handler.client.SendMessage(ctx, chatID, "📜 No task history yet", "")
		return
	}

	var sb strings.Builder
	plainText := c.handler.plainTextMode

	if plainText {
		sb.WriteString("📜 Recent Tasks\n\n")
	} else {
		sb.WriteString("📜 *Recent Tasks*\n\n")
	}

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
		age := formatTimeAgo(exec.CreatedAt)

		if plainText {
			sb.WriteString(fmt.Sprintf("%s %s\n", emoji, exec.TaskID))
		} else {
			sb.WriteString(fmt.Sprintf("%s `%s`\n", emoji, exec.TaskID))
		}
		sb.WriteString(fmt.Sprintf("   %s%s\n", age, duration))

		// Add PR link if present
		if exec.PRUrl != "" {
			if plainText {
				sb.WriteString(fmt.Sprintf("   PR: %s\n", exec.PRUrl))
			} else {
				sb.WriteString(fmt.Sprintf("   [PR](%s)\n", exec.PRUrl))
			}
		}
		sb.WriteString("\n")
	}

	_, _ = c.handler.client.SendMessage(ctx, chatID, sb.String(), c.handler.getParseMode())
}

// handleBudget shows usage and costs
func (c *CommandHandler) handleBudget(ctx context.Context, chatID string) {
	if c.store == nil {
		_, _ = c.handler.client.SendMessage(ctx, chatID, "💰 Budget not available (no memory store)", "")
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
		_, _ = c.handler.client.SendMessage(ctx, chatID, "❌ Failed to fetch usage data", "")
		return
	}

	var sb strings.Builder
	plainText := c.handler.plainTextMode

	if plainText {
		sb.WriteString("💰 Usage This Month\n\n")
	} else {
		sb.WriteString("💰 *Usage This Month*\n\n")
	}

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
	if plainText {
		sb.WriteString("\nCosts\n")
	} else {
		sb.WriteString("\n*Costs*\n")
	}
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
	if plainText {
		sb.WriteString(fmt.Sprintf("\nTotal: $%.2f\n", summary.TotalCost))
	} else {
		sb.WriteString(fmt.Sprintf("\n*Total*: $%.2f\n", summary.TotalCost))
	}

	// Period info
	if plainText {
		sb.WriteString(fmt.Sprintf("\nPeriod: %s - %s", monthStart.Format("Jan 2"), now.Format("Jan 2")))
	} else {
		sb.WriteString(fmt.Sprintf("\n_Period: %s - %s_", monthStart.Format("Jan 2"), now.Format("Jan 2")))
	}

	_, _ = c.handler.client.SendMessage(ctx, chatID, sb.String(), c.handler.getParseMode())
}

// handleBrief generates and sends a daily brief on demand
func (c *CommandHandler) handleBrief(ctx context.Context, chatID string) {
	if c.store == nil {
		_, _ = c.handler.client.SendMessage(ctx, chatID, "📋 Brief not available (no memory store)", "")
		return
	}

	_, _ = c.handler.client.SendMessage(ctx, chatID, "📊 Generating brief...", "")

	generator := briefs.NewGenerator(c.store, nil)
	brief, err := generator.GenerateDaily()
	if err != nil {
		_, _ = c.handler.client.SendMessage(ctx, chatID,
			fmt.Sprintf("❌ Failed to generate brief: %s", err.Error()), "")
		return
	}

	// Format as plain text for Telegram
	formatter := briefs.NewPlainTextFormatter()
	text, err := formatter.Format(brief)
	if err != nil {
		_, _ = c.handler.client.SendMessage(ctx, chatID,
			fmt.Sprintf("❌ Failed to format brief: %s", err.Error()), "")
		return
	}

	_, _ = c.handler.client.SendMessage(ctx, chatID, text, "")
}

// formatTimeAgo formats a time as relative (e.g., "2h ago", "3d ago")
func formatTimeAgo(t time.Time) string {
	d := time.Since(t)

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2")
	}
}
