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

// TestManager_SubmitApprovalRequest_FallbackIsDeterministic verifies that the
// fallback handler selection is stable across runs. Each iteration builds a
// fresh Manager (so Go's per-map iteration-order randomization is re-seeded)
// and asserts the same handler is chosen every time. With handlers "email" and
// "slack" registered, the lexicographically-first key ("email") must always win.
func TestManager_SubmitApprovalRequest_FallbackIsDeterministic(t *testing.T) {
	const iterations = 50
	var chosen string

	for i := 0; i < iterations; i++ {
		config := DefaultConfig()
		config.Enabled = true
		config.PreExecution.Enabled = true
		config.PreExecution.Timeout = 10 * time.Second

		m := NewManager(config)
		slackH := &mockHandler{name: "slack"}
		emailH := &mockHandler{name: "email"}
		m.RegisterHandler(slackH)
		m.RegisterHandler(emailH)

		req := &Request{
			ID:               "det-fallback",
			TaskID:           "TASK-DET",
			Stage:            StagePreExecution,
			PreferredChannel: "", // no preference → deterministic fallback
			CreatedAt:        time.Now(),
		}

		if _, err := m.SubmitApprovalRequest(context.Background(), req); err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}

		var got string
		slackH.mu.Lock()
		if len(slackH.sentReqs) == 1 {
			got = slackH.name
		}
		slackH.mu.Unlock()
		emailH.mu.Lock()
		if len(emailH.sentReqs) == 1 {
			got = emailH.name
		}
		emailH.mu.Unlock()

		if got == "" {
			t.Fatalf("iteration %d: no handler received the request", i)
		}
		if i == 0 {
			chosen = got
		} else if got != chosen {
			t.Fatalf("non-deterministic fallback: iteration %d chose %q, first run chose %q", i, got, chosen)
		}

		if err := m.RecordDecision(context.Background(), req.ID, DecisionApproved, "tester"); err != nil {
			t.Fatalf("iteration %d: record decision: %v", i, err)
		}
	}

	if chosen != "email" {
		t.Errorf("expected lexicographically-first handler %q to be chosen, got %q", "email", chosen)
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
