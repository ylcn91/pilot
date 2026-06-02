package memory

import (
	"database/sql"
	"fmt"
)

// GetFailureReasons returns breakdown of failure reasons
func (s *Store) GetFailureReasons(query MetricsQuery, limit int) ([]*FailureReason, error) {
	var args []interface{}
	whereClause := "WHERE created_at >= ? AND created_at < ? AND status = 'failed' AND error != ''"
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

	args = append(args, limit)

	// Group by first line of error (usually the main error message)
	rows, err := s.db.Query(`
		SELECT
			SUBSTR(error, 1, INSTR(error || char(10), char(10)) - 1) as reason,
			COUNT(*) as count
		FROM executions
		`+whereClause+`
		GROUP BY reason
		ORDER BY count DESC
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get failure reasons: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var reasons []*FailureReason
	for rows.Next() {
		var r FailureReason
		if err := rows.Scan(&r.Reason, &r.Count); err != nil {
			return nil, err
		}
		reasons = append(reasons, &r)
	}

	return reasons, rows.Err()
}

// GetPeakUsageHours returns execution counts by hour of day
func (s *Store) GetPeakUsageHours(query MetricsQuery) (map[int]int, error) {
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

	rows, err := s.db.Query(`
		SELECT
			CAST(strftime('%H', created_at) AS INTEGER) as hour,
			COUNT(*) as count
		FROM executions
		`+whereClause+`
		GROUP BY hour
		ORDER BY hour
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get peak usage hours: %w", err)
	}
	defer func() { _ = rows.Close() }()

	hours := make(map[int]int)
	for rows.Next() {
		var hour, count int
		if err := rows.Scan(&hour, &count); err != nil {
			return nil, err
		}
		hours[hour] = count
	}

	return hours, rows.Err()
}

// ExportMetrics exports execution data for external analytics
func (s *Store) ExportMetrics(query MetricsQuery) ([]*ExportedExecution, error) {
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

	rows, err := s.db.Query(`
		SELECT
			id, task_id, project_path, status, duration_ms,
			tokens_input, tokens_output, tokens_total, estimated_cost_usd,
			files_changed, lines_added, lines_removed, model_name,
			pr_url, commit_sha, created_at, completed_at
		FROM executions
		`+whereClause+`
		ORDER BY created_at DESC
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to export metrics: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var exports []*ExportedExecution
	for rows.Next() {
		var e ExportedExecution
		var completedAt sql.NullTime
		var tokensInput, tokensOutput, tokensTotal sql.NullInt64
		var cost sql.NullFloat64
		var filesChanged, linesAdded, linesRemoved sql.NullInt64
		var modelName, prURL, commitSHA sql.NullString

		if err := rows.Scan(
			&e.ID, &e.TaskID, &e.ProjectPath, &e.Status, &e.DurationMs,
			&tokensInput, &tokensOutput, &tokensTotal, &cost,
			&filesChanged, &linesAdded, &linesRemoved, &modelName,
			&prURL, &commitSHA, &e.CreatedAt, &completedAt,
		); err != nil {
			return nil, err
		}

		if tokensInput.Valid {
			e.TokensInput = tokensInput.Int64
		}
		if tokensOutput.Valid {
			e.TokensOutput = tokensOutput.Int64
		}
		if tokensTotal.Valid {
			e.TokensTotal = tokensTotal.Int64
		}
		if cost.Valid {
			e.EstimatedCostUSD = cost.Float64
		}
		if filesChanged.Valid {
			e.FilesChanged = int(filesChanged.Int64)
		}
		if linesAdded.Valid {
			e.LinesAdded = int(linesAdded.Int64)
		}
		if linesRemoved.Valid {
			e.LinesRemoved = int(linesRemoved.Int64)
		}
		if modelName.Valid {
			e.ModelName = modelName.String
		}
		if prURL.Valid {
			e.PRUrl = prURL.String
		}
		if commitSHA.Valid {
			e.CommitSHA = commitSHA.String
		}
		if completedAt.Valid {
			e.CompletedAt = &completedAt.Time
		}

		exports = append(exports, &e)
	}

	return exports, rows.Err()
}
