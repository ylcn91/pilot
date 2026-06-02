package executor

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// parseStreamEvent parses a stream-json event and reports progress
// Returns (finalResult, errorMessage) - non-empty when task completes
func (r *Runner) parseStreamEvent(taskID, line string, state *progressState) (string, string) {
	var event StreamEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		// Not valid JSON, skip
		return "", ""
	}

	switch event.Type {
	case "system":
		if event.Subtype == "init" {
			r.reportProgress(taskID, "🚀 Started", 5, "Claude Code initialized")
		}

	case "assistant":
		if event.Message != nil {
			for _, block := range event.Message.Content {
				switch block.Type {
				case "tool_use":
					r.handleToolUse(taskID, block.Name, block.Input, state)
				case "text":
					// Parse Navigator-specific patterns from text
					r.parseNavigatorPatterns(taskID, block.Text, state)
				}
			}
		}

	case "user":
		// Tool results - parse for commit SHAs
		if event.ToolUseResult != nil {
			var toolResult ToolResultContent
			if err := json.Unmarshal(event.ToolUseResult, &toolResult); err == nil {
				// Extract commit SHA from git commit output
				// Pattern: "[branch abc1234] commit message" or "[main abc1234] message"
				extractCommitSHA(toolResult.Content, state)
			}
		}

	case "result":
		// Capture final usage stats from result event
		if event.Usage != nil {
			state.tokensInput += event.Usage.InputTokens
			state.tokensOutput += event.Usage.OutputTokens
			state.cacheCreationInputTokens += event.Usage.CacheCreationInputTokens
			state.cacheReadInputTokens += event.Usage.CacheReadInputTokens
		}
		if event.Model != "" {
			state.modelName = event.Model
		}
		r.log.Debug("Stream result received",
			slog.String("task_id", taskID),
			slog.Bool("is_error", event.IsError),
			slog.String("model", event.Model),
		)
		if event.IsError {
			r.log.Warn("Claude Code returned error", slog.String("task_id", taskID), slog.String("error", event.Result))
			return "", event.Result
		}
		return event.Result, ""
	}

	// Capture model name before reporting tokens so the callback receives the correct model.
	if event.Model != "" && state.modelName == "" {
		state.modelName = event.Model
	}
	// Track usage from any event with usage info
	if event.Usage != nil {
		state.tokensInput += event.Usage.InputTokens
		state.tokensOutput += event.Usage.OutputTokens
		state.cacheCreationInputTokens += event.Usage.CacheCreationInputTokens
		state.cacheReadInputTokens += event.Usage.CacheReadInputTokens
		// Report token usage to callbacks (e.g., dashboard)
		r.reportTokens(taskID, state.tokensInput, state.tokensOutput, state.modelName)
	}

	return "", ""
}

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
