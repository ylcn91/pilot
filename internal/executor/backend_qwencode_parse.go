package executor

import (
	"encoding/json"
	"fmt"
)

// parseStreamEvent converts Qwen Code stream-json to BackendEvent.
// Qwen Code's stream-json format is nearly identical to Claude Code's:
// same top-level types (system, assistant, user, result), same content block
// structure, same usage fields. Key differences:
// 1. Tool names are snake_case (read_file vs Read) — normalized via qwenToolNameMap
// 2. User messages contain tool_result blocks in message.content[] instead of flat tool_use_result
func (b *QwenCodeBackend) parseStreamEvent(line string) BackendEvent {
	event := BackendEvent{
		Raw: line,
	}

	var streamEvent StreamEvent
	if err := json.Unmarshal([]byte(line), &streamEvent); err != nil {
		event.Type = EventTypeText
		event.Message = line
		return event
	}

	switch streamEvent.Type {
	case "system":
		if streamEvent.Subtype == "init" {
			event.Type = EventTypeInit
			event.SessionID = streamEvent.SessionID
			event.Message = "Qwen Code initialized"
		}

	case "assistant":
		if streamEvent.Message != nil {
			for _, block := range streamEvent.Message.Content {
				switch block.Type {
				case "tool_use":
					event.Type = EventTypeToolUse
					// Normalize snake_case tool names to PascalCase for Runner compatibility
					event.ToolName = normalizeQwenToolName(block.Name)
					event.ToolInput = block.Input
					event.Message = fmt.Sprintf("Using %s", event.ToolName)
				case "text":
					event.Type = EventTypeText
					event.Message = block.Text
				}
			}
		}

	case "user":
		// Qwen Code sends tool results in message.content[] as tool_result blocks.
		// The result text may be in "content" or "text" field depending on version.
		if streamEvent.Message != nil {
			for _, block := range streamEvent.Message.Content {
				if block.Type == "tool_result" {
					event.Type = EventTypeToolResult
					// Prefer "content" field (Qwen standard), fall back to "text"
					if block.Content != "" {
						event.ToolResult = block.Content
					} else {
						event.ToolResult = block.Text
					}
					event.IsError = block.IsError
				}
			}
		}
		// Also handle Claude-style flat tool_use_result for forward compatibility
		if event.Type == "" && streamEvent.ToolUseResult != nil {
			event.Type = EventTypeToolResult
			var toolResult ToolResultContent
			if err := json.Unmarshal(streamEvent.ToolUseResult, &toolResult); err == nil {
				event.ToolResult = toolResult.Content
				event.IsError = toolResult.IsError
			}
		}

	case "result":
		event.Type = EventTypeResult
		event.Message = streamEvent.Result
		event.IsError = streamEvent.IsError
	}

	// Capture usage info
	if streamEvent.Usage != nil {
		event.TokensInput = streamEvent.Usage.InputTokens
		event.TokensOutput = streamEvent.Usage.OutputTokens
	}
	if streamEvent.Model != "" {
		event.Model = streamEvent.Model
	}

	return event
}
