package budget

import (
	"context"
	"sync"

	"github.com/ylcn91/pilot/internal/memory"
)

// mockUsageProvider implements UsageProvider for testing
type mockUsageProvider struct {
	dailyCost   float64
	monthlyCost float64
	callCount   int
	mu          sync.Mutex
}

func (m *mockUsageProvider) GetUsageSummary(query memory.UsageQuery) (*memory.UsageSummary, error) {
	m.mu.Lock()
	m.callCount++
	isOddCall := m.callCount%2 == 1
	m.mu.Unlock()

	// Enforcer always calls: daily query first, then monthly query
	// So odd calls (1st, 3rd, ...) are daily, even calls (2nd, 4th, ...) are monthly
	// This fixes the bug where on day 1 of month, both queries have same duration
	if isOddCall {
		return &memory.UsageSummary{
			TotalCost: m.dailyCost,
		}, nil
	}
	return &memory.UsageSummary{
		TotalCost: m.monthlyCost,
	}, nil
}

func (m *mockUsageProvider) Reset() {
	m.mu.Lock()
	m.callCount = 0
	m.mu.Unlock()
}

// errorUsageProvider returns an error for testing error handling
type errorUsageProvider struct{}

func (e *errorUsageProvider) GetUsageSummary(query memory.UsageQuery) (*memory.UsageSummary, error) {
	return nil, context.DeadlineExceeded
}
