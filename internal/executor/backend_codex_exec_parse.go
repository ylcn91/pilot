package executor

import (
	"encoding/json"
)

type codexExecEnvelope struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Item     *codexExecItem  `json:"item"`
	Usage    *codexExecUsage `json:"usage"`
	Message  string          `json:"message"`
	Error    string          `json:"error"`
	Msg      json.RawMessage `json:"msg"`
}

type codexExecItem struct {
	ID               string          `json:"id"`
	Type             string          `json:"type"`
	Text             string          `json:"text"`
	Command          string          `json:"command"`
	AggregatedOutput string          `json:"aggregated_output"`
	ExitCode         *int            `json:"exit_code"`
	Status           string          `json:"status"`
	SummaryText      string          `json:"summary_text"`
	RawContent       string          `json:"raw_content"`
	Changes          json.RawMessage `json:"changes"`
}

type codexExecUsage struct {
	InputTokens           int64  `json:"input_tokens"`
	CachedInputTokens     int64  `json:"cached_input_tokens"`
	OutputTokens          int64  `json:"output_tokens"`
	ReasoningOutputTokens int64  `json:"reasoning_output_tokens"`
	TotalTokens           int64  `json:"total_tokens"`
	Model                 string `json:"model"`
}

func (b *CodexExecBackend) parseStreamEvent(line string) BackendEvent {
	event := BackendEvent{Raw: line}

	var envelope codexExecEnvelope
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		event.Type = EventTypeText
		event.Message = line
		return event
	}
	if envelope.Type == "" && len(envelope.Msg) > 0 {
		if err := json.Unmarshal(envelope.Msg, &envelope); err != nil {
			event.Type = EventTypeText
			event.Message = line
			return event
		}
	}

	switch envelope.Type {
	case "thread.started":
		event.Type = EventTypeInit
		event.SessionID = envelope.ThreadID
		event.Message = "Codex exec initialized"
	case "turn.started", "task_started":
		event.Type = EventTypeProgress
		event.Message = "Codex turn started"
	case "turn.completed", "task_complete":
		event.Type = EventTypeResult
		event.Message = envelope.Message
	case "stream_error":
		event.Type = EventTypeError
		event.IsError = true
		event.Message = firstNonEmpty(envelope.Error, envelope.Message)
	case "item.started", "item.completed":
		event = b.parseItemEvent(event, envelope.Type, envelope.Item)
	default:
		if envelope.Message != "" {
			event.Type = EventTypeText
			event.Message = envelope.Message
		} else {
			event.Type = EventTypeProgress
			event.Message = envelope.Type
		}
	}

	if envelope.Usage != nil {
		event.TokensInput = envelope.Usage.InputTokens
		event.CacheReadInputTokens = envelope.Usage.CachedInputTokens
		event.TokensOutput = envelope.Usage.OutputTokens
		event.Model = envelope.Usage.Model
	}

	return event
}

func (b *CodexExecBackend) parseItemEvent(event BackendEvent, eventType string, item *codexExecItem) BackendEvent {
	if item == nil {
		event.Type = EventTypeProgress
		event.Message = eventType
		return event
	}

	switch item.Type {
	case "agent_message":
		event.Type = EventTypeText
		event.Message = item.Text
	case "reasoning":
		event.Type = EventTypeProgress
		event.Message = firstNonEmpty(item.SummaryText, item.RawContent, "Codex reasoning")
	case "command_execution":
		if eventType == "item.started" || item.Status == "in_progress" {
			event.Type = EventTypeToolUse
			event.ToolName = "Bash"
			event.ToolInput = map[string]interface{}{"command": item.Command}
			event.Message = "Using Bash"
		} else {
			event.Type = EventTypeToolResult
			event.ToolName = "Bash"
			event.ToolResult = item.AggregatedOutput
			event.Message = item.AggregatedOutput
			event.IsError = item.ExitCode != nil && *item.ExitCode != 0
		}
	case "file_change":
		event.Type = EventTypeToolUse
		event.ToolName = "Edit"
		event.ToolInput = map[string]interface{}{"changes": string(item.Changes)}
		event.Message = "Applying file changes"
	default:
		event.Type = EventTypeProgress
		event.Message = item.Type
	}

	return event
}
