package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// --- Main Execute Loop ---

func (b *OpenAIBackend) Execute(ctx context.Context, opts ExecuteOptions) (*BackendResult, error) {
	if b.apiKey == "" {
		return nil, fmt.Errorf("no API key configured for openai-api backend (set OPENAI_API_KEY or executor.openai.api_key)")
	}

	model := opts.Model
	if model == "" {
		model = b.model
	}

	cwd := opts.ProjectPath
	if cwd == "" {
		cwd = "/app"
	}

	if opts.EventHandler != nil {
		opts.EventHandler(BackendEvent{
			Type:    EventTypeInit,
			Message: "OpenAI-compatible API backend initialized",
			Model:   model,
		})
	}

	// Build conversation: system prompt + initial user kickoff
	content := opts.Prompt
	beginContent := "Begin. Follow the mandatory workflow."
	messages := []openaiMsg{
		{Role: "system", Content: &content},
		{Role: "user", Content: &beginContent},
	}

	var totalPromptTokens, totalCompletionTokens int64
	var lastOutput string
	var sawSuccess bool

	for turn := 0; turn < apiMaxTurns; turn++ {
		if ctx.Err() != nil {
			return &BackendResult{
				Success:           sawSuccess,
				Output:            lastOutput,
				LastAssistantText: lastOutput,
				Error:             "context cancelled",
				TokensInput:       totalPromptTokens,
				TokensOutput:      totalCompletionTokens,
				Model:             model,
				SawSuccessResult:  sawSuccess,
			}, nil
		}

		req := &openaiRequest{
			Model:    model,
			Messages: messages,
			Tools:    openaiTools,
		}

		slog.Info("OpenAI API call", slog.Int("turn", turn), slog.String("model", model),
			slog.Int("messages", len(messages)))

		accum, err := b.callAPI(ctx, req)
		if err != nil {
			slog.Error("OpenAI API call failed", slog.Int("turn", turn), slog.Any("error", err))

			if opts.EventHandler != nil {
				opts.EventHandler(BackendEvent{
					Type:    EventTypeError,
					Message: err.Error(),
					IsError: true,
				})
			}

			return &BackendResult{
				Success:           sawSuccess,
				Output:            lastOutput,
				LastAssistantText: lastOutput,
				Error:             err.Error(),
				TokensInput:       totalPromptTokens,
				TokensOutput:      totalCompletionTokens,
				Model:             model,
				SawSuccessResult:  sawSuccess,
			}, nil
		}

		totalPromptTokens += accum.PromptTokens
		totalCompletionTokens += accum.CompletionTokens

		// Emit text event
		if accum.Content != "" {
			lastOutput = accum.Content
			if opts.EventHandler != nil {
				opts.EventHandler(BackendEvent{
					Type:         EventTypeText,
					Message:      accum.Content,
					TokensInput:  accum.PromptTokens,
					TokensOutput: accum.CompletionTokens,
					Model:        model,
				})
			}
		}

		// Build assistant message for conversation history
		assistantMsg := openaiMsg{Role: "assistant"}
		if accum.Content != "" {
			c := accum.Content
			assistantMsg.Content = &c
		}
		// nil Content serialises as null — required for tool-call-only turns
		if len(accum.ToolCalls) > 0 {
			assistantMsg.ToolCalls = accum.ToolCalls
		}
		messages = append(messages, assistantMsg)

		// Process tool calls
		if accum.FinishReason == "tool_calls" && len(accum.ToolCalls) > 0 {
			for _, tc := range accum.ToolCalls {
				var toolInput map[string]interface{}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &toolInput); err != nil {
					toolInput = map[string]interface{}{"error": "failed to parse arguments: " + err.Error()}
				}

				if opts.EventHandler != nil {
					opts.EventHandler(BackendEvent{
						Type:      EventTypeToolUse,
						ToolName:  tc.Function.Name,
						ToolInput: toolInput,
						Model:     model,
					})
				}

				slog.Info("OpenAI tool exec", slog.Int("turn", turn), slog.String("tool", tc.Function.Name))
				toolResult := executeTool(tc.Function.Name, toolInput, cwd)

				if opts.EventHandler != nil {
					opts.EventHandler(BackendEvent{
						Type:       EventTypeToolResult,
						ToolName:   tc.Function.Name,
						ToolResult: toolResult,
					})
				}

				// OpenAI tool result message format
				toolResultContent := toolResult
				messages = append(messages, openaiMsg{
					Role:       "tool",
					Content:    &toolResultContent,
					ToolCallID: tc.ID,
				})
			}

		} else if accum.FinishReason == "stop" || accum.FinishReason == "" {
			// "stop" = normal completion; empty = some providers omit it
			sawSuccess = true
			if opts.EventHandler != nil {
				opts.EventHandler(BackendEvent{
					Type:    EventTypeResult,
					Message: lastOutput,
				})
			}
			break
		}

		// Context pruning: rough estimate 4 chars = 1 token
		if estimateOpenAIContextTokens(messages) > apiContextPruneAt {
			messages = pruneOpenAIMessages(messages)
			slog.Info("OpenAI context pruned", slog.Int("turn", turn))
		}
	}

	return &BackendResult{
		Success:           sawSuccess,
		Output:            lastOutput,
		LastAssistantText: lastOutput,
		TokensInput:       totalPromptTokens,
		TokensOutput:      totalCompletionTokens,
		Model:             model,
		SawSuccessResult:  sawSuccess,
	}, nil
}

// --- Context Management ---

func estimateOpenAIContextTokens(messages []openaiMsg) int {
	total := 0
	for _, msg := range messages {
		if msg.Content != nil {
			total += len(*msg.Content) / 4
		}
		for _, tc := range msg.ToolCalls {
			total += len(tc.Function.Arguments) / 4
		}
	}
	return total
}

func pruneOpenAIMessages(messages []openaiMsg) []openaiMsg {
	keepTurns := 12
	// messages[0] is always the system prompt; +1 accounts for it
	if len(messages) <= keepTurns*2+1 {
		return messages
	}
	system := messages[0]
	keep := messages[len(messages)-keepTurns*2:]
	notice := "[Earlier messages pruned to save context. Continue working on the task.]"
	pruned := []openaiMsg{
		system,
		{Role: "user", Content: &notice},
	}
	return append(pruned, keep...)
}
