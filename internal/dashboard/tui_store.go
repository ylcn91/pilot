package dashboard

import (
	"fmt"
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ylcn91/pilot/internal/memory"
)

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

	// Load recent executions as completed tasks
	executions, err := m.store.GetRecentExecutions(20)
	if err != nil {
		slog.Warn("failed to load recent executions", slog.Any("error", err))
		return
	}

	// Initialize metrics card from lifetime execution data (survives restarts).
	// Session tokens only track the current process; executions table has the real totals.
	lifetime, err := m.store.GetLifetimeTokens()
	if err != nil {
		slog.Warn("failed to load lifetime tokens", slog.Any("error", err))
	} else {
		m.metricsCard.TotalTokens = int(lifetime.TotalTokens)
		m.metricsCard.InputTokens = int(lifetime.InputTokens)
		m.metricsCard.OutputTokens = int(lifetime.OutputTokens)
		m.metricsCard.TotalCostUSD = lifetime.TotalCostUSD
	}

	// Initialize task counts from lifetime data (survives restarts).
	// Previous code sampled from GetRecentExecutions(20), showing only last 20 results.
	taskCounts, err := m.store.GetLifetimeTaskCounts()
	if err != nil {
		slog.Warn("failed to load lifetime task counts", slog.Any("error", err))
	} else {
		m.metricsCard.TotalTasks = taskCounts.Total
		m.metricsCard.Succeeded = taskCounts.Succeeded
		m.metricsCard.Failed = taskCounts.Failed
		m.metricsCard.Declined = taskCounts.Declined
		m.metricsCard.NoOp = taskCounts.NoOp
		m.metricsCard.Stalled = taskCounts.Stalled
		m.metricsCard.RateLimited = taskCounts.RateLimited
		m.metricsCard.Infra = taskCounts.Infra
		m.metricsCard.Skipped = taskCounts.Skipped
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
		m.completedTasks = append(m.completedTasks, CompletedTask{
			ID:          exec.TaskID,
			Title:       exec.TaskTitle,
			Status:      status,
			Duration:    fmt.Sprintf("%dms", exec.DurationMs),
			CompletedAt: completedAt,
			PeakRSSMB:   exec.PeakRSSMB,
		})
	}

	// Compute cost per task
	if m.metricsCard.TotalTasks > 0 {
		m.metricsCard.CostPerTask = m.metricsCard.TotalCostUSD / float64(m.metricsCard.TotalTasks)
	}

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
	m.metricsCard.TokenHistory = make([]int64, 7)
	m.metricsCard.CostHistory = make([]float64, 7)
	m.metricsCard.TaskHistory = make([]int, 7)
	for i := 0; i < 7; i++ {
		day := now.AddDate(0, 0, -6+i).Format("2006-01-02")
		if dm, ok := byDate[day]; ok {
			m.metricsCard.TokenHistory[i] = dm.TotalTokens
			m.metricsCard.CostHistory[i] = dm.TotalCostUSD
			m.metricsCard.TaskHistory[i] = dm.ExecutionCount
		}
	}
}

// storeRefreshCmd queries SQLite for current execution state (GH-2248).
// Runs asynchronously so the TUI never blocks on DB I/O.
func storeRefreshCmd(store *memory.Store) tea.Cmd {
	return func() tea.Msg {
		msg := storeRefreshMsg{}

		executions, err := store.GetRecentExecutions(20)
		if err != nil {
			slog.Warn("store refresh: failed to load executions", slog.Any("error", err))
			return msg
		}
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

		lifetime, err := store.GetLifetimeTokens()
		if err != nil {
			slog.Warn("store refresh: failed to load lifetime tokens", slog.Any("error", err))
		} else {
			msg.metricsCard.TotalTokens = int(lifetime.TotalTokens)
			msg.metricsCard.InputTokens = int(lifetime.InputTokens)
			msg.metricsCard.OutputTokens = int(lifetime.OutputTokens)
			msg.metricsCard.TotalCostUSD = lifetime.TotalCostUSD
		}

		taskCounts, err := store.GetLifetimeTaskCounts()
		if err != nil {
			slog.Warn("store refresh: failed to load task counts", slog.Any("error", err))
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

		if msg.metricsCard.TotalTasks > 0 {
			msg.metricsCard.CostPerTask = msg.metricsCard.TotalCostUSD / float64(msg.metricsCard.TotalTasks)
		}

		return msg
	}
}
