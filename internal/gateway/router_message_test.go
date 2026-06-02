package gateway

import (
	"encoding/json"
	"testing"
)

func TestMessageTypes(t *testing.T) {
	tests := []struct {
		name     string
		msgType  MessageType
		expected string
	}{
		{"task type", MessageTypeTask, "task"},
		{"status type", MessageTypeStatus, "status"},
		{"progress type", MessageTypeProgress, "progress"},
		{"ping type", MessageTypePing, "ping"},
		{"pong type", MessageTypePong, "pong"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.msgType) != tt.expected {
				t.Errorf("MessageType = %s, want %s", tt.msgType, tt.expected)
			}
		})
	}
}

func TestMessageSerialization(t *testing.T) {
	tests := []struct {
		name    string
		message Message
	}{
		{
			name: "task message",
			message: Message{
				Type:    MessageTypeTask,
				Payload: json.RawMessage(`{"id":"task-123","title":"Test Task"}`),
			},
		},
		{
			name: "status message",
			message: Message{
				Type:    MessageTypeStatus,
				Payload: json.RawMessage(`{"running":true,"sessions":5}`),
			},
		},
		{
			name: "progress message",
			message: Message{
				Type:    MessageTypeProgress,
				Payload: json.RawMessage(`{"percent":75,"message":"Processing..."}`),
			},
		},
		{
			name: "ping message with timestamp",
			message: Message{
				Type:    MessageTypePing,
				Payload: json.RawMessage(`{"timestamp":1234567890}`),
			},
		},
		{
			name: "message with empty payload",
			message: Message{
				Type:    MessageTypePong,
				Payload: json.RawMessage(`{}`),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Serialize
			data, err := json.Marshal(tt.message)
			if err != nil {
				t.Fatalf("Failed to marshal: %v", err)
			}

			// Deserialize
			var decoded Message
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}

			if decoded.Type != tt.message.Type {
				t.Errorf("Type = %s, want %s", decoded.Type, tt.message.Type)
			}
			if string(decoded.Payload) != string(tt.message.Payload) {
				t.Errorf("Payload = %s, want %s", decoded.Payload, tt.message.Payload)
			}
		})
	}
}
