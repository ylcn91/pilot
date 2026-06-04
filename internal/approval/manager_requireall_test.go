package approval

import (
	"context"
	"sync"
	"testing"
	"time"
)

// multiResponseHandler emits a configurable sequence of responses on the same
// channel so require_all accumulation across multiple approvers can be tested.
type multiResponseHandler struct {
	name      string
	responses []*Response
	mu        sync.Mutex
	sent      int
}

func (h *multiResponseHandler) Name() string { return h.name }

func (h *multiResponseHandler) SendApprovalRequest(_ context.Context, _ *Request) (<-chan *Response, error) {
	ch := make(chan *Response, len(h.responses))
	go func() {
		for _, r := range h.responses {
			time.Sleep(5 * time.Millisecond)
			h.mu.Lock()
			h.sent++
			h.mu.Unlock()
			ch <- r
		}
	}()
	return ch, nil
}

func (h *multiResponseHandler) CancelRequest(_ context.Context, _ string) error { return nil }

func waitForWriterCalls(w *mockPRStateWriter, n int, d time.Duration) []struct{ requestID, decision, by string } {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if calls := w.getCalls(); len(calls) >= n {
			return calls
		}
		time.Sleep(5 * time.Millisecond)
	}
	return w.getCalls()
}

func TestManager_RequireAll(t *testing.T) {
	tests := []struct {
		name           string
		approvers      []string
		responses      []*Response
		wantResolved   bool
		wantDecision   string
		wantApprovedBy string
	}{
		{
			name:      "single approver approves resolves",
			approvers: []string{"alice"},
			responses: []*Response{
				{Decision: DecisionApproved, ApprovedBy: "alice"},
			},
			wantResolved:   true,
			wantDecision:   "approved",
			wantApprovedBy: "alice",
		},
		{
			name:      "two approvers first approve does not resolve",
			approvers: []string{"alice", "bob"},
			responses: []*Response{
				{Decision: DecisionApproved, ApprovedBy: "alice"},
			},
			wantResolved: false,
		},
		{
			name:      "two approvers both approve resolves",
			approvers: []string{"alice", "bob"},
			responses: []*Response{
				{Decision: DecisionApproved, ApprovedBy: "alice"},
				{Decision: DecisionApproved, ApprovedBy: "bob"},
			},
			wantResolved:   true,
			wantDecision:   "approved",
			wantApprovedBy: "bob",
		},
		{
			name:      "two approvers rejection resolves immediately",
			approvers: []string{"alice", "bob"},
			responses: []*Response{
				{Decision: DecisionRejected, ApprovedBy: "alice"},
			},
			wantResolved:   true,
			wantDecision:   "rejected",
			wantApprovedBy: "alice",
		},
		{
			name:      "duplicate approval from same approver does not resolve",
			approvers: []string{"alice", "bob"},
			responses: []*Response{
				{Decision: DecisionApproved, ApprovedBy: "alice"},
				{Decision: DecisionApproved, ApprovedBy: "alice"},
			},
			wantResolved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.Enabled = true
			config.PreExecution.Enabled = true
			config.PreExecution.Timeout = 5 * time.Second
			config.PreExecution.RequireAll = true

			m := NewManager(config)
			writer := &mockPRStateWriter{}
			m.WithStateWriter(writer)

			handler := &multiResponseHandler{name: "test", responses: tt.responses}
			m.RegisterHandler(handler)

			req := &Request{
				ID:        "req-requireall",
				TaskID:    "TASK-RA",
				Stage:     StagePreExecution,
				Approvers: tt.approvers,
				CreatedAt: time.Now(),
			}

			if _, err := m.SubmitApprovalRequest(context.Background(), req); err != nil {
				t.Fatalf("submit: %v", err)
			}

			if tt.wantResolved {
				calls := waitForWriterCalls(writer, 1, 500*time.Millisecond)
				if len(calls) != 1 {
					t.Fatalf("expected 1 writer call, got %d", len(calls))
				}
				if calls[0].decision != tt.wantDecision {
					t.Errorf("decision = %q, want %q", calls[0].decision, tt.wantDecision)
				}
				if calls[0].by != tt.wantApprovedBy {
					t.Errorf("approvedBy = %q, want %q", calls[0].by, tt.wantApprovedBy)
				}
				if pending := m.GetPendingRequests(); len(pending) != 0 {
					t.Errorf("expected 0 pending after resolve, got %d", len(pending))
				}
			} else {
				// Give the goroutine time to consume all responses without resolving.
				time.Sleep(150 * time.Millisecond)
				if calls := writer.getCalls(); len(calls) != 0 {
					t.Fatalf("expected no writer call (not all approved), got %d: %+v", len(calls), calls)
				}
				if pending := m.GetPendingRequests(); len(pending) != 1 {
					t.Errorf("expected request to stay pending, got %d pending", len(pending))
				}
			}
		})
	}
}
