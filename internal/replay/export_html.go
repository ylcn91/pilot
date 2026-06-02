package replay

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func renderHTMLHeader(recording *Recording) string {
	var sb strings.Builder

	statusClass := "status-completed"
	switch recording.Status {
	case "failed":
		statusClass = "status-failed"
	case "cancelled":
		statusClass = "status-cancelled"
	}

	sb.WriteString("<div class=\"container\">\n")
	sb.WriteString("<div class=\"header\">\n")
	sb.WriteString(fmt.Sprintf("<h1>📹 %s <span class=\"status-badge %s\">%s</span></h1>\n",
		recording.ID, statusClass, recording.Status))
	sb.WriteString(fmt.Sprintf("<div class=\"subtitle\">Task: %s</div>\n", escapeHTML(recording.TaskID)))
	sb.WriteString(fmt.Sprintf("<div class=\"subtitle\">Project: %s</div>\n", escapeHTML(recording.ProjectPath)))
	sb.WriteString(fmt.Sprintf("<div class=\"subtitle\">Executed: %s</div>\n",
		recording.StartTime.Format("January 2, 2006 at 15:04:05")))
	sb.WriteString("</div>\n")

	return sb.String()
}

func renderHTMLSummary(recording *Recording) string {
	var sb strings.Builder

	sb.WriteString("<div class=\"summary-grid\">\n")

	// Duration
	sb.WriteString("<div class=\"summary-card\">\n")
	sb.WriteString("<div class=\"label\">Duration</div>\n")
	sb.WriteString(fmt.Sprintf("<div class=\"value\">%s</div>\n", formatDuration(recording.Duration)))
	sb.WriteString("</div>\n")

	// Events
	sb.WriteString("<div class=\"summary-card\">\n")
	sb.WriteString("<div class=\"label\">Total Events</div>\n")
	sb.WriteString(fmt.Sprintf("<div class=\"value\">%d</div>\n", recording.EventCount))
	sb.WriteString("</div>\n")

	// Tokens
	if recording.TokenUsage != nil {
		sb.WriteString("<div class=\"summary-card\">\n")
		sb.WriteString("<div class=\"label\">Total Tokens</div>\n")
		sb.WriteString(fmt.Sprintf("<div class=\"value\">%s</div>\n", formatNumber(recording.TokenUsage.TotalTokens)))
		sb.WriteString(fmt.Sprintf("<div class=\"subvalue\">%s in / %s out</div>\n",
			formatNumber(recording.TokenUsage.InputTokens),
			formatNumber(recording.TokenUsage.OutputTokens)))
		sb.WriteString("</div>\n")

		// Cost
		sb.WriteString("<div class=\"summary-card\">\n")
		sb.WriteString("<div class=\"label\">Estimated Cost</div>\n")
		sb.WriteString(fmt.Sprintf("<div class=\"value\">$%.4f</div>\n", recording.TokenUsage.EstimatedCostUSD))
		sb.WriteString("</div>\n")
	}

	// Model
	if recording.Metadata != nil && recording.Metadata.ModelName != "" {
		sb.WriteString("<div class=\"summary-card\">\n")
		sb.WriteString("<div class=\"label\">Model</div>\n")
		sb.WriteString(fmt.Sprintf("<div class=\"value\" style=\"font-size: 16px;\">%s</div>\n",
			escapeHTML(recording.Metadata.ModelName)))
		sb.WriteString("</div>\n")
	}

	// Navigator
	if recording.Metadata != nil && recording.Metadata.HasNavigator {
		sb.WriteString("<div class=\"summary-card\">\n")
		sb.WriteString("<div class=\"label\">Navigator</div>\n")
		sb.WriteString("<div class=\"value\" style=\"color: var(--accent-green);\">✓ Enabled</div>\n")
		sb.WriteString("</div>\n")
	}

	sb.WriteString("</div>\n")

	return sb.String()
}

func renderHTMLTokenBreakdown(report *AnalysisReport) string {
	var sb strings.Builder

	sb.WriteString("<div class=\"section\">\n")
	sb.WriteString("<h2><span class=\"icon\">📊</span> Token Breakdown</h2>\n")

	// By phase
	if len(report.TokenBreakdown.ByPhase) > 0 {
		sb.WriteString("<h3 style=\"margin-bottom: 12px; color: var(--text-secondary);\">By Phase</h3>\n")

		total := report.Recording.TokenUsage.TotalTokens
		for phase, usage := range report.TokenBreakdown.ByPhase {
			pct := float64(usage.TotalTokens) / float64(total) * 100
			sb.WriteString("<div class=\"chart-bar\">\n")
			sb.WriteString(fmt.Sprintf("<div class=\"chart-label\">%s</div>\n", phase))
			sb.WriteString("<div class=\"chart-bar-container\">\n")
			sb.WriteString(fmt.Sprintf("<div class=\"chart-bar-fill phase-%s\" style=\"width: %.1f%%;\"></div>\n",
				strings.ToLower(phase), pct))
			sb.WriteString("</div>\n")
			sb.WriteString(fmt.Sprintf("<div class=\"chart-value\">%s (%.1f%%)</div>\n",
				formatNumber(usage.TotalTokens), pct))
			sb.WriteString("</div>\n")
		}
	}

	sb.WriteString("</div>\n")

	return sb.String()
}

func renderHTMLPhaseChart(report *AnalysisReport, totalDuration time.Duration) string {
	var sb strings.Builder

	sb.WriteString("<div class=\"section\">\n")
	sb.WriteString("<h2><span class=\"icon\">⏱️</span> Phase Timing</h2>\n")

	for _, phase := range report.PhaseAnalysis {
		pct := phase.Percentage
		sb.WriteString("<div class=\"chart-bar\">\n")
		sb.WriteString(fmt.Sprintf("<div class=\"chart-label\">%s</div>\n", phase.Phase))
		sb.WriteString("<div class=\"chart-bar-container\">\n")
		phaseClass := strings.ToLower(strings.ReplaceAll(phase.Phase, " ", "-"))
		sb.WriteString(fmt.Sprintf("<div class=\"chart-bar-fill phase-%s\" style=\"width: %.1f%%;\"></div>\n",
			phaseClass, pct))
		sb.WriteString("</div>\n")
		sb.WriteString(fmt.Sprintf("<div class=\"chart-value\">%s (%.1f%%)</div>\n",
			formatDuration(phase.Duration), pct))
		sb.WriteString("</div>\n")
	}

	sb.WriteString("</div>\n")

	return sb.String()
}

func renderHTMLToolUsage(report *AnalysisReport) string {
	var sb strings.Builder

	sb.WriteString("<div class=\"section\">\n")
	sb.WriteString("<h2><span class=\"icon\">🔧</span> Tool Usage</h2>\n")
	sb.WriteString("<div class=\"tool-grid\">\n")

	// Sort by count
	tools := make([]ToolUsageStats, len(report.ToolUsage))
	copy(tools, report.ToolUsage)
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Count > tools[j].Count
	})

	for _, tool := range tools {
		icon := getToolIcon(tool.Tool)
		errorStr := ""
		if tool.ErrorCount > 0 {
			errorStr = fmt.Sprintf(" <span style=\"color: var(--accent-red);\">(%d errors)</span>", tool.ErrorCount)
		}

		sb.WriteString("<div class=\"tool-card\">\n")
		sb.WriteString(fmt.Sprintf("<div class=\"tool-name\">%s %s</div>\n", icon, tool.Tool))
		sb.WriteString(fmt.Sprintf("<div class=\"tool-stats\">%d calls%s</div>\n", tool.Count, errorStr))
		if tool.InputTokens > 0 || tool.OutputTokens > 0 {
			sb.WriteString(fmt.Sprintf("<div class=\"tool-stats\">%s tokens</div>\n",
				formatNumber(tool.InputTokens+tool.OutputTokens)))
		}
		sb.WriteString("</div>\n")
	}

	sb.WriteString("</div>\n")
	sb.WriteString("</div>\n")

	return sb.String()
}

func renderHTMLErrors(report *AnalysisReport) string {
	var sb strings.Builder

	sb.WriteString("<div class=\"section\">\n")
	sb.WriteString(fmt.Sprintf("<h2><span class=\"icon\">❌</span> Errors (%d)</h2>\n", len(report.Errors)))
	sb.WriteString("<div class=\"error-list\">\n")

	for _, err := range report.Errors {
		sb.WriteString("<div class=\"error-item\">\n")
		sb.WriteString(fmt.Sprintf("<div class=\"error-meta\">#%d at %s | Phase: %s | Tool: %s</div>\n",
			err.Sequence, err.Timestamp.Format("15:04:05"), err.Phase, err.Tool))
		sb.WriteString(fmt.Sprintf("<div class=\"error-message\">%s</div>\n", escapeHTML(err.Message)))
		sb.WriteString("</div>\n")
	}

	sb.WriteString("</div>\n")
	sb.WriteString("</div>\n")

	return sb.String()
}

func renderHTMLTimeline(events []*StreamEvent) string {
	var sb strings.Builder

	sb.WriteString("<div class=\"section\">\n")
	sb.WriteString(fmt.Sprintf("<h2><span class=\"icon\">📜</span> Execution Timeline (%d events)</h2>\n", len(events)))
	sb.WriteString("<div class=\"timeline\">\n")

	for _, event := range events {
		class := "timeline-event"
		icon := "📝"

		if event.Parsed != nil {
			p := event.Parsed
			if p.IsError {
				class += " error"
				icon = "❌"
			} else if p.ToolName != "" {
				class += " tool"
				icon = getToolIcon(p.ToolName)
			} else if p.Text != "" {
				class += " text"
				icon = "💬"
			} else if p.Type == "result" {
				class += " result"
				icon = "✅"
			} else if p.Type == "system" {
				icon = "⚙️"
			}
		}

		content := formatEventForTimeline(event)

		sb.WriteString(fmt.Sprintf("<div class=\"%s\">\n", class))
		sb.WriteString(fmt.Sprintf("<span class=\"timeline-time\">%s</span>\n",
			event.Timestamp.Format("15:04:05.000")))
		sb.WriteString(fmt.Sprintf("<span class=\"timeline-seq\">#%d</span>\n", event.Sequence))
		sb.WriteString(fmt.Sprintf("<span class=\"timeline-icon\">%s</span>\n", icon))
		sb.WriteString(fmt.Sprintf("<span class=\"timeline-content\">%s</span>\n", escapeHTML(content)))
		sb.WriteString("</div>\n")
	}

	sb.WriteString("</div>\n")
	sb.WriteString("</div>\n")
	sb.WriteString("</div>\n") // Close container

	return sb.String()
}

func formatEventForTimeline(event *StreamEvent) string {
	if event.Parsed == nil {
		return fmt.Sprintf("(%s)", event.Type)
	}

	p := event.Parsed

	switch p.Type {
	case "system":
		if p.Subtype == "init" {
			return "System initialized"
		}
		return fmt.Sprintf("System: %s", p.Subtype)

	case "assistant":
		if p.ToolName != "" {
			detail := formatToolDetail(p)
			if detail != "" {
				return fmt.Sprintf("%s: %s", p.ToolName, truncate(detail, 100))
			}
			return p.ToolName
		}
		if p.Text != "" {
			return truncate(strings.ReplaceAll(p.Text, "\n", " "), 150)
		}
		return "(assistant)"

	case "user":
		return "Tool result received"

	case "result":
		if p.IsError {
			return fmt.Sprintf("Error: %s", truncate(p.Result, 100))
		}
		if p.InputTokens > 0 || p.OutputTokens > 0 {
			return fmt.Sprintf("Completed (%d in, %d out tokens)", p.InputTokens, p.OutputTokens)
		}
		return "Completed"

	default:
		return fmt.Sprintf("(%s)", p.Type)
	}
}
