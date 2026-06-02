package replay

import (
	"fmt"
	"strings"
	"time"
)

// Analyzer analyzes a recording and generates a report
type Analyzer struct {
	recording *Recording
	events    []*StreamEvent
}

// NewAnalyzer creates a new recording analyzer
func NewAnalyzer(recording *Recording) (*Analyzer, error) {
	events, err := LoadStreamEvents(recording)
	if err != nil {
		return nil, fmt.Errorf("failed to load events: %w", err)
	}

	return &Analyzer{
		recording: recording,
		events:    events,
	}, nil
}

// Analyze generates an analysis report
func (a *Analyzer) Analyze() (*AnalysisReport, error) {
	report := &AnalysisReport{
		Recording: a.recording,
		TokenBreakdown: TokenBreakdown{
			ByPhase: make(map[string]TokenUsage),
			ByTool:  make(map[string]TokenUsage),
		},
		PhaseAnalysis:  make([]PhaseAnalysis, 0),
		ToolUsage:      make([]ToolUsageStats, 0),
		Errors:         make([]ErrorEvent, 0),
		DecisionPoints: make([]DecisionPoint, 0),
	}

	// Track tool usage
	toolStats := make(map[string]*ToolUsageStats)
	currentPhase := "Init"
	phaseStartTime := a.recording.StartTime
	phaseEvents := 0
	phaseTools := make(map[string]bool)

	for _, event := range a.events {
		if event.Parsed == nil {
			continue
		}

		parsed := event.Parsed

		// Track errors
		if parsed.IsError {
			report.Errors = append(report.Errors, ErrorEvent{
				Timestamp: event.Timestamp,
				Phase:     currentPhase,
				Tool:      parsed.ToolName,
				Message:   truncate(parsed.Result, 200),
				Sequence:  event.Sequence,
			})
		}

		// Track tool usage
		if parsed.ToolName != "" {
			if _, exists := toolStats[parsed.ToolName]; !exists {
				toolStats[parsed.ToolName] = &ToolUsageStats{
					Tool: parsed.ToolName,
				}
			}
			stats := toolStats[parsed.ToolName]
			stats.Count++
			stats.InputTokens += parsed.InputTokens
			stats.OutputTokens += parsed.OutputTokens
			if parsed.IsError {
				stats.ErrorCount++
			}
			phaseTools[parsed.ToolName] = true
		}

		// Track token usage by phase
		if parsed.InputTokens > 0 || parsed.OutputTokens > 0 {
			usage := report.TokenBreakdown.ByPhase[currentPhase]
			usage.InputTokens += parsed.InputTokens
			usage.OutputTokens += parsed.OutputTokens
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
			report.TokenBreakdown.ByPhase[currentPhase] = usage
		}

		// Detect phase changes from text
		if parsed.Text != "" {
			newPhase := detectPhaseFromText(parsed.Text)
			if newPhase != "" && newPhase != currentPhase {
				// Record previous phase
				if phaseEvents > 0 {
					tools := make([]string, 0, len(phaseTools))
					for t := range phaseTools {
						tools = append(tools, t)
					}
					duration := event.Timestamp.Sub(phaseStartTime)
					report.PhaseAnalysis = append(report.PhaseAnalysis, PhaseAnalysis{
						Phase:      currentPhase,
						Duration:   duration,
						Percentage: float64(duration) / float64(a.recording.Duration) * 100,
						EventCount: phaseEvents,
						ToolsUsed:  tools,
					})
				}

				// Start new phase
				currentPhase = newPhase
				phaseStartTime = event.Timestamp
				phaseEvents = 0
				phaseTools = make(map[string]bool)
			}

			// Detect decision points (Navigator patterns)
			if strings.Contains(parsed.Text, "WORKFLOW CHECK") ||
				strings.Contains(parsed.Text, "Decision:") ||
				strings.Contains(parsed.Text, "Approach:") {
				report.DecisionPoints = append(report.DecisionPoints, DecisionPoint{
					Timestamp:   event.Timestamp,
					Sequence:    event.Sequence,
					Description: truncate(parsed.Text, 100),
				})
			}
		}

		phaseEvents++
	}

	// Record final phase
	if phaseEvents > 0 {
		tools := make([]string, 0, len(phaseTools))
		for t := range phaseTools {
			tools = append(tools, t)
		}
		duration := a.recording.EndTime.Sub(phaseStartTime)
		report.PhaseAnalysis = append(report.PhaseAnalysis, PhaseAnalysis{
			Phase:      currentPhase,
			Duration:   duration,
			Percentage: float64(duration) / float64(a.recording.Duration) * 100,
			EventCount: phaseEvents,
			ToolsUsed:  tools,
		})
	}

	// Convert tool stats to slice
	for _, stats := range toolStats {
		report.ToolUsage = append(report.ToolUsage, *stats)
	}

	// Token breakdown by tool
	for _, stats := range toolStats {
		report.TokenBreakdown.ByTool[stats.Tool] = TokenUsage{
			InputTokens:  stats.InputTokens,
			OutputTokens: stats.OutputTokens,
			TotalTokens:  stats.InputTokens + stats.OutputTokens,
		}
	}

	return report, nil
}

// detectPhaseFromText extracts phase from Navigator text patterns
func detectPhaseFromText(text string) string {
	textLower := strings.ToLower(text)

	if strings.Contains(textLower, "phase:") {
		if strings.Contains(textLower, "research") {
			return "Research"
		}
		if strings.Contains(textLower, "impl") {
			return "Implementing"
		}
		if strings.Contains(textLower, "verify") {
			return "Verifying"
		}
		if strings.Contains(textLower, "complete") {
			return "Completing"
		}
		if strings.Contains(textLower, "init") {
			return "Init"
		}
	}

	if strings.Contains(text, "LOOP MODE ACTIVATED") ||
		strings.Contains(text, "TASK MODE ACTIVATED") {
		return "Init"
	}

	return ""
}

// FormatReport formats an analysis report for terminal display
func FormatReport(report *AnalysisReport) string {
	var sb strings.Builder

	rec := report.Recording

	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString("EXECUTION ANALYSIS REPORT\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	// Overview
	sb.WriteString(fmt.Sprintf("Recording:  %s\n", rec.ID))
	sb.WriteString(fmt.Sprintf("Task:       %s\n", rec.TaskID))
	sb.WriteString(fmt.Sprintf("Status:     %s\n", rec.Status))
	sb.WriteString(fmt.Sprintf("Duration:   %s\n", rec.Duration.Round(time.Second)))
	sb.WriteString(fmt.Sprintf("Events:     %d\n", rec.EventCount))
	sb.WriteString("\n")

	// Token Usage
	if rec.TokenUsage != nil {
		sb.WriteString("TOKEN USAGE\n")
		sb.WriteString("───────────────────────────────────────\n")
		sb.WriteString(fmt.Sprintf("  Input:    %d tokens\n", rec.TokenUsage.InputTokens))
		sb.WriteString(fmt.Sprintf("  Output:   %d tokens\n", rec.TokenUsage.OutputTokens))
		sb.WriteString(fmt.Sprintf("  Total:    %d tokens\n", rec.TokenUsage.TotalTokens))
		sb.WriteString(fmt.Sprintf("  Cost:     $%.4f\n", rec.TokenUsage.EstimatedCostUSD))
		sb.WriteString("\n")
	}

	// Phase Analysis
	if len(report.PhaseAnalysis) > 0 {
		sb.WriteString("PHASE ANALYSIS\n")
		sb.WriteString("───────────────────────────────────────\n")
		for _, phase := range report.PhaseAnalysis {
			sb.WriteString(fmt.Sprintf("  %-12s %8s (%5.1f%%) %d events\n",
				phase.Phase+":",
				phase.Duration.Round(time.Second),
				phase.Percentage,
				phase.EventCount,
			))
		}
		sb.WriteString("\n")
	}

	// Tool Usage
	if len(report.ToolUsage) > 0 {
		sb.WriteString("TOOL USAGE\n")
		sb.WriteString("───────────────────────────────────────\n")
		for _, tool := range report.ToolUsage {
			errStr := ""
			if tool.ErrorCount > 0 {
				errStr = fmt.Sprintf(" (%d errors)", tool.ErrorCount)
			}
			sb.WriteString(fmt.Sprintf("  %-12s %4d calls%s\n", tool.Tool+":", tool.Count, errStr))
		}
		sb.WriteString("\n")
	}

	// Errors
	if len(report.Errors) > 0 {
		sb.WriteString("ERRORS\n")
		sb.WriteString("───────────────────────────────────────\n")
		for _, err := range report.Errors {
			sb.WriteString(fmt.Sprintf("  #%d [%s] %s: %s\n",
				err.Sequence,
				err.Timestamp.Format("15:04:05"),
				err.Tool,
				err.Message,
			))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	return sb.String()
}
