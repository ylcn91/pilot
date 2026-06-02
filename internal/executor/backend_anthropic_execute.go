package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// --- Main Execute Loop ---

func (b *AnthropicBackend) Execute(ctx context.Context, opts ExecuteOptions) (*BackendResult, error) {
	if b.apiKey == "" {
		return nil, fmt.Errorf("no API key configured for anthropic-api backend")
	}

	model := opts.Model
	if model == "" {
		model = b.config.ResolveModel("claude-opus-4-6")
	}

	cwd := opts.ProjectPath
	if cwd == "" {
		cwd = "/app"
	}

	// Emit init event
	if opts.EventHandler != nil {
		opts.EventHandler(BackendEvent{
			Type:    EventTypeInit,
			Message: "Anthropic API backend initialized",
			Model:   model,
		})
	}

	messages := []apiMessage{
		{Role: "user", Content: opts.Prompt},
	}

	var totalInputTokens, totalOutputTokens int64
	var lastOutput string
	var sawSuccess bool

	for turn := 0; turn < apiMaxTurns; turn++ {
		// Check context
		if ctx.Err() != nil {
			return &BackendResult{
				Success:          sawSuccess,
				Output:           lastOutput,
				Error:            "context cancelled",
				TokensInput:      totalInputTokens,
				TokensOutput:     totalOutputTokens,
				Model:            model,
				SawSuccessResult: sawSuccess,
			}, nil
		}

		// Progressive thinking budget
		thinkingBudget := thinkingLowBudget
		if turn < thinkingHighTurns {
			thinkingBudget = thinkingHighBudget
		}

		// Map effort to thinking budget override
		switch opts.Effort {
		case "low":
			thinkingBudget = 1000
		case "medium":
			thinkingBudget = 3000
		case "max":
			thinkingBudget = 15000
		}

		// Disable thinking for non-Opus models (Haiku/Sonnet don't benefit as much,
		// and it avoids compatibility issues with older model versions)
		useThinking := strings.Contains(model, "opus")

		req := &apiRequest{
			Model:     model,
			MaxTokens: maxOutputTokens,
			System:    opts.Prompt,
			Messages:  messages,
			Tools:     apiTools,
		}
		if useThinking {
			req.MaxTokens = thinkingBudget + maxOutputTokens
			req.Thinking = &apiThinking{Type: "enabled", BudgetTokens: thinkingBudget}
		}

		// First message has prompt as system, subsequent don't repeat it
		if turn == 0 {
			req.System = opts.Prompt
			req.Messages = []apiMessage{
				{Role: "user", Content: "Begin. Follow the mandatory workflow."},
			}
			messages = req.Messages
		} else {
			req.System = opts.Prompt
		}

		slog.Info("API call", slog.Int("turn", turn), slog.String("model", model),
			slog.Bool("thinking", useThinking), slog.Int("budget", thinkingBudget),
			slog.String("effort", opts.Effort))

		response, err := b.callAPI(ctx, req)
		if err != nil {
			slog.Error("API call failed", slog.Int("turn", turn), slog.Any("error", err))

			if opts.EventHandler != nil {
				opts.EventHandler(BackendEvent{
					Type:    EventTypeError,
					Message: err.Error(),
					IsError: true,
				})
			}

			return &BackendResult{
				Success:          sawSuccess,
				Output:           lastOutput,
				Error:            err.Error(),
				TokensInput:      totalInputTokens,
				TokensOutput:     totalOutputTokens,
				Model:            model,
				SawSuccessResult: sawSuccess,
			}, nil
		}

		totalInputTokens += response.Usage.InputTokens
		totalOutputTokens += response.Usage.OutputTokens

		// Process response content → extract tool calls and text
		var assistantBlocks []apiContentBlock
		var toolCalls []apiContentBlock

		for _, block := range response.Content {
			switch block.Type {
			case "text":
				lastOutput = block.Text
				if opts.EventHandler != nil {
					opts.EventHandler(BackendEvent{
						Type:         EventTypeText,
						Message:      block.Text,
						TokensInput:  response.Usage.InputTokens,
						TokensOutput: response.Usage.OutputTokens,
						Model:        model,
					})
				}
				assistantBlocks = append(assistantBlocks, block)

			case "tool_use":
				var toolInput map[string]interface{}
				if err := json.Unmarshal(block.Input, &toolInput); err != nil {
					toolInput = map[string]interface{}{"error": "failed to parse input"}
				}

				if opts.EventHandler != nil {
					opts.EventHandler(BackendEvent{
						Type:      EventTypeToolUse,
						ToolName:  block.Name,
						ToolInput: toolInput,
						Model:     model,
					})
				}

				toolCalls = append(toolCalls, block)
				assistantBlocks = append(assistantBlocks, block)

			case "thinking":
				// Skip thinking blocks in message history
				assistantBlocks = append(assistantBlocks, block)
			}
		}

		// Add assistant message to conversation
		messages = append(messages, apiMessage{Role: "assistant", Content: assistantBlocks})

		// Process tool calls
		if response.StopReason == "tool_use" && len(toolCalls) > 0 {
			var toolResults []apiContentBlock

			for _, tc := range toolCalls {
				var toolInput map[string]interface{}
				_ = json.Unmarshal(tc.Input, &toolInput)

				slog.Info("Tool exec", slog.Int("turn", turn), slog.String("tool", tc.Name))
				result := executeTool(tc.Name, toolInput, cwd)

				if opts.EventHandler != nil {
					opts.EventHandler(BackendEvent{
						Type:       EventTypeToolResult,
						ToolName:   tc.Name,
						ToolResult: result,
					})
				}

				toolResults = append(toolResults, apiContentBlock{
					Type:      "tool_result",
					ToolUseID: tc.ID,
					Content:   result,
				})
			}

			messages = append(messages, apiMessage{Role: "user", Content: toolResults})

		} else if response.StopReason == "end_turn" {
			// Model finished
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
		if estimateContextTokens(messages) > apiContextPruneAt {
			messages = pruneAPIMessages(messages)
			slog.Info("Context pruned", slog.Int("turn", turn))
		}
	}

	return &BackendResult{
		Success:          sawSuccess,
		Output:           lastOutput,
		TokensInput:      totalInputTokens,
		TokensOutput:     totalOutputTokens,
		Model:            model,
		SawSuccessResult: sawSuccess,
	}, nil
}

// --- Context Management ---

func estimateContextTokens(messages []apiMessage) int {
	total := 0
	for _, msg := range messages {
		switch c := msg.Content.(type) {
		case string:
			total += len(c) / 4
		case []apiContentBlock:
			for _, b := range c {
				total += len(b.Text) / 4
				total += len(b.Content) / 4
				total += len(b.Input) / 4
			}
		default:
			data, _ := json.Marshal(c)
			total += len(data) / 4
		}
	}
	return total
}

func pruneAPIMessages(messages []apiMessage) []apiMessage {
	keepTurns := 12
	if len(messages) <= keepTurns*2 {
		return messages
	}
	first := messages[0]
	keep := messages[len(messages)-keepTurns*2:]
	pruned := []apiMessage{
		first,
		{Role: "user", Content: "[Earlier messages pruned to save context. Continue working on the task.]"},
	}
	return append(pruned, keep...)
}
