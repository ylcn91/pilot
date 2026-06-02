package approval

import (
	"fmt"
	"strconv"
	"time"
)

// formatApprovalMessage formats the approval request message
func (h *TelegramHandler) formatApprovalMessage(req *Request) string {
	var icon, stageLabel string

	switch req.Stage {
	case StagePreExecution:
		icon = "🚀"
		stageLabel = "Pre-Execution Approval"
	case StagePreMerge:
		icon = "🔀"
		stageLabel = "Pre-Merge Approval"
	case StagePostFailure:
		icon = "❌"
		stageLabel = "Post-Failure Decision"
	default:
		icon = "⚠️"
		stageLabel = "Approval Required"
	}

	text := fmt.Sprintf("%s %s\n\nTask: %s\n%s", icon, stageLabel, req.TaskID, req.Title)

	if req.Description != "" {
		text += fmt.Sprintf("\n\n%s", truncateForTelegram(req.Description, 500))
	}

	// Add metadata
	if prURL, ok := req.Metadata["pr_url"].(string); ok && prURL != "" {
		text += fmt.Sprintf("\n\nPR: %s", prURL)
	}
	if errorMsg, ok := req.Metadata["error"].(string); ok && errorMsg != "" {
		text += fmt.Sprintf("\n\nError: %s", truncateForTelegram(errorMsg, 200))
	}

	// Add timeout info
	timeLeft := time.Until(req.ExpiresAt).Round(time.Minute)
	text += fmt.Sprintf("\n\nExpires in: %s", formatDuration(timeLeft))

	return text
}

// createApprovalKeyboard creates inline keyboard buttons
func (h *TelegramHandler) createApprovalKeyboard(req *Request) [][]InlineKeyboardButton {
	var approveText, rejectText string

	switch req.Stage {
	case StagePreExecution:
		approveText = "✅ Execute"
		rejectText = "❌ Cancel"
	case StagePreMerge:
		approveText = "✅ Merge"
		rejectText = "❌ Reject"
	case StagePostFailure:
		approveText = "🔄 Retry"
		rejectText = "⏹ Abort"
	default:
		approveText = "✅ Approve"
		rejectText = "❌ Reject"
	}

	return [][]InlineKeyboardButton{
		{
			{Text: approveText, CallbackData: "approve:" + req.ID},
			{Text: rejectText, CallbackData: "reject:" + req.ID},
		},
	}
}

// formatResponseMessage formats the message after a response
func (h *TelegramHandler) formatResponseMessage(req *Request, decision Decision, username string) string {
	var icon, status string

	switch decision {
	case DecisionApproved:
		icon = "✅"
		status = "APPROVED"
	case DecisionRejected:
		icon = "❌"
		status = "REJECTED"
	default:
		icon = "⏱"
		status = "TIMEOUT"
	}

	text := fmt.Sprintf("%s %s\n\nTask: %s\n%s\n\nDecision: %s", icon, status, req.TaskID, req.Title, username)

	return text
}

// formatCancelledMessage formats the message when request is cancelled
func (h *TelegramHandler) formatCancelledMessage(req *Request) string {
	return fmt.Sprintf("⏹ CANCELLED\n\nTask: %s\n%s\n\nApproval request was cancelled.", req.TaskID, req.Title)
}

// truncateForTelegram truncates text to fit Telegram message limits
func truncateForTelegram(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen-3] + "..."
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < 0 {
		return "expired"
	}
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	hours := int(d.Hours())
	if hours == 1 {
		return "1 hour"
	}
	return strconv.Itoa(hours) + " hours"
}
