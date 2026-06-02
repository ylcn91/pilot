package approval

import (
	"context"
	"testing"
	"time"
)

func TestTruncateForTelegram(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "short text unchanged",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "exact length unchanged",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "long text truncated",
			input:    "hello world",
			maxLen:   8,
			expected: "hello...",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   10,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateForTelegram(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "negative duration",
			duration: -1 * time.Minute,
			expected: "expired",
		},
		{
			name:     "seconds",
			duration: 30 * time.Second,
			expected: "30 seconds",
		},
		{
			name:     "one minute",
			duration: 1 * time.Minute,
			expected: "1 minutes",
		},
		{
			name:     "multiple minutes",
			duration: 45 * time.Minute,
			expected: "45 minutes",
		},
		{
			name:     "one hour",
			duration: 1 * time.Hour,
			expected: "1 hour",
		},
		{
			name:     "multiple hours",
			duration: 5 * time.Hour,
			expected: "5 hours",
		},
		{
			name:     "zero duration",
			duration: 0,
			expected: "0 seconds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestTelegramHandler_FormatApprovalMessage_Stages(t *testing.T) {
	client := &mockTelegramClient{}
	handler := NewTelegramHandler(client, "chat123")

	tests := []struct {
		stage     Stage
		wantIcon  string
		wantLabel string
	}{
		{StagePreExecution, "🚀", "Pre-Execution Approval"},
		{StagePreMerge, "🔀", "Pre-Merge Approval"},
		{StagePostFailure, "❌", "Post-Failure Decision"},
		{Stage("unknown"), "⚠️", "Approval Required"},
	}

	for _, tt := range tests {
		t.Run(string(tt.stage), func(t *testing.T) {
			req := &Request{
				ID:        "test",
				TaskID:    "TASK-01",
				Stage:     tt.stage,
				Title:     "Test",
				ExpiresAt: time.Now().Add(1 * time.Hour),
			}

			text := handler.formatApprovalMessage(req)

			if !containsString(text, tt.wantIcon) {
				t.Errorf("expected icon '%s' in message", tt.wantIcon)
			}
			if !containsString(text, tt.wantLabel) {
				t.Errorf("expected label '%s' in message", tt.wantLabel)
			}
		})
	}
}

func TestTelegramHandler_FormatResponseMessage(t *testing.T) {
	client := &mockTelegramClient{}
	handler := NewTelegramHandler(client, "chat123")

	req := &Request{
		ID:     "test",
		TaskID: "TASK-01",
		Title:  "Test Task",
	}

	tests := []struct {
		decision   Decision
		wantIcon   string
		wantStatus string
	}{
		{DecisionApproved, "✅", "APPROVED"},
		{DecisionRejected, "❌", "REJECTED"},
		{DecisionTimeout, "⏱", "TIMEOUT"},
	}

	for _, tt := range tests {
		t.Run(string(tt.decision), func(t *testing.T) {
			text := handler.formatResponseMessage(req, tt.decision, "testuser")

			if !containsString(text, tt.wantIcon) {
				t.Errorf("expected icon '%s' in message", tt.wantIcon)
			}
			if !containsString(text, tt.wantStatus) {
				t.Errorf("expected status '%s' in message", tt.wantStatus)
			}
			if !containsString(text, "testuser") {
				t.Error("expected username in message")
			}
		})
	}
}

func TestTelegramHandler_CreateApprovalKeyboard(t *testing.T) {
	client := &mockTelegramClient{}
	handler := NewTelegramHandler(client, "chat123")

	tests := []struct {
		stage       Stage
		wantApprove string
		wantReject  string
	}{
		{StagePreExecution, "Execute", "Cancel"},
		{StagePreMerge, "Merge", "Reject"},
		{StagePostFailure, "Retry", "Abort"},
		{Stage("unknown"), "Approve", "Reject"},
	}

	for _, tt := range tests {
		t.Run(string(tt.stage), func(t *testing.T) {
			req := &Request{
				ID:    "test-kb",
				Stage: tt.stage,
			}

			keyboard := handler.createApprovalKeyboard(req)

			if len(keyboard) != 1 || len(keyboard[0]) != 2 {
				t.Fatalf("expected 1x2 keyboard, got %v", keyboard)
			}

			if !containsString(keyboard[0][0].Text, tt.wantApprove) {
				t.Errorf("expected approve button to contain '%s', got '%s'", tt.wantApprove, keyboard[0][0].Text)
			}
			if !containsString(keyboard[0][1].Text, tt.wantReject) {
				t.Errorf("expected reject button to contain '%s', got '%s'", tt.wantReject, keyboard[0][1].Text)
			}

			// Verify callback data format
			if keyboard[0][0].CallbackData != "approve:test-kb" {
				t.Errorf("expected callback 'approve:test-kb', got '%s'", keyboard[0][0].CallbackData)
			}
			if keyboard[0][1].CallbackData != "reject:test-kb" {
				t.Errorf("expected callback 'reject:test-kb', got '%s'", keyboard[0][1].CallbackData)
			}
		})
	}
}

func TestTelegramHandler_NilMessageResponse(t *testing.T) {
	// Create a client that returns nil result
	client := &mockTelegramClient{}
	// Modify to return nil result
	handler := NewTelegramHandler(client, "chat123")

	req := &Request{
		ID:        "req-nil",
		TaskID:    "TASK-01",
		Stage:     StagePreExecution,
		Title:     "Test task",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	// Should handle gracefully even with message ID 0
	respCh, err := handler.SendApprovalRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if respCh == nil {
		t.Fatal("expected non-nil response channel")
	}
}

func TestTelegramHandler_CancelWithZeroMessageID(t *testing.T) {
	// Create handler that tracks a request with message ID 0
	client := &mockTelegramClient{}
	handler := NewTelegramHandler(client, "chat123")

	// Manually add a pending request with 0 message ID
	handler.mu.Lock()
	handler.pending["req-zero"] = &telegramPending{
		Request: &Request{
			ID:     "req-zero",
			TaskID: "TASK-01",
			Title:  "Test",
		},
		MessageID:  0, // Zero message ID
		ResponseCh: make(chan *Response, 1),
	}
	handler.mu.Unlock()

	// Cancel should not attempt to edit message
	err := handler.CancelRequest(context.Background(), "req-zero")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No messages should be edited since message ID is 0
	edited := client.getEditedMessages()
	if len(edited) != 0 {
		t.Errorf("expected no edited messages for zero message ID, got %d", len(edited))
	}
}
