package approval

import (
	"context"
	"testing"
	"time"
)

// TestManager_SubmitApprovalRequest_FallbackWhenPreferredChannelAbsent verifies
// that when a request names a PreferredChannel that is not registered, the
// manager falls back to an available registered handler rather than dropping
// the request. Two handlers are registered (neither named the preferred
// channel); exactly one of them must receive the request, and it must be
// tracked as pending under that handler.
func TestManager_SubmitApprovalRequest_FallbackWhenPreferredChannelAbsent(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.PreExecution.Enabled = true
	config.PreExecution.Timeout = 10 * time.Second

	m := NewManager(config)

	// Two registered handlers; neither matches the preferred channel.
	// respondWith=nil → request stays pending so we can inspect dispatch.
	slackH := &mockHandler{name: "slack"}
	emailH := &mockHandler{name: "email"}
	m.RegisterHandler(slackH)
	m.RegisterHandler(emailH)

	req := &Request{
		ID:               "fallback-1",
		TaskID:           "TASK-FALLBACK",
		Stage:            StagePreExecution,
		PreferredChannel: "telegram", // not registered
		CreatedAt:        time.Now(),
	}

	requestID, err := m.SubmitApprovalRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requestID != req.ID {
		t.Errorf("expected request ID %q, got %q", req.ID, requestID)
	}

	// Exactly one handler must have received the request (fallback picks one).
	slackH.mu.Lock()
	slackCount := len(slackH.sentReqs)
	slackH.mu.Unlock()
	emailH.mu.Lock()
	emailCount := len(emailH.sentReqs)
	emailH.mu.Unlock()

	if total := slackCount + emailCount; total != 1 {
		t.Fatalf("expected exactly 1 handler to receive the request via fallback, got %d (slack=%d, email=%d)",
			total, slackCount, emailCount)
	}

	// The request must be tracked as pending under one of the registered handlers,
	// never under the absent preferred channel.
	pending := m.GetPendingRequests()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending request, got %d", len(pending))
	}
	if pending[0].ID != req.ID {
		t.Errorf("expected pending request %q, got %q", req.ID, pending[0].ID)
	}

	// Clean up the background goroutine.
	if err := m.RecordDecision(context.Background(), req.ID, DecisionApproved, "tester"); err != nil {
		t.Fatalf("record decision: %v", err)
	}
}

// TestManager_SubmitApprovalRequest_PrefersRegisteredPreferredChannel verifies
// that when the preferred channel IS registered, the request is dispatched to
// that specific handler and not the other registered one.
func TestManager_SubmitApprovalRequest_PrefersRegisteredPreferredChannel(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.PreExecution.Enabled = true
	config.PreExecution.Timeout = 10 * time.Second

	m := NewManager(config)

	slackH := &mockHandler{name: "slack"}
	telegramH := &mockHandler{name: "telegram"}
	m.RegisterHandler(slackH)
	m.RegisterHandler(telegramH)

	req := &Request{
		ID:               "preferred-1",
		TaskID:           "TASK-PREFERRED",
		Stage:            StagePreExecution,
		PreferredChannel: "telegram", // registered
		CreatedAt:        time.Now(),
	}

	if _, err := m.SubmitApprovalRequest(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	telegramH.mu.Lock()
	telegramCount := len(telegramH.sentReqs)
	telegramH.mu.Unlock()
	slackH.mu.Lock()
	slackCount := len(slackH.sentReqs)
	slackH.mu.Unlock()

	if telegramCount != 1 {
		t.Errorf("expected preferred telegram handler to receive 1 request, got %d", telegramCount)
	}
	if slackCount != 0 {
		t.Errorf("expected non-preferred slack handler to receive 0 requests, got %d", slackCount)
	}

	if err := m.RecordDecision(context.Background(), req.ID, DecisionApproved, "tester"); err != nil {
		t.Fatalf("record decision: %v", err)
	}
}
