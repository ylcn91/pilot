package executor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// parseAssistantResponse decodes the synchronous response from
// POST /session/:id/message and populates result. The body shape comes from
// OpenCode v1.4.6 (packages/opencode/src/server/instance/session.ts) and
// looks like: {info: AssistantMessage, parts: Part[]}.
//
// Text parts are concatenated into result.Output. Non-text parts (tool, file,
// agent, etc.) are surfaced through opts.EventHandler so the runner sees the
// same event stream it would for a streaming backend. Token usage and model
// metadata come from info.
func (b *OpenCodeBackend) parseAssistantResponse(body io.Reader, opts ExecuteOptions, result *BackendResult) error {
	var resp ocAssistantResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return fmt.Errorf("decode opencode response: %w", err)
	}

	// Model metadata: prefer providerID/modelID; fall back to model field.
	if resp.Info.ModelID != "" {
		if resp.Info.ProviderID != "" {
			result.Model = resp.Info.ProviderID + "/" + resp.Info.ModelID
		} else {
			result.Model = resp.Info.ModelID
		}
	} else if resp.Info.Model != "" {
		result.Model = resp.Info.Model
	}

	// Token usage. v1.4.x exposes tokens.{input,output,reasoning,cache.{read,write}}.
	result.TokensInput += resp.Info.Tokens.Input
	result.TokensOutput += resp.Info.Tokens.Output
	result.CacheReadInputTokens += resp.Info.Tokens.Cache.Read
	result.CacheCreationInputTokens += resp.Info.Tokens.Cache.Write

	if resp.Info.SessionID != "" {
		result.SessionID = resp.Info.SessionID
	}

	// If the assistant message carries an error indicator, propagate it.
	if resp.Info.Error.Message != "" {
		result.Error = resp.Info.Error.Message
	} else if resp.Info.Error.Name != "" {
		result.Error = resp.Info.Error.Name
	}

	var output strings.Builder
	for _, part := range resp.Parts {
		switch part.Type {
		case "text":
			output.WriteString(part.Text)
			if opts.EventHandler != nil {
				opts.EventHandler(BackendEvent{
					Type:    EventTypeText,
					Message: part.Text,
					Raw:     part.Text,
				})
			}
		case "tool":
			if opts.EventHandler != nil {
				opts.EventHandler(BackendEvent{
					Type:      EventTypeToolUse,
					ToolName:  part.Tool,
					ToolInput: part.State.Input,
					Message:   fmt.Sprintf("Using %s", part.Tool),
				})
				if part.State.Status == "completed" || part.State.Status == "error" {
					ev := BackendEvent{
						Type:       EventTypeToolResult,
						ToolName:   part.Tool,
						ToolResult: part.State.Output,
						IsError:    part.State.Status == "error",
					}
					opts.EventHandler(ev)
				}
			}
		case "step-start", "step-finish", "reasoning", "file", "agent", "snapshot":
			if opts.EventHandler != nil {
				opts.EventHandler(BackendEvent{
					Type:    EventTypeProgress,
					Message: part.Type,
				})
			}
		}
	}

	result.Output = output.String()

	// Synthesize a final result event so downstream consumers (alerts, logging)
	// see a terminal event, mirroring the SSE path.
	if opts.EventHandler != nil {
		opts.EventHandler(BackendEvent{
			Type:         EventTypeResult,
			Message:      result.Output,
			IsError:      result.Error != "",
			TokensInput:  resp.Info.Tokens.Input,
			TokensOutput: resp.Info.Tokens.Output,
			Model:        result.Model,
		})
	}

	return nil
}

// parseSSEStream parses Server-Sent Events from OpenCode.
func (b *OpenCodeBackend) parseSSEStream(reader io.Reader, opts ExecuteOptions, result *BackendResult) error {
	scanner := bufio.NewScanner(reader)
	// Increase buffer for large events
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var eventData strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if opts.Verbose {
			fmt.Printf("   [sse] %s\n", line)
		}

		// SSE format: "data: {...}"
		if strings.HasPrefix(line, "data: ") {
			eventData.WriteString(strings.TrimPrefix(line, "data: "))
			continue
		}

		// Empty line marks end of event
		if line == "" && eventData.Len() > 0 {
			event := b.parseOpenCodeEvent(eventData.String())
			if opts.EventHandler != nil {
				opts.EventHandler(event)
			}

			// Track final result
			if event.Type == EventTypeResult {
				if event.IsError {
					result.Error = event.Message
				} else {
					result.Output = event.Message
				}
			}

			// Accumulate token usage. GH-2428: also accumulate cache tokens so
			// the SSE path matches the synchronous parseAssistantResponse path.
			result.TokensInput += event.TokensInput
			result.TokensOutput += event.TokensOutput
			result.CacheCreationInputTokens += event.CacheCreationInputTokens
			result.CacheReadInputTokens += event.CacheReadInputTokens
			if event.Model != "" {
				result.Model = event.Model
			}

			eventData.Reset()
		}
	}

	return scanner.Err()
}

// parseOpenCodeEvent converts OpenCode SSE event to BackendEvent.
func (b *OpenCodeBackend) parseOpenCodeEvent(data string) BackendEvent {
	event := BackendEvent{
		Raw: data,
	}

	var ocEvent openCodeEvent
	if err := json.Unmarshal([]byte(data), &ocEvent); err != nil {
		event.Type = EventTypeText
		event.Message = data
		return event
	}

	// Map OpenCode event types to backend events
	switch ocEvent.Type {
	case "message.start", "session.start":
		event.Type = EventTypeInit
		event.Message = "OpenCode session started"

	case "message.delta", "content.delta":
		event.Type = EventTypeText
		if ocEvent.Delta != nil {
			event.Message = ocEvent.Delta.Text
		}

	case "tool.start", "tool_use":
		event.Type = EventTypeToolUse
		event.ToolName = ocEvent.Tool
		if ocEvent.Input != nil {
			event.ToolInput = ocEvent.Input
		}
		event.Message = fmt.Sprintf("Using %s", ocEvent.Tool)

	case "tool.end", "tool_result":
		event.Type = EventTypeToolResult
		event.ToolResult = ocEvent.Output
		event.IsError = ocEvent.IsError

	case "message.end", "done":
		event.Type = EventTypeResult
		event.Message = ocEvent.Output
		event.IsError = ocEvent.IsError

	case "error":
		event.Type = EventTypeError
		event.Message = ocEvent.Error
		event.IsError = true

	case "usage":
		event.Type = EventTypeProgress
		if ocEvent.Usage != nil {
			event.TokensInput = ocEvent.Usage.InputTokens
			event.TokensOutput = ocEvent.Usage.OutputTokens
			event.CacheCreationInputTokens = ocEvent.Usage.cacheCreate()
			event.CacheReadInputTokens = ocEvent.Usage.cacheRead()
		}

	default:
		// Unknown event type, treat as progress
		event.Type = EventTypeProgress
		event.Message = data
	}

	// Extract usage if present (covers events where the usage block lives at
	// the top level rather than under a dedicated "usage" event type).
	if ocEvent.Usage != nil {
		event.TokensInput = ocEvent.Usage.InputTokens
		event.TokensOutput = ocEvent.Usage.OutputTokens
		event.CacheCreationInputTokens = ocEvent.Usage.cacheCreate()
		event.CacheReadInputTokens = ocEvent.Usage.cacheRead()
	}
	if ocEvent.Model != "" {
		event.Model = ocEvent.Model
	}

	return event
}
