package approval

import (
	"context"
	"sync"
	"time"
)

// mockPRStateWriter records SetApprovalDecision calls for test assertions.
type mockPRStateWriter struct {
	mu    sync.Mutex
	calls []struct{ requestID, decision, by string }
}

func (w *mockPRStateWriter) SetApprovalDecision(_ context.Context, requestID, decision, by string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, struct{ requestID, decision, by string }{requestID, decision, by})
	return nil
}

func (w *mockPRStateWriter) getCalls() []struct{ requestID, decision, by string } {
	w.mu.Lock()
	defer w.mu.Unlock()
	result := make([]struct{ requestID, decision, by string }, len(w.calls))
	copy(result, w.calls)
	return result
}

// mockHandler is a test double for Handler
type mockHandler struct {
	name        string
	sentReqs    []*Request
	respondWith *Response
	cancelCalls []string
	mu          sync.Mutex
}

func (m *mockHandler) Name() string {
	return m.name
}

func (m *mockHandler) SendApprovalRequest(ctx context.Context, req *Request) (<-chan *Response, error) {
	m.mu.Lock()
	m.sentReqs = append(m.sentReqs, req)
	m.mu.Unlock()
	ch := make(chan *Response, 1)
	if m.respondWith != nil {
		go func() {
			time.Sleep(10 * time.Millisecond) // Simulate async response
			ch <- m.respondWith
		}()
	}
	return ch, nil
}

func (m *mockHandler) CancelRequest(ctx context.Context, requestID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancelCalls = append(m.cancelCalls, requestID)
	return nil
}
