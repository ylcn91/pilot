package approval

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTelegramHandler_HandleCallback_Approve(t *testing.T) {
	client := &mockTelegramClient{}
	handler := NewTelegramHandler(client, "chat123")

	req := &Request{
		ID:        "req-cb-approve",
		TaskID:    "TASK-01",
		Stage:     StagePreExecution,
		Title:     "Test task",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	respCh, err := handler.SendApprovalRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Handle approve callback
	handled := handler.HandleCallback(context.Background(), "cb123", "approve:req-cb-approve", "user123", "testuser")
	if !handled {
		t.Error("expected callback to be handled")
	}

	// Wait for response
	select {
	case resp := <-respCh:
		if resp == nil {
			t.Fatal("expected response, got nil")
		}
		if resp.Decision != DecisionApproved {
			t.Errorf("expected approved, got %s", resp.Decision)
		}
		if resp.ApprovedBy != "testuser" {
			t.Errorf("expected testuser, got %s", resp.ApprovedBy)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}

	// Verify callback was answered
	cbs := client.getAnsweredCallbacks()
	if len(cbs) != 1 {
		t.Fatalf("expected 1 answered callback, got %d", len(cbs))
	}
	if cbs[0].Text != "Approved!" {
		t.Errorf("expected 'Approved!', got '%s'", cbs[0].Text)
	}

	// Verify message was edited
	edited := client.getEditedMessages()
	if len(edited) != 1 {
		t.Fatalf("expected 1 edited message, got %d", len(edited))
	}
	if !containsString(edited[0].Text, "APPROVED") {
		t.Errorf("expected APPROVED in message, got: %s", edited[0].Text)
	}
}

func TestTelegramHandler_HandleCallback_Reject(t *testing.T) {
	client := &mockTelegramClient{}
	handler := NewTelegramHandler(client, "chat123")

	req := &Request{
		ID:        "req-cb-reject",
		TaskID:    "TASK-01",
		Stage:     StagePreExecution,
		Title:     "Test task",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	respCh, err := handler.SendApprovalRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Handle reject callback
	handled := handler.HandleCallback(context.Background(), "cb123", "reject:req-cb-reject", "user123", "testuser")
	if !handled {
		t.Error("expected callback to be handled")
	}

	// Wait for response
	select {
	case resp := <-respCh:
		if resp == nil {
			t.Fatal("expected response, got nil")
		}
		if resp.Decision != DecisionRejected {
			t.Errorf("expected rejected, got %s", resp.Decision)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}

	// Verify callback answer text
	cbs := client.getAnsweredCallbacks()
	if cbs[0].Text != "Rejected" {
		t.Errorf("expected 'Rejected', got '%s'", cbs[0].Text)
	}
}

func TestTelegramHandler_HandleCallback_InvalidFormat(t *testing.T) {
	client := &mockTelegramClient{}
	handler := NewTelegramHandler(client, "chat123")

	tests := []struct {
		name string
		data string
	}{
		{"empty data", ""},
		{"random data", "random:data"},
		{"partial approve", "approve"},
		{"partial reject", "reject"},
		{"invalid prefix", "unknown:req-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handled := handler.HandleCallback(context.Background(), "cb123", tt.data, "user", "username")
			if handled {
				t.Errorf("expected callback '%s' to not be handled", tt.data)
			}
		})
	}
}

func TestTelegramHandler_HandleCallback_ExpiredRequest(t *testing.T) {
	client := &mockTelegramClient{}
	handler := NewTelegramHandler(client, "chat123")

	// Handle callback for a request that doesn't exist
	handled := handler.HandleCallback(context.Background(), "cb123", "approve:nonexistent", "user", "username")
	if !handled {
		t.Error("expected callback to be handled (even for expired requests)")
	}

	// Verify "expired" callback answer was sent
	cbs := client.getAnsweredCallbacks()
	if len(cbs) != 1 {
		t.Fatalf("expected 1 answered callback, got %d", len(cbs))
	}
	if !containsString(cbs[0].Text, "expired") {
		t.Errorf("expected expired message, got: %s", cbs[0].Text)
	}
}

func TestTelegramHandler_HandleCallback_EditError(t *testing.T) {
	client := &mockTelegramClient{
		editError: errors.New("edit failed"),
	}
	handler := NewTelegramHandler(client, "chat123")

	req := &Request{
		ID:        "req-edit-fail",
		TaskID:    "TASK-01",
		Stage:     StagePreExecution,
		Title:     "Test task",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	respCh, err := handler.SendApprovalRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Handle callback - should succeed even if edit fails
	handled := handler.HandleCallback(context.Background(), "cb123", "approve:req-edit-fail", "user", "testuser")
	if !handled {
		t.Error("expected callback to be handled")
	}

	// Should still receive response
	select {
	case resp := <-respCh:
		if resp.Decision != DecisionApproved {
			t.Errorf("expected approved, got %s", resp.Decision)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}
}

func TestTelegramHandler_HandleCallbackWithZeroMessageID(t *testing.T) {
	client := &mockTelegramClient{}
	handler := NewTelegramHandler(client, "chat123")

	// Manually add a pending request with 0 message ID
	respCh := make(chan *Response, 1)
	handler.mu.Lock()
	handler.pending["req-zero-cb"] = &telegramPending{
		Request: &Request{
			ID:     "req-zero-cb",
			TaskID: "TASK-01",
			Title:  "Test",
		},
		MessageID:  0,
		ResponseCh: respCh,
	}
	handler.mu.Unlock()

	// Handle callback - should not try to edit message
	handled := handler.HandleCallback(context.Background(), "cb1", "approve:req-zero-cb", "user", "testuser")
	if !handled {
		t.Error("expected callback to be handled")
	}

	// Should still receive response
	select {
	case resp := <-respCh:
		if resp.Decision != DecisionApproved {
			t.Errorf("expected approved, got %s", resp.Decision)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}

	// No messages should be edited
	edited := client.getEditedMessages()
	if len(edited) != 0 {
		t.Errorf("expected no edited messages, got %d", len(edited))
	}
}

// TestTelegramHandler_ApproverRouting verifies that the destination chat_id is
// resolved from req.Approvers[0] when set, and falls back to the constructor
// chat_id when the slice is empty.
func TestTelegramHandler_ApproverRouting(t *testing.T) {
	t.Run("uses approver chat_id when set", func(t *testing.T) {
		client := &mockTelegramClient{}
		handler := NewTelegramHandler(client, "constructor-chat")

		req := &Request{
			ID:        "req-approver",
			TaskID:    "TASK-01",
			Stage:     StagePreMerge,
			Title:     "Test",
			Approvers: []string{"99999"},
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}

		_, err := handler.SendApprovalRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		msgs := client.getSentMessages()
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message sent, got %d", len(msgs))
		}
		if msgs[0].ChatID != "99999" {
			t.Errorf("expected chat_id '99999', got '%s'", msgs[0].ChatID)
		}
	})

	t.Run("falls back to constructor chat_id when approvers empty", func(t *testing.T) {
		client := &mockTelegramClient{}
		handler := NewTelegramHandler(client, "constructor-chat")

		req := &Request{
			ID:        "req-no-approver",
			TaskID:    "TASK-01",
			Stage:     StagePreMerge,
			Title:     "Test",
			Approvers: []string{},
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}

		_, err := handler.SendApprovalRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		msgs := client.getSentMessages()
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message sent, got %d", len(msgs))
		}
		if msgs[0].ChatID != "constructor-chat" {
			t.Errorf("expected chat_id 'constructor-chat', got '%s'", msgs[0].ChatID)
		}
	})

	t.Run("edit after callback uses same chat_id as original send", func(t *testing.T) {
		client := &mockTelegramClient{}
		handler := NewTelegramHandler(client, "constructor-chat")

		req := &Request{
			ID:        "req-edit-routing",
			TaskID:    "TASK-01",
			Stage:     StagePreMerge,
			Title:     "Test",
			Approvers: []string{"99999"},
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}

		_, err := handler.SendApprovalRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		handler.HandleCallback(context.Background(), "cb1", "approve:req-edit-routing", "user", "tester")

		edited := client.getEditedMessages()
		if len(edited) != 1 {
			t.Fatalf("expected 1 edited message, got %d", len(edited))
		}
		if edited[0].ChatID != "99999" {
			t.Errorf("expected edit chat_id '99999', got '%s'", edited[0].ChatID)
		}
	})
}
