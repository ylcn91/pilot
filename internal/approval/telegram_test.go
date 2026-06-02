package approval

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTelegramHandler_Name(t *testing.T) {
	client := &mockTelegramClient{}
	handler := NewTelegramHandler(client, "12345")

	if handler.Name() != "telegram" {
		t.Errorf("expected name 'telegram', got '%s'", handler.Name())
	}
}

func TestTelegramHandler_SendApprovalRequest(t *testing.T) {
	tests := []struct {
		name          string
		stage         Stage
		wantKeyboard  string
		wantStageText string
	}{
		{
			name:          "pre_execution stage",
			stage:         StagePreExecution,
			wantKeyboard:  "Execute",
			wantStageText: "Pre-Execution Approval",
		},
		{
			name:          "pre_merge stage",
			stage:         StagePreMerge,
			wantKeyboard:  "Merge",
			wantStageText: "Pre-Merge Approval",
		},
		{
			name:          "post_failure stage",
			stage:         StagePostFailure,
			wantKeyboard:  "Retry",
			wantStageText: "Post-Failure Decision",
		},
		{
			name:          "unknown stage",
			stage:         Stage("unknown"),
			wantKeyboard:  "Approve",
			wantStageText: "Approval Required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &mockTelegramClient{}
			handler := NewTelegramHandler(client, "chat123")

			req := &Request{
				ID:        "req-1",
				TaskID:    "TASK-01",
				Stage:     tt.stage,
				Title:     "Test task title",
				ExpiresAt: time.Now().Add(1 * time.Hour),
			}

			respCh, err := handler.SendApprovalRequest(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if respCh == nil {
				t.Fatal("expected non-nil response channel")
			}

			// Verify message was sent
			msgs := client.getSentMessages()
			if len(msgs) != 1 {
				t.Fatalf("expected 1 message sent, got %d", len(msgs))
			}

			msg := msgs[0]
			if msg.ChatID != "chat123" {
				t.Errorf("expected chat ID 'chat123', got '%s'", msg.ChatID)
			}

			// Check message contains expected text
			if !containsString(msg.Text, tt.wantStageText) {
				t.Errorf("expected message to contain '%s', got '%s'", tt.wantStageText, msg.Text)
			}

			// Check keyboard
			if len(msg.Keyboard) != 1 || len(msg.Keyboard[0]) != 2 {
				t.Errorf("expected 1 row with 2 buttons, got %v", msg.Keyboard)
			}

			if !containsString(msg.Keyboard[0][0].Text, tt.wantKeyboard) {
				t.Errorf("expected approve button to contain '%s', got '%s'", tt.wantKeyboard, msg.Keyboard[0][0].Text)
			}
		})
	}
}

func TestTelegramHandler_SendApprovalRequest_WithMetadata(t *testing.T) {
	client := &mockTelegramClient{}
	handler := NewTelegramHandler(client, "chat123")

	req := &Request{
		ID:          "req-meta",
		TaskID:      "TASK-01",
		Stage:       StagePreMerge,
		Title:       "Test PR merge",
		Description: "This is a detailed description",
		Metadata: map[string]interface{}{
			"pr_url": "https://github.com/org/repo/pull/123",
			"error":  "Some error message",
		},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	_, err := handler.SendApprovalRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := client.getSentMessages()
	msg := msgs[0]

	// Check metadata is included
	if !containsString(msg.Text, "https://github.com/org/repo/pull/123") {
		t.Error("expected PR URL in message")
	}
	if !containsString(msg.Text, "Some error message") {
		t.Error("expected error in message")
	}
	if !containsString(msg.Text, "This is a detailed description") {
		t.Error("expected description in message")
	}
}

func TestTelegramHandler_SendApprovalRequest_Error(t *testing.T) {
	client := &mockTelegramClient{
		sendError: errors.New("network error"),
	}
	handler := NewTelegramHandler(client, "chat123")

	req := &Request{
		ID:        "req-err",
		TaskID:    "TASK-01",
		Stage:     StagePreExecution,
		Title:     "Test task",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	_, err := handler.SendApprovalRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !containsString(err.Error(), "failed to send Telegram message") {
		t.Errorf("expected error about Telegram message, got: %v", err)
	}
}

func TestTelegramHandler_CancelRequest(t *testing.T) {
	client := &mockTelegramClient{}
	handler := NewTelegramHandler(client, "chat123")

	req := &Request{
		ID:        "req-cancel",
		TaskID:    "TASK-01",
		Stage:     StagePreExecution,
		Title:     "Test task to cancel",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	// Send request first
	_, err := handler.SendApprovalRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error sending request: %v", err)
	}

	// Now cancel it
	err = handler.CancelRequest(context.Background(), "req-cancel")
	if err != nil {
		t.Fatalf("unexpected error cancelling: %v", err)
	}

	// Verify message was edited
	edited := client.getEditedMessages()
	if len(edited) != 1 {
		t.Fatalf("expected 1 edited message, got %d", len(edited))
	}

	if !containsString(edited[0].Text, "CANCELLED") {
		t.Errorf("expected cancelled message, got: %s", edited[0].Text)
	}
}

func TestTelegramHandler_CancelRequest_NotFound(t *testing.T) {
	client := &mockTelegramClient{}
	handler := NewTelegramHandler(client, "chat123")

	// Cancel a request that doesn't exist
	err := handler.CancelRequest(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("expected no error for nonexistent request, got: %v", err)
	}

	// No messages should be edited
	edited := client.getEditedMessages()
	if len(edited) != 0 {
		t.Errorf("expected no edited messages, got %d", len(edited))
	}
}

func TestTelegramHandler_CancelRequest_EditError(t *testing.T) {
	client := &mockTelegramClient{
		editError: errors.New("edit failed"),
	}
	handler := NewTelegramHandler(client, "chat123")

	req := &Request{
		ID:        "req-edit-err",
		TaskID:    "TASK-01",
		Stage:     StagePreExecution,
		Title:     "Test task",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	_, err := handler.SendApprovalRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error sending request: %v", err)
	}

	// Cancel should not fail even if edit fails (just logs warning)
	err = handler.CancelRequest(context.Background(), "req-edit-err")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}
