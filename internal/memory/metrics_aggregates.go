package memory

import (
	"fmt"
	"time"
)

// TelemetryGapStats summarises how many recent completed executions reported
// zero token usage, used by the startup health check (GH-2428). A high ratio
// signals the configured backend's usage events aren't being parsed — cost
// reporting and per-task budgets silently misbehave when this happens.
type TelemetryGapStats struct {
	CompletedRuns int // Completed runs inspected (with non-empty commit_sha)
	ZeroTokenRuns int // Subset where tokens_total = 0
}

// RecentCompletedTelemetryStats counts how many of the last `limit` completed
// executions with a real commit reported tokens_total = 0. Excludes epic
// orchestrator rows (no commit_sha) so we measure backend telemetry, not
// the parent-task path that legitimately has no tokens. GH-2428.
func (s *Store) RecentCompletedTelemetryStats(limit int) (*TelemetryGapStats, error) {
	if limit <= 0 {
		limit = 50
	}
	row := s.db.QueryRow(`
		SELECT
			COUNT(*) as total,
			COALESCE(SUM(CASE WHEN tokens_total = 0 THEN 1 ELSE 0 END), 0) as zero_tokens
		FROM (
			SELECT tokens_total
			FROM executions
			WHERE status = 'completed'
			  AND commit_sha != ''
			  AND commit_sha IS NOT NULL
			ORDER BY created_at DESC
			LIMIT ?
		)
	`, limit)
	stats := &TelemetryGapStats{}
	if err := row.Scan(&stats.CompletedRuns, &stats.ZeroTokenRuns); err != nil {
		return nil, fmt.Errorf("failed to scan telemetry gap stats: %w", err)
	}
	return stats, nil
}

// SaveExecutionMetrics saves metrics for an execution
func (s *Store) SaveExecutionMetrics(metrics *ExecutionMetrics) error {
	return s.withRetry("SaveExecutionMetrics", func() error {
		_, err := s.db.Exec(`
			UPDATE executions SET
				tokens_input = ?,
				tokens_output = ?,
				tokens_total = ?,
				estimated_cost_usd = ?,
				files_changed = ?,
				lines_added = ?,
				lines_removed = ?,
				model_name = ?,
				peak_rss_mb = ?,
				final_rss_mb = ?
			WHERE id = ?
		`, metrics.TokensInput, metrics.TokensOutput, metrics.TokensTotal,
			metrics.EstimatedCostUSD, metrics.FilesChanged, metrics.LinesAdded,
			metrics.LinesRemoved, metrics.ModelName,
			metrics.PeakRSSMB, metrics.FinalRSSMB,
			metrics.ExecutionID)
		return err
	})
}

// GetMetricsSummary returns aggregated metrics for a time period
func (s *Store) GetMetricsSummary(query MetricsQuery) (*MetricsSummary, error) {
	summary := &MetricsSummary{
		PeriodStart: query.Start,
		PeriodEnd:   query.End,
	}

	var args []interface{}
	whereClause := "WHERE created_at >= ? AND created_at < ?"
	args = append(args, query.Start, query.End)

	if len(query.Projects) > 0 {
		placeholders := ""
		for i, p := range query.Projects {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, p)
		}
		whereClause += " AND project_path IN (" + placeholders + ")"
	}

	// Get aggregated metrics
	row := s.db.QueryRow(`
		SELECT
			COUNT(*) as total,
			COALESCE(SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END), 0) as completed,
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0) as failed,
			COALESCE(SUM(duration_ms), 0) as total_duration,
			CAST(COALESCE(AVG(CASE WHEN status = 'completed' THEN duration_ms END), 0) AS INTEGER) as avg_duration,
			COALESCE(MIN(CASE WHEN status = 'completed' THEN duration_ms END), 0) as min_duration,
			COALESCE(MAX(CASE WHEN status = 'completed' THEN duration_ms END), 0) as max_duration,
			COALESCE(SUM(tokens_input), 0) as total_input,
			COALESCE(SUM(tokens_output), 0) as total_output,
			COALESCE(SUM(tokens_total), 0) as total_tokens,
			COALESCE(SUM(estimated_cost_usd), 0) as total_cost,
			COALESCE(SUM(files_changed), 0) as files_changed,
			COALESCE(SUM(lines_added), 0) as lines_added,
			COALESCE(SUM(lines_removed), 0) as lines_removed,
			COALESCE(SUM(CASE WHEN pr_url != '' AND pr_url IS NOT NULL THEN 1 ELSE 0 END), 0) as prs
		FROM executions
	`+whereClause, args...)

	err := row.Scan(
		&summary.TotalExecutions,
		&summary.SuccessCount,
		&summary.FailedCount,
		&summary.TotalDurationMs,
		&summary.AvgDurationMs,
		&summary.MinDurationMs,
		&summary.MaxDurationMs,
		&summary.TotalTokensInput,
		&summary.TotalTokensOutput,
		&summary.TotalTokens,
		&summary.TotalCostUSD,
		&summary.TotalFilesChanged,
		&summary.TotalLinesAdded,
		&summary.TotalLinesRemoved,
		&summary.PRsCreated,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics summary: %w", err)
	}

	// Calculate derived metrics
	if summary.TotalExecutions > 0 {
		summary.SuccessRate = float64(summary.SuccessCount) / float64(summary.TotalExecutions)
		summary.AvgTokensPerTask = summary.TotalTokens / int64(summary.TotalExecutions)
		summary.AvgCostUSD = summary.TotalCostUSD / float64(summary.TotalExecutions)
	}

	return summary, nil
}

// GetDailyMetrics returns metrics aggregated by day
func (s *Store) GetDailyMetrics(query MetricsQuery) ([]*DailyMetrics, error) {
	var args []interface{}
	whereClause := "WHERE created_at >= ? AND created_at < ? AND tokens_total > 0"
	args = append(args, query.Start, query.End)

	if len(query.Projects) > 0 {
		placeholders := ""
		for i, p := range query.Projects {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, p)
		}
		whereClause += " AND project_path IN (" + placeholders + ")"
	}

	rows, err := s.db.Query(`
		SELECT
			date(created_at) as day,
			COUNT(*) as total,
			COALESCE(SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END), 0) as completed,
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0) as failed,
			COALESCE(SUM(duration_ms), 0) as total_duration,
			COALESCE(SUM(tokens_total), 0) as total_tokens,
			COALESCE(SUM(estimated_cost_usd), 0) as total_cost,
			COALESCE(SUM(files_changed), 0) as files_changed,
			COALESCE(SUM(lines_added), 0) as lines_added,
			COALESCE(SUM(lines_removed), 0) as lines_removed,
			COALESCE(SUM(CASE WHEN pr_url != '' AND pr_url IS NOT NULL THEN 1 ELSE 0 END), 0) as prs
		FROM executions
		`+whereClause+`
		GROUP BY date(created_at)
		ORDER BY day DESC
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get daily metrics: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var metrics []*DailyMetrics
	for rows.Next() {
		var m DailyMetrics
		var dateStr string
		if err := rows.Scan(
			&dateStr,
			&m.ExecutionCount,
			&m.SuccessCount,
			&m.FailedCount,
			&m.TotalDurationMs,
			&m.TotalTokens,
			&m.TotalCostUSD,
			&m.FilesChanged,
			&m.LinesAdded,
			&m.LinesRemoved,
			&m.PRsCreated,
		); err != nil {
			return nil, err
		}
		m.Date, _ = time.Parse("2006-01-02", dateStr)
		metrics = append(metrics, &m)
	}

	return metrics, rows.Err()
}

// GetProjectMetrics returns metrics aggregated by project
func (s *Store) GetProjectMetrics(query MetricsQuery) ([]*ProjectMetrics, error) {
	var args []interface{}
	whereClause := "WHERE e.created_at >= ? AND e.created_at < ?"
	args = append(args, query.Start, query.End)

	if len(query.Projects) > 0 {
		placeholders := ""
		for i, p := range query.Projects {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, p)
		}
		whereClause += " AND e.project_path IN (" + placeholders + ")"
	}

	rows, err := s.db.Query(`
		SELECT
			e.project_path,
			COALESCE(p.name, e.project_path) as project_name,
			COUNT(*) as total,
			COALESCE(SUM(CASE WHEN e.status = 'completed' THEN 1 ELSE 0 END), 0) as completed,
			COALESCE(SUM(CASE WHEN e.status = 'failed' THEN 1 ELSE 0 END), 0) as failed,
			COALESCE(SUM(e.duration_ms), 0) as total_duration,
			COALESCE(SUM(e.tokens_total), 0) as total_tokens,
			COALESCE(SUM(e.estimated_cost_usd), 0) as total_cost,
			MAX(e.created_at) as last_exec
		FROM executions e
		LEFT JOIN projects p ON e.project_path = p.path
		`+whereClause+`
		GROUP BY e.project_path
		ORDER BY total DESC
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get project metrics: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var metrics []*ProjectMetrics
	for rows.Next() {
		var m ProjectMetrics
		if err := rows.Scan(
			&m.ProjectPath,
			&m.ProjectName,
			&m.ExecutionCount,
			&m.SuccessCount,
			&m.FailedCount,
			&m.TotalDurationMs,
			&m.TotalTokens,
			&m.TotalCostUSD,
			&m.LastExecution,
		); err != nil {
			return nil, err
		}
		if m.ExecutionCount > 0 {
			m.SuccessRate = float64(m.SuccessCount) / float64(m.ExecutionCount)
		}
		metrics = append(metrics, &m)
	}

	return metrics, rows.Err()
}
