package codexruntime

import (
	"encoding/json"
	"errors"
)

type EventType string

const (
	EventNotification       EventType = "notification"
	EventError              EventType = "error"
	EventThreadStatus       EventType = "thread_status"
	EventTurnStarted        EventType = "turn_started"
	EventTurnCompleted      EventType = "turn_completed"
	EventItemStarted        EventType = "item_started"
	EventItemCompleted      EventType = "item_completed"
	EventHookStarted        EventType = "hook_started"
	EventHookCompleted      EventType = "hook_completed"
	EventAgentMessageDelta  EventType = "agent_message_delta"
	EventReasoningDelta     EventType = "reasoning_delta"
	EventCommandOutputDelta EventType = "command_output_delta"
	EventFileOutputDelta    EventType = "file_output_delta"
	EventDiffUpdated        EventType = "diff_updated"
	EventPatchUpdated       EventType = "patch_updated"
	EventPlanUpdated        EventType = "plan_updated"
)

type Event struct {
	Type        EventType       `json:"type"`
	Method      string          `json:"method"`
	ThreadID    string          `json:"threadId,omitempty"`
	TurnID      string          `json:"turnId,omitempty"`
	ItemID      string          `json:"itemId,omitempty"`
	Delta       string          `json:"delta,omitempty"`
	Diff        string          `json:"diff,omitempty"`
	Error       string          `json:"error,omitempty"`
	Explanation string          `json:"explanation,omitempty"`
	Status      any             `json:"status,omitempty"`
	Plan        json.RawMessage `json:"plan,omitempty"`
	Changes     json.RawMessage `json:"changes,omitempty"`
	RawParams   json.RawMessage `json:"rawParams,omitempty"`
}

func MapNotification(msg Message) (Event, error) {
	if len(msg.ID) > 0 {
		return Event{}, errors.New("cannot map response as notification")
	}
	if msg.Method == "" {
		return Event{}, errors.New("notification method is required")
	}

	fields, err := rawFields(msg.Params)
	if err != nil {
		return Event{}, err
	}

	event := Event{
		Type:      eventTypeForMethod(msg.Method),
		Method:    msg.Method,
		ThreadID:  rawString(fields, "threadId"),
		TurnID:    rawString(fields, "turnId"),
		ItemID:    rawString(fields, "itemId"),
		Delta:     rawString(fields, "delta"),
		Diff:      rawString(fields, "diff"),
		Error:     firstRawString(fields, "message", "error", "reason"),
		RawParams: msg.Params,
	}
	if explanation := rawString(fields, "explanation"); explanation != "" {
		event.Explanation = explanation
	}
	if status, ok := fields["status"]; ok {
		event.Status = decodeAny(status)
	}
	if plan, ok := fields["plan"]; ok {
		event.Plan = plan
	}
	if changes, ok := fields["changes"]; ok {
		event.Changes = changes
	}
	return event, nil
}

func eventTypeForMethod(method string) EventType {
	switch method {
	case "error":
		return EventError
	case "thread/status/changed":
		return EventThreadStatus
	case "turn/started":
		return EventTurnStarted
	case "turn/completed":
		return EventTurnCompleted
	case "item/started":
		return EventItemStarted
	case "item/completed":
		return EventItemCompleted
	case "hook/started":
		return EventHookStarted
	case "hook/completed":
		return EventHookCompleted
	case "item/agentMessage/delta":
		return EventAgentMessageDelta
	case "item/reasoning/textDelta", "item/reasoning/summaryTextDelta":
		return EventReasoningDelta
	case "item/commandExecution/outputDelta", "command/exec/outputDelta", "process/outputDelta":
		return EventCommandOutputDelta
	case "item/fileChange/outputDelta":
		return EventFileOutputDelta
	case "turn/diff/updated":
		return EventDiffUpdated
	case "item/fileChange/patchUpdated":
		return EventPatchUpdated
	case "turn/plan/updated":
		return EventPlanUpdated
	default:
		return EventNotification
	}
}

func rawFields(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func rawString(fields map[string]json.RawMessage, key string) string {
	raw, ok := fields[key]
	if !ok {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return ""
	}
	return text
}

func firstRawString(fields map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		if value := rawString(fields, key); value != "" {
			return value
		}
	}
	return ""
}

func decodeAny(raw json.RawMessage) any {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}
