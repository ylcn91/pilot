package executor

import (
	"fmt"
	"log/slog"
)

// processBackendEvent handles events from any backend and updates progress state.
// This is the unified event handler that works with both Claude Code and OpenCode.
func (r *Runner) processBackendEvent(taskID string, event BackendEvent, state *progressState) {
	// Track token usage
	state.tokensInput += event.TokensInput
	state.tokensOutput += event.TokensOutput
	state.cacheCreationInputTokens += event.CacheCreationInputTokens
	state.cacheReadInputTokens += event.CacheReadInputTokens
	if event.Model != "" {
		state.modelName = event.Model
	}

	// Report token usage to callbacks (e.g., dashboard)
	if event.TokensInput > 0 || event.TokensOutput > 0 {
		r.reportTokens(taskID, state.tokensInput, state.tokensOutput, state.modelName)
	}

	// GH-539: Check per-task token/duration limit on each event
	if r.tokenLimitCheck != nil && !state.budgetExceeded {
		if !r.tokenLimitCheck(taskID, event.TokensInput, event.TokensOutput) {
			state.budgetExceeded = true
			state.budgetReason = fmt.Sprintf("per-task limit exceeded at %d input + %d output tokens",
				state.tokensInput, state.tokensOutput)
			r.log.Warn("Per-task budget limit exceeded, cancelling execution",
				slog.String("task_id", taskID),
				slog.Int64("input_tokens", state.tokensInput),
				slog.Int64("output_tokens", state.tokensOutput),
			)
			if state.budgetCancel != nil {
				state.budgetCancel()
			}
			return // Skip further event processing
		}
	}

	switch event.Type {
	case EventTypeInit:
		// GH-1265: Capture session ID for resume in self-review
		if event.SessionID != "" {
			state.sessionID = event.SessionID
		}
		r.reportProgress(taskID, "🚀 Started", 5, event.Message)

	case EventTypeText:
		// Parse Navigator-specific patterns from text
		if event.Message != "" {
			r.parseNavigatorPatterns(taskID, event.Message, state)
		}

	case EventTypeToolUse:
		r.handleToolUse(taskID, event.ToolName, event.ToolInput, state)

	case EventTypeToolResult:
		// Extract commit SHA from tool output
		if event.ToolResult != "" {
			extractCommitSHA(event.ToolResult, state)
		}

	case EventTypeResult:
		r.log.Debug("Backend result received",
			slog.String("task_id", taskID),
			slog.Bool("is_error", event.IsError),
		)

	case EventTypeError:
		r.log.Warn("Backend error", slog.String("task_id", taskID), slog.String("error", event.Message))

	case EventTypeProgress:
		// Progress events may contain phase information
		if event.Phase != "" {
			r.handleNavigatorPhase(taskID, event.Phase, state)
		}
	}
}
