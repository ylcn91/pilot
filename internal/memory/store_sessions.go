package memory

import (
	"database/sql"
	"fmt"
	"time"
)

// Session represents a dashboard session with token usage and task counts.
// Sessions are keyed by date (YYYY-MM-DD) for daily aggregation.
type Session struct {
	ID                string
	Date              string // YYYY-MM-DD format
	StartedAt         time.Time
	EndedAt           *time.Time
	TotalInputTokens  int
	TotalOutputTokens int
	TotalCostCents    int
	TasksCompleted    int
	TasksFailed       int
}

// GetOrCreateDailySession retrieves today's session or creates a new one.
// Sessions are keyed by date to aggregate daily metrics.
func (s *Store) GetOrCreateDailySession() (*Session, error) {
	today := time.Now().Format("2006-01-02")

	// Try to get existing session for today
	row := s.db.QueryRow(`
		SELECT id, date, started_at, ended_at, total_input_tokens, total_output_tokens,
		       total_cost_cents, tasks_completed, tasks_failed
		FROM sessions WHERE date = ?
	`, today)

	var session Session
	var endedAt sql.NullTime
	err := row.Scan(&session.ID, &session.Date, &session.StartedAt, &endedAt,
		&session.TotalInputTokens, &session.TotalOutputTokens,
		&session.TotalCostCents, &session.TasksCompleted, &session.TasksFailed)

	if err == sql.ErrNoRows {
		// Create new session for today
		session = Session{
			ID:        fmt.Sprintf("session-%s-%d", today, time.Now().UnixNano()),
			Date:      today,
			StartedAt: time.Now(),
		}
		err = s.withRetry("GetOrCreateDailySession", func() error {
			_, err := s.db.Exec(`
				INSERT INTO sessions (id, date, started_at)
				VALUES (?, ?, ?)
			`, session.ID, session.Date, session.StartedAt)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create session: %w", err)
		}
		return &session, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	if endedAt.Valid {
		session.EndedAt = &endedAt.Time
	}

	return &session, nil
}

// UpdateSessionTokens updates token counts for a session.
func (s *Store) UpdateSessionTokens(sessionID string, inputTokens, outputTokens int) error {
	return s.withRetry("UpdateSessionTokens", func() error {
		_, err := s.db.Exec(`
			UPDATE sessions
			SET total_input_tokens = total_input_tokens + ?,
			    total_output_tokens = total_output_tokens + ?
			WHERE id = ?
		`, inputTokens, outputTokens, sessionID)
		return err
	})
}

// UpdateSessionTaskCount updates task completion/failure counts.
func (s *Store) UpdateSessionTaskCount(sessionID string, completed, failed int) error {
	return s.withRetry("UpdateSessionTaskCount", func() error {
		_, err := s.db.Exec(`
			UPDATE sessions
			SET tasks_completed = tasks_completed + ?,
			    tasks_failed = tasks_failed + ?
			WHERE id = ?
		`, completed, failed, sessionID)
		return err
	})
}

// LifetimeTokens holds cumulative token and cost totals from all executions.
type LifetimeTokens struct {
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
	TotalCostUSD float64
}

// GetLifetimeTokens returns cumulative token usage and cost across all executions.
// Unlike session-scoped data, this survives restarts by querying the executions table directly.
// Rows with zero tokens (dispatcher queue rows, early-failure rows) are excluded so they
// don't dilute per-task averages.
func (s *Store) GetLifetimeTokens() (*LifetimeTokens, error) {
	row := s.db.QueryRow(`
		SELECT
			COALESCE(SUM(tokens_input), 0),
			COALESCE(SUM(tokens_output), 0),
			COALESCE(SUM(tokens_total), 0),
			COALESCE(SUM(estimated_cost_usd), 0)
		FROM executions
		WHERE tokens_total > 0
	`)

	var lt LifetimeTokens
	if err := row.Scan(&lt.InputTokens, &lt.OutputTokens, &lt.TotalTokens, &lt.TotalCostUSD); err != nil {
		return nil, fmt.Errorf("failed to get lifetime tokens: %w", err)
	}
	return &lt, nil
}

// LifetimeTaskCounts holds cumulative outcome counts from all executions.
// TASK-358: Failed counts genuine task failures only; non-failure terminal
// outcomes (no-op, stalled, declined, rate-limited, infra, skipped) are broken
// out separately so the dashboard does not inflate the failed count.
type LifetimeTaskCounts struct {
	Total       int
	Succeeded   int
	Failed      int
	Declined    int
	NoOp        int
	Stalled     int
	RateLimited int
	Infra       int
	Skipped     int
}

// NonFailure returns the total of all non-failure terminal outcomes (everything
// that is neither succeeded nor a genuine failure). TASK-358.
func (c LifetimeTaskCounts) NonFailure() int {
	return c.NoOp + c.Stalled + c.Declined + c.RateLimited + c.Infra + c.Skipped
}

// GetLifetimeTaskCounts returns cumulative task counts across all executions.
// Parallels GetLifetimeTokens — survives restarts by querying executions table directly.
func (s *Store) GetLifetimeTaskCounts() (*LifetimeTaskCounts, error) {
	row := s.db.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'declined' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'no_op' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'stalled' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'rate_limited' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'infra' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'skipped' THEN 1 ELSE 0 END), 0)
		FROM executions
	`)

	var tc LifetimeTaskCounts
	if err := row.Scan(&tc.Total, &tc.Succeeded, &tc.Failed, &tc.Declined, &tc.NoOp, &tc.Stalled,
		&tc.RateLimited, &tc.Infra, &tc.Skipped); err != nil {
		return nil, fmt.Errorf("failed to get lifetime task counts: %w", err)
	}
	return &tc, nil
}

// EndSession marks a session as ended.
func (s *Store) EndSession(sessionID string) error {
	return s.withRetry("EndSession", func() error {
		_, err := s.db.Exec(`
			UPDATE sessions SET ended_at = CURRENT_TIMESTAMP WHERE id = ?
		`, sessionID)
		return err
	})
}
