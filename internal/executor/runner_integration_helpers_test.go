//go:build integration

package executor

import (
	"context"
	"sync/atomic"
)

// mockAlertProcessor implements AlertEventProcessor for integration tests
type mockAlertProcessor struct {
	events []AlertEvent
}

func (m *mockAlertProcessor) ProcessEvent(event AlertEvent) {
	m.events = append(m.events, event)
}

// mockIntegrationBackend implements Backend for integration tests
type mockIntegrationBackend struct {
	name       string
	results    []*BackendResult
	resultIdx  int32
	executeErr error
	execCount  int32
	available  bool
}

func newMockIntegrationBackend(name string, available bool) *mockIntegrationBackend {
	return &mockIntegrationBackend{
		name:      name,
		available: available,
		results:   make([]*BackendResult, 0),
	}
}

func (m *mockIntegrationBackend) Name() string {
	return m.name
}

func (m *mockIntegrationBackend) IsAvailable() bool {
	return m.available
}

func (m *mockIntegrationBackend) Execute(ctx context.Context, opts ExecuteOptions) (*BackendResult, error) {
	atomic.AddInt32(&m.execCount, 1)

	if m.executeErr != nil {
		return nil, m.executeErr
	}

	idx := int(atomic.AddInt32(&m.resultIdx, 1)) - 1
	if idx >= len(m.results) {
		// Return last result if exhausted
		if len(m.results) > 0 {
			return m.results[len(m.results)-1], nil
		}
		return &BackendResult{Success: true, Output: "default success"}, nil
	}
	return m.results[idx], nil
}

func (m *mockIntegrationBackend) addResult(result *BackendResult) {
	m.results = append(m.results, result)
}

func (m *mockIntegrationBackend) getExecCount() int {
	return int(atomic.LoadInt32(&m.execCount))
}

// testQualityChecker is a simple QualityChecker for tests
type testQualityChecker struct {
	checkFunc func(ctx context.Context) (*QualityOutcome, error)
}

func (t *testQualityChecker) Check(ctx context.Context) (*QualityOutcome, error) {
	return t.checkFunc(ctx)
}
