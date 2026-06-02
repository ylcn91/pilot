package alerts

import (
	"context"
	"sync"
	"testing"
	"time"
)

// mockChannel is a test mock for the Channel interface
type mockChannel struct {
	name     string
	typ      string
	alerts   []*Alert
	mu       sync.Mutex
	err      error
	received chan struct{} // buffered; signaled on every dispatched alert
}

func newMockChannel(name, typ string) *mockChannel {
	return &mockChannel{
		name:     name,
		typ:      typ,
		alerts:   make([]*Alert, 0),
		received: make(chan struct{}, 64),
	}
}

func (m *mockChannel) Name() string { return m.name }
func (m *mockChannel) Type() string { return m.typ }

func (m *mockChannel) Send(ctx context.Context, alert *Alert) error {
	if m.err != nil {
		return m.err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alerts = append(m.alerts, alert)
	select {
	case m.received <- struct{}{}:
	default:
	}
	return nil
}

func (m *mockChannel) getAlerts() []*Alert {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*Alert, len(m.alerts))
	copy(result, m.alerts)
	return result
}

func (m *mockChannel) setError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

// waitForAlerts drains n items from ch.received within timeout and calls
// t.Fatal on timeout. Use for positive-assertion tests (expect N alerts).
func waitForAlerts(t *testing.T, ch *mockChannel, n int, timeout time.Duration) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-ch.received:
		case <-time.After(timeout):
			t.Fatalf("timeout waiting for alert %d/%d after %v", i+1, n, timeout)
		}
	}
}
