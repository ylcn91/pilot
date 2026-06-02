package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/memory"
)

func newBudgetStatusCmd() *cobra.Command {
	var userID string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current budget status",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			// Open store
			store, err := memory.NewStore(cfg.Memory.Path)
			if err != nil {
				return fmt.Errorf("failed to open memory store: %w", err)
			}
			defer func() { _ = store.Close() }()

			// Create enforcer
			budgetCfg := cfg.Budget
			if budgetCfg == nil {
				budgetCfg = budget.DefaultConfig()
			}

			enforcer := budget.NewEnforcer(budgetCfg, store)

			// Get status
			ctx := context.Background()
			status, err := enforcer.GetStatus(ctx, "", userID)
			if err != nil {
				return fmt.Errorf("failed to get budget status: %w", err)
			}

			// Render and print
			output := renderBudgetStatus(budgetCfg, status)
			fmt.Print(output)

			return nil
		},
	}

	cmd.Flags().StringVar(&userID, "user", "", "Filter by user ID")

	return cmd
}

// renderBudgetStatus renders the complete budget status display
func renderBudgetStatus(cfg *budget.Config, status *budget.Status) string {
	var b strings.Builder

	// Header
	b.WriteString(budgetHeaderStyle.Render("BUDGET STATUS"))
	b.WriteString("\n")
	b.WriteString(budgetDivider())
	b.WriteString("\n\n")

	// Status indicator line
	if !cfg.Enabled {
		b.WriteString("  ")
		b.WriteString(budgetDimStyle.Render("[DISABLED]"))
		b.WriteString(" Enable with: budget.enabled: true\n\n")
	} else if status.DailyPercent >= 100 || status.MonthlyPercent >= 100 {
		b.WriteString("  ")
		b.WriteString(budgetErrorStyle.Render("[X] Budget limit exceeded"))
		b.WriteString("\n\n")
	} else if status.DailyPercent >= 80 || status.MonthlyPercent >= 80 {
		b.WriteString("  ")
		if status.DailyPercent >= 80 && status.DailyPercent < 100 {
			b.WriteString(budgetWarnStyle.Render("[!] Approaching daily limit"))
		} else {
			b.WriteString(budgetWarnStyle.Render("[!] Approaching monthly limit"))
		}
		b.WriteString("\n\n")
	}

	// Paused/blocked warnings
	if status.IsPaused {
		b.WriteString("  ")
		b.WriteString(budgetErrorStyle.Render(fmt.Sprintf("[PAUSED] %s", status.PauseReason)))
		b.WriteString("\n\n")
	}
	if status.BlockedTasks > 0 {
		b.WriteString("  ")
		b.WriteString(budgetWarnStyle.Render(fmt.Sprintf("[!] %d task(s) blocked", status.BlockedTasks)))
		b.WriteString("\n\n")
	}

	// Daily budget
	b.WriteString(formatBudgetLine("Daily", status.DailySpent, status.DailyLimit, status.DailyPercent))
	b.WriteString("\n")

	// Monthly budget
	b.WriteString(formatBudgetLine("Monthly", status.MonthlySpent, status.MonthlyLimit, status.MonthlyPercent))

	// Footer section
	b.WriteString(budgetDivider())
	b.WriteString("\n")

	// Limits line
	tokensStr := formatTokensCompact(cfg.PerTask.MaxTokens)
	durationStr := formatDurationCompact(cfg.PerTask.MaxDuration)
	b.WriteString(fmt.Sprintf("  %-14s%s tokens    %s duration\n", "Limits", tokensStr, durationStr))

	// Enforcement line
	b.WriteString(fmt.Sprintf("  %-14sdaily:%-7s monthly:%-7s task:%s\n",
		"Enforcement",
		cfg.OnExceed.Daily,
		cfg.OnExceed.Monthly,
		cfg.OnExceed.PerTask,
	))

	b.WriteString(budgetDivider())
	b.WriteString("\n")

	// Timestamp (right-aligned)
	timestamp := formatRelativeTimestamp(status.LastUpdated)
	padding := budgetWidth - len(timestamp)
	if padding < 0 {
		padding = 0
	}
	b.WriteString(budgetDimStyle.Render(fmt.Sprintf("%s%s", strings.Repeat(" ", padding), timestamp)))
	b.WriteString("\n")

	return b.String()
}

// formatBudgetLine formats a single budget line (label + values + bar)
func formatBudgetLine(label string, spent, limit, percent float64) string {
	var b strings.Builder

	// Format values
	values := fmt.Sprintf("$%.2f / $%.2f", spent, limit)

	// Calculate padding for right-aligned percentage
	// Layout: "  {label:14}{values}{padding}{percent:4}%"
	percentStr := fmt.Sprintf("%.0f%%", percent)
	contentLen := 2 + budgetLabelCol + len(values) + len(percentStr)
	padding := budgetWidth - contentLen
	if padding < 1 {
		padding = 1
	}

	// First line: label, values, percent
	b.WriteString(fmt.Sprintf("  %-*s%s%s%s\n",
		budgetLabelCol, label,
		values,
		strings.Repeat(" ", padding),
		percentStr,
	))

	// Second line: progress bar (indented to align with values)
	b.WriteString(fmt.Sprintf("  %s%s\n",
		strings.Repeat(" ", budgetLabelCol),
		renderColoredBar(percent, budgetBarWidth),
	))

	return b.String()
}

// renderColoredBar renders a progress bar with color based on percentage
func renderColoredBar(percent float64, width int) string {
	// Determine color
	color := budgetBarNormal
	if percent >= 100 {
		color = budgetBarError
	} else if percent >= 80 {
		color = budgetBarWarn
	}

	// Clamp percent
	if percent > 100 {
		percent = 100
	}
	if percent < 0 {
		percent = 0
	}

	filled := int(float64(width) * percent / 100)
	empty := width - filled

	filledStyle := lipgloss.NewStyle().Foreground(color)
	emptyStyle := budgetDimStyle

	return filledStyle.Render(strings.Repeat("█", filled)) +
		emptyStyle.Render(strings.Repeat("░", empty))
}

// formatRelativeTimestamp returns a human-friendly timestamp
func formatRelativeTimestamp(t time.Time) string {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterday := today.AddDate(0, 0, -1)

	timeStr := t.Format("15:04")

	if t.After(today) {
		return fmt.Sprintf("Updated %s today", timeStr)
	}
	if t.After(yesterday) {
		return fmt.Sprintf("Updated %s yesterday", timeStr)
	}
	return fmt.Sprintf("Updated %s", t.Format("2006-01-02 15:04"))
}

// formatTokensCompact formats token count compactly (e.g., "100k")
func formatTokensCompact(tokens int64) string {
	if tokens <= 0 {
		return "unlimited"
	}
	if tokens >= 1000000 {
		return fmt.Sprintf("%.0fM", float64(tokens)/1000000)
	}
	if tokens >= 1000 {
		return fmt.Sprintf("%.0fk", float64(tokens)/1000)
	}
	return fmt.Sprintf("%d", tokens)
}

// formatDurationCompact formats duration compactly (e.g., "30m")
func formatDurationCompact(d time.Duration) string {
	if d <= 0 {
		return "unlimited"
	}
	if d >= time.Hour {
		h := d.Hours()
		if h == float64(int(h)) {
			return fmt.Sprintf("%.0fh", h)
		}
		return fmt.Sprintf("%.1fh", h)
	}
	if d >= time.Minute {
		return fmt.Sprintf("%.0fm", d.Minutes())
	}
	return fmt.Sprintf("%.0fs", d.Seconds())
}
