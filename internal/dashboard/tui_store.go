package dashboard

import (
	"fmt"
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ylcn91/pilot/internal/memory"
)

// loadStoreSnapshot queries SQLite for current execution state and maps it into
// a storeRefreshMsg. The bool reports whether the recent-executions query
// succeeded; callers use it to decide whether to apply the snapshot. The
// returned snapshot carries the most recent 5 executions as completed tasks
// plus lifetime token/task metrics (cost-per-task included).
func loadStoreSnapshot(store *memory.Store) (storeRefreshMsg, bool) {
	msg := storeRefreshMsg{}

	executions, err := store.GetRecentExecutions(20)
	if err != nil {
		slog.Warn("failed to load recent executions", slog.Any("error", err))
		return msg, false
	}

	// Initialize metrics card from lifetime execution data (survives restarts).
	// Session tokens only track the current process; executions table has the real totals.
	lifetime, err := store.GetLifetimeTokens()
	if err != nil {
		slog.Warn("failed to load lifetime tokens", slog.Any("error", err))
	} else {
		msg.metricsCard.TotalTokens = int(lifetime.TotalTokens)
		msg.metricsCard.InputTokens = int(lifetime.InputTokens)
		msg.metricsCard.OutputTokens = int(lifetime.OutputTokens)
		msg.metricsCard.TotalCostUSD = lifetime.TotalCostUSD
	}

	// Initialize task counts from lifetime data (survives restarts).
	// Previous code sampled from GetRecentExecutions(20), showing only last 20 results.
	taskCounts, err := store.GetLifetimeTaskCounts()
	if err != nil {
		slog.Warn("failed to load lifetime task counts", slog.Any("error", err))
	} else {
		msg.metricsCard.TotalTasks = taskCounts.Total
		msg.metricsCard.Succeeded = taskCounts.Succeeded
		msg.metricsCard.Failed = taskCounts.Failed
		msg.metricsCard.Declined = taskCounts.Declined
		msg.metricsCard.NoOp = taskCounts.NoOp
		msg.metricsCard.Stalled = taskCounts.Stalled
		msg.metricsCard.RateLimited = taskCounts.RateLimited
		msg.metricsCard.Infra = taskCounts.Infra
		msg.metricsCard.Skipped = taskCounts.Skipped
	}

	// Populate history panel from recent executions (most recent 5)
	for i, exec := range executions {
		if i >= 5 {
			break
		}
		status := "success"
		if exec.Status == "failed" {
			status = "failed"
		}
		completedAt := exec.CreatedAt
		if exec.CompletedAt != nil {
			completedAt = *exec.CompletedAt
		}
		msg.completedTasks = append(msg.completedTasks, CompletedTask{
			ID:          exec.TaskID,
			Title:       exec.TaskTitle,
			Status:      status,
			Duration:    fmt.Sprintf("%dms", exec.DurationMs),
			CompletedAt: completedAt,
			PeakRSSMB:   exec.PeakRSSMB,
		})
	}

	// Compute cost per task
	if msg.metricsCard.TotalTasks > 0 {
		msg.metricsCard.CostPerTask = msg.metricsCard.TotalCostUSD / float64(msg.metricsCard.TotalTasks)
	}

	return msg, true
}

// hydrateFromStore loads persisted state from SQLite.
func (m *Model) hydrateFromStore() {
	if m.store == nil {
		return
	}

	// Get or create today's session
	session, err := m.store.GetOrCreateDailySession()
	if err != nil {
		slog.Warn("failed to get/create session", slog.Any("error", err))
	} else {
		m.sessionID = session.ID
		m.tokenUsage = TokenUsage{
			InputTokens:  session.TotalInputTokens,
			OutputTokens: session.TotalOutputTokens,
			TotalTokens:  session.TotalInputTokens + session.TotalOutputTokens,
		}
	}

	snapshot, ok := loadStoreSnapshot(m.store)
	if !ok {
		return
	}
	m.metrics.card = snapshot.metricsCard
	m.completedTasks = append(m.completedTasks, snapshot.completedTasks...)

	// Load sparkline history
	m.loadMetricsHistory()
}

// persistTokenUsage saves token usage to the current session.
func (m *Model) persistTokenUsage(inputDelta, outputDelta int) {
	if m.store == nil || m.sessionID == "" {
		return
	}
	if err := m.store.UpdateSessionTokens(m.sessionID, inputDelta, outputDelta); err != nil {
		slog.Warn("failed to persist token usage", slog.Any("error", err))
	}
}

// loadMetricsHistory queries daily metrics for the past 7 days and populates sparkline history arrays.
func (m *Model) loadMetricsHistory() {
	if m.store == nil {
		return
	}
	now := time.Now()
	query := memory.MetricsQuery{
		Start: now.AddDate(0, 0, -7),
		End:   now,
	}
	dailyMetrics, err := m.store.GetDailyMetrics(query)
	if err != nil {
		slog.Warn("failed to load metrics history", slog.Any("error", err))
		return
	}

	// Build date→metrics map (GetDailyMetrics returns DESC order)
	byDate := make(map[string]*memory.DailyMetrics, len(dailyMetrics))
	for _, dm := range dailyMetrics {
		byDate[dm.Date.Format("2006-01-02")] = dm
	}

	// Fill 7-day arrays oldest→newest (left→right in sparkline)
	m.metrics.card.TokenHistory = make([]int64, 7)
	m.metrics.card.CostHistory = make([]float64, 7)
	m.metrics.card.TaskHistory = make([]int, 7)
	for i := 0; i < 7; i++ {
		day := now.AddDate(0, 0, -6+i).Format("2006-01-02")
		if dm, ok := byDate[day]; ok {
			m.metrics.card.TokenHistory[i] = dm.TotalTokens
			m.metrics.card.CostHistory[i] = dm.TotalCostUSD
			m.metrics.card.TaskHistory[i] = dm.ExecutionCount
		}
	}
}

// storeRefreshCmd queries SQLite for current execution state (GH-2248).
// Runs asynchronously so the TUI never blocks on DB I/O.
func storeRefreshCmd(store *memory.Store) tea.Cmd {
	return func() tea.Msg {
		msg, _ := loadStoreSnapshot(store)
		return msg
	}
}
