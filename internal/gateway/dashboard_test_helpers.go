package gateway

import (
	"github.com/ylcn91/pilot/internal/memory"
)

// mockDashboardStore implements DashboardStore for testing.
type mockDashboardStore struct {
	lifetimeTokens *memory.LifetimeTokens
	taskCounts     *memory.LifetimeTaskCounts
	dailyMetrics   []*memory.DailyMetrics
	executions     []*memory.Execution
	queuedTasks    []*memory.Execution
	activeExecs    []*memory.Execution
	logEntries     []*memory.LogEntry
}

func (m *mockDashboardStore) GetLifetimeTokens() (*memory.LifetimeTokens, error) {
	return m.lifetimeTokens, nil
}

func (m *mockDashboardStore) GetLifetimeTaskCounts() (*memory.LifetimeTaskCounts, error) {
	return m.taskCounts, nil
}

func (m *mockDashboardStore) GetDailyMetrics(_ memory.MetricsQuery) ([]*memory.DailyMetrics, error) {
	return m.dailyMetrics, nil
}

func (m *mockDashboardStore) GetRecentExecutions(_ int) ([]*memory.Execution, error) {
	return m.executions, nil
}

func (m *mockDashboardStore) GetQueuedTasks(_ int) ([]*memory.Execution, error) {
	return m.queuedTasks, nil
}

func (m *mockDashboardStore) GetActiveExecutions() ([]*memory.Execution, error) {
	return m.activeExecs, nil
}

func (m *mockDashboardStore) GetRecentLogs(_ int) ([]*memory.LogEntry, error) {
	return m.logEntries, nil
}

func newTestServerWithDashboard(store DashboardStore) *Server {
	s := NewServer(&Config{Host: "127.0.0.1", Port: 9090})
	s.dashboard.store = store
	return s
}

// mockGitGraphResult is a minimal response struct used by the test fetcher.
type mockGitGraphResult struct {
	Lines      []interface{} `json:"lines"`
	TotalCount int           `json:"total_count"`
}
