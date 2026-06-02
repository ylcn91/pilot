package telegram

import (
	"fmt"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/executor"
)

// FormatTaskConfirmation formats a task confirmation message
func FormatTaskConfirmation(taskID, description, projectPath string) string {
	return fmt.Sprintf(
		"📋 Confirm Task\n\n"+
			"%s\n\n"+
			"Task: %s\n"+
			"Project: %s\n\n"+
			"Execute this task?",
		taskID,
		truncateDescription(description, 200),
		projectPath,
	)
}

// FormatTaskStarted formats a task started message
func FormatTaskStarted(taskID, description string) string {
	return fmt.Sprintf(
		"🚀 Executing\n%s\n\n%s",
		taskID,
		truncateDescription(description, 150),
	)
}

// FormatProgressUpdate formats a progress update message
func FormatProgressUpdate(taskID, phase string, progress int, message string) string {
	// Build progress bar (20 chars)
	filled := progress / 5 // 0-20 filled chars
	if filled > 20 {
		filled = 20
	}
	if filled < 0 {
		filled = 0
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", 20-filled)

	// Phase emoji
	phaseEmoji := "⏳"
	switch phase {
	case "Starting":
		phaseEmoji = "🚀"
	case "Branching":
		phaseEmoji = "🌿"
	case "Exploring":
		phaseEmoji = "🔍"
	case "Installing":
		phaseEmoji = "📦"
	case "Implementing":
		phaseEmoji = "⚙️"
	case "Testing":
		phaseEmoji = "🧪"
	case "Committing":
		phaseEmoji = "💾"
	case "Completed":
		phaseEmoji = "✅"
	case "Navigator":
		phaseEmoji = "🧭"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s %s (%d%%)\n", phaseEmoji, phase, progress))
	sb.WriteString(fmt.Sprintf("%s\n\n", bar))
	sb.WriteString(taskID)

	// Add activity message if present
	if message != "" {
		cleanMsg := truncateDescription(message, 60)
		sb.WriteString(fmt.Sprintf("\n\n📝 %s", cleanMsg))
	}

	return sb.String()
}

// FormatTaskResult formats a task result message with clean output
func FormatTaskResult(result *executor.ExecutionResult) string {
	if result.Success {
		return formatSuccessResult(result)
	}
	return formatFailureResult(result)
}

// formatSuccessResult formats a successful task result
func formatSuccessResult(result *executor.ExecutionResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("✅ Task completed\n%s\n\n", result.TaskID))
	sb.WriteString(fmt.Sprintf("⏱ Duration: %s\n", result.Duration.Round(time.Second)))

	// Add commit SHA if present
	if result.CommitSHA != "" {
		sb.WriteString(fmt.Sprintf("📝 Commit: %s\n", result.CommitSHA[:min(8, len(result.CommitSHA))]))
	}

	// Add quality gates summary (GH-209)
	if result.QualityGates != nil && result.QualityGates.Enabled {
		sb.WriteString(formatQualityGatesSummary(result.QualityGates))
	}

	// Add PR URL if present
	if result.PRUrl != "" {
		sb.WriteString(fmt.Sprintf("\n🔗 PR: %s\n", result.PRUrl))
	}

	// Clean and add output summary
	cleanOutput := cleanInternalSignals(result.Output)
	if cleanOutput != "" {
		// Extract key information from output
		summary := extractSummary(cleanOutput)
		if summary != "" {
			sb.WriteString(fmt.Sprintf("\n📄 Summary:\n%s", summary))
		}
	}

	return sb.String()
}

// formatQualityGatesSummary formats quality gate results for Telegram (GH-209)
func formatQualityGatesSummary(qg *executor.QualityGatesResult) string {
	if qg == nil || len(qg.Gates) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n🔒 Quality Gates: ")

	// Count passed gates
	passed := 0
	for _, g := range qg.Gates {
		if g.Passed {
			passed++
		}
	}
	sb.WriteString(fmt.Sprintf("%d/%d passed\n", passed, len(qg.Gates)))

	// List individual gates
	for _, gate := range qg.Gates {
		var icon string
		if gate.Passed {
			icon = "✅"
		} else {
			icon = "❌"
		}

		durationStr := gate.Duration.Round(time.Second).String()
		sb.WriteString(fmt.Sprintf("- %s %s (%s", gate.Name, icon, durationStr))

		// Add retry count if any
		if gate.RetryCount > 0 {
			sb.WriteString(fmt.Sprintf(", %d retry", gate.RetryCount))
		}
		sb.WriteString(")\n")
	}

	return sb.String()
}

// formatFailureResult formats a failed task result
func formatFailureResult(result *executor.ExecutionResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("❌ Task failed\n%s\n\n", result.TaskID))
	sb.WriteString(fmt.Sprintf("⏱ Duration: %s\n", result.Duration.Round(time.Second)))

	// Add quality gates summary if available (GH-209)
	if result.QualityGates != nil && result.QualityGates.Enabled {
		sb.WriteString(formatQualityGatesSummary(result.QualityGates))
	}

	cleanError := cleanInternalSignals(result.Error)
	if cleanError == "" {
		cleanError = "Unknown error"
	}

	// Truncate error for Telegram
	if len(cleanError) > 400 {
		cleanError = cleanError[:400] + "..."
	}

	sb.WriteString(fmt.Sprintf("\n%s", cleanError))
	return sb.String()
}

// FormatGreeting formats a greeting response
func FormatGreeting(username string) string {
	name := "there"
	if username != "" {
		name = username
	}
	return fmt.Sprintf(
		"👋 Hey %s! I'm Pilot.\n\n"+
			"I can help you in different ways:\n\n"+
			"💬 *Chat* - Ask opinions or discuss\n"+
			"  \"What do you think about using Redis?\"\n\n"+
			"🔍 *Questions* - Quick answers\n"+
			"  \"What files handle auth?\"\n\n"+
			"🔬 *Research* - Deep analysis\n"+
			"  \"Research how caching works here\"\n\n"+
			"📐 *Planning* - Design before building\n"+
			"  \"Plan how to add rate limiting\"\n\n"+
			"🚀 *Tasks* - Build features\n"+
			"  \"Add a logout button\"\n\n"+
			"Type /help for commands.",
		name,
	)
}
