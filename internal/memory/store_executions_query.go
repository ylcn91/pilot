package memory

import (
	"database/sql"
	"fmt"
	"time"
)

// GetRecentExecutions returns the most recent executions ordered by creation time.
// The limit parameter specifies the maximum number of executions to return.
func (s *Store) GetRecentExecutions(limit int) ([]*Execution, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, project_path, status, output, error, duration_ms, pr_url, commit_sha, created_at, completed_at,
			COALESCE(task_title, ''), COALESCE(task_description, ''), COALESCE(task_branch, ''),
			COALESCE(task_base_branch, ''), COALESCE(task_create_pr, 0), COALESCE(task_verbose, 0),
			COALESCE(peak_rss_mb, 0), COALESCE(final_rss_mb, 0)
		FROM executions ORDER BY created_at DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var executions []*Execution
	for rows.Next() {
		var exec Execution
		var completedAt sql.NullTime
		if err := rows.Scan(&exec.ID, &exec.TaskID, &exec.ProjectPath, &exec.Status, &exec.Output, &exec.Error, &exec.DurationMs, &exec.PRUrl, &exec.CommitSHA, &exec.CreatedAt, &completedAt,
			&exec.TaskTitle, &exec.TaskDescription, &exec.TaskBranch, &exec.TaskBaseBranch, &exec.TaskCreatePR, &exec.TaskVerbose,
			&exec.PeakRSSMB, &exec.FinalRSSMB); err != nil {
			return nil, err
		}
		if completedAt.Valid {
			exec.CompletedAt = &completedAt.Time
		}
		executions = append(executions, &exec)
	}

	return executions, rows.Err()
}

// BriefQuery holds parameters for querying execution data within a time period.
// Used for generating daily briefs and reports.
type BriefQuery struct {
	Start    time.Time
	End      time.Time
	Projects []string // Empty = all projects
}

// GetExecutionsInPeriod retrieves executions within the specified time range.
// If query.Projects is non-empty, results are filtered to those projects only.
func (s *Store) GetExecutionsInPeriod(query BriefQuery) ([]*Execution, error) {
	var rows *sql.Rows
	var err error

	if len(query.Projects) > 0 {
		// Build placeholders for IN clause
		placeholders := ""
		args := make([]interface{}, 0, len(query.Projects)+2)
		args = append(args, query.Start, query.End)
		for i, p := range query.Projects {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, p)
		}
		rows, err = s.db.Query(`
			SELECT id, task_id, project_path, status, output, error, duration_ms, pr_url, commit_sha, created_at, completed_at, COALESCE(task_title, '')
			FROM executions
			WHERE created_at >= ? AND created_at < ?
			AND project_path IN (`+placeholders+`)
			ORDER BY created_at DESC
		`, args...)
	} else {
		rows, err = s.db.Query(`
			SELECT id, task_id, project_path, status, output, error, duration_ms, pr_url, commit_sha, created_at, completed_at, COALESCE(task_title, '')
			FROM executions
			WHERE created_at >= ? AND created_at < ?
			ORDER BY created_at DESC
		`, query.Start, query.End)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var executions []*Execution
	for rows.Next() {
		var exec Execution
		var completedAt sql.NullTime
		if err := rows.Scan(&exec.ID, &exec.TaskID, &exec.ProjectPath, &exec.Status, &exec.Output, &exec.Error, &exec.DurationMs, &exec.PRUrl, &exec.CommitSHA, &exec.CreatedAt, &completedAt, &exec.TaskTitle); err != nil {
			return nil, err
		}
		if completedAt.Valid {
			exec.CompletedAt = &completedAt.Time
		}
		executions = append(executions, &exec)
	}

	return executions, rows.Err()
}

// GetActiveExecutions retrieves all executions with status "running".
func (s *Store) GetActiveExecutions() ([]*Execution, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, project_path, status, output, error, duration_ms, pr_url, commit_sha, created_at, completed_at
		FROM executions
		WHERE status = 'running'
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var executions []*Execution
	for rows.Next() {
		var exec Execution
		var completedAt sql.NullTime
		if err := rows.Scan(&exec.ID, &exec.TaskID, &exec.ProjectPath, &exec.Status, &exec.Output, &exec.Error, &exec.DurationMs, &exec.PRUrl, &exec.CommitSHA, &exec.CreatedAt, &completedAt); err != nil {
			return nil, err
		}
		if completedAt.Valid {
			exec.CompletedAt = &completedAt.Time
		}
		executions = append(executions, &exec)
	}

	return executions, rows.Err()
}

// GetBriefMetrics calculates aggregate metrics for a time period including
// task counts, success rates, average duration, and PR creation statistics.
func (s *Store) GetBriefMetrics(query BriefQuery) (*BriefMetricsData, error) {
	var result BriefMetricsData

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

	// Get counts and averages
	row := s.db.QueryRow(`
		SELECT
			COUNT(*) as total,
			COALESCE(SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END), 0) as completed,
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0) as failed,
			CAST(COALESCE(AVG(CASE WHEN status = 'completed' THEN duration_ms END), 0) AS INTEGER) as avg_duration,
			COALESCE(SUM(CASE WHEN pr_url != '' THEN 1 ELSE 0 END), 0) as prs_created,
			COALESCE(SUM(tokens_total), 0) as total_tokens,
			COALESCE(SUM(estimated_cost_usd), 0) as total_cost
		FROM executions
	`+whereClause, args...)

	if err := row.Scan(&result.TotalTasks, &result.CompletedCount, &result.FailedCount, &result.AvgDurationMs, &result.PRsCreated, &result.TotalTokensUsed, &result.EstimatedCostUSD); err != nil {
		return nil, fmt.Errorf("failed to get metrics: %w", err)
	}

	if result.TotalTasks > 0 {
		result.SuccessRate = float64(result.CompletedCount) / float64(result.TotalTasks)
	}

	return &result, nil
}

// BriefMetricsData holds aggregate metrics calculated from execution data.
type BriefMetricsData struct {
	TotalTasks       int
	CompletedCount   int
	FailedCount      int
	SuccessRate      float64
	AvgDurationMs    int64
	PRsCreated       int
	TotalTokensUsed  int64
	EstimatedCostUSD float64
}

// GetQueuedTasks returns tasks with status "queued" or "pending" waiting to be executed.
// Results are ordered by creation time ascending (oldest first) up to the specified limit.
func (s *Store) GetQueuedTasks(limit int) ([]*Execution, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, project_path, status, output, error, duration_ms, pr_url, commit_sha, created_at, completed_at
		FROM executions
		WHERE status = 'queued' OR status = 'pending'
		ORDER BY created_at ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var executions []*Execution
	for rows.Next() {
		var exec Execution
		var completedAt sql.NullTime
		if err := rows.Scan(&exec.ID, &exec.TaskID, &exec.ProjectPath, &exec.Status, &exec.Output, &exec.Error, &exec.DurationMs, &exec.PRUrl, &exec.CommitSHA, &exec.CreatedAt, &completedAt); err != nil {
			return nil, err
		}
		if completedAt.Valid {
			exec.CompletedAt = &completedAt.Time
		}
		executions = append(executions, &exec)
	}

	return executions, rows.Err()
}

// GetQueuedTasksForProject returns queued/pending tasks for a specific project.
// Results are ordered by creation time ascending (oldest first) up to the specified limit.
// This is used by the per-project worker to get the next task to execute.
func (s *Store) GetQueuedTasksForProject(projectPath string, limit int) ([]*Execution, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, project_path, status, output, error, duration_ms, pr_url, commit_sha, created_at, completed_at,
			COALESCE(task_title, ''), COALESCE(task_description, ''), COALESCE(task_branch, ''),
			COALESCE(task_base_branch, ''), COALESCE(task_create_pr, 0), COALESCE(task_verbose, 0),
			COALESCE(task_source_adapter, ''), COALESCE(task_source_issue_id, ''),
			COALESCE(task_labels, ''), COALESCE(task_state, '')
		FROM executions
		WHERE (status = 'queued' OR status = 'pending') AND project_path = ?
		ORDER BY created_at ASC
		LIMIT ?
	`, projectPath, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var executions []*Execution
	for rows.Next() {
		var exec Execution
		var completedAt sql.NullTime
		var labelsJSON string
		if err := rows.Scan(&exec.ID, &exec.TaskID, &exec.ProjectPath, &exec.Status, &exec.Output, &exec.Error, &exec.DurationMs, &exec.PRUrl, &exec.CommitSHA, &exec.CreatedAt, &completedAt,
			&exec.TaskTitle, &exec.TaskDescription, &exec.TaskBranch, &exec.TaskBaseBranch, &exec.TaskCreatePR, &exec.TaskVerbose,
			&exec.TaskSourceAdapter, &exec.TaskSourceIssueID, &labelsJSON, &exec.TaskState); err != nil {
			return nil, err
		}
		if completedAt.Valid {
			exec.CompletedAt = &completedAt.Time
		}
		exec.TaskLabels = unmarshalLabels(labelsJSON)
		executions = append(executions, &exec)
	}

	return executions, rows.Err()
}

// GetStaleRunningExecutions returns executions that have been in "running" status
// for longer than the specified duration. Used to detect crashed workers on restart.
func (s *Store) GetStaleRunningExecutions(staleDuration time.Duration) ([]*Execution, error) {
	staleTime := time.Now().Add(-staleDuration)
	rows, err := s.db.Query(`
		SELECT id, task_id, project_path, status, output, error, duration_ms, pr_url, commit_sha, created_at, completed_at
		FROM executions
		WHERE status = 'running' AND created_at < ?
		ORDER BY created_at ASC
	`, staleTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var executions []*Execution
	for rows.Next() {
		var exec Execution
		var completedAt sql.NullTime
		if err := rows.Scan(&exec.ID, &exec.TaskID, &exec.ProjectPath, &exec.Status, &exec.Output, &exec.Error, &exec.DurationMs, &exec.PRUrl, &exec.CommitSHA, &exec.CreatedAt, &completedAt); err != nil {
			return nil, err
		}
		if completedAt.Valid {
			exec.CompletedAt = &completedAt.Time
		}
		executions = append(executions, &exec)
	}

	return executions, rows.Err()
}

// GetStaleQueuedExecutions returns executions that have been in "queued" status
// for longer than the specified duration. Used to detect stuck queue entries.
func (s *Store) GetStaleQueuedExecutions(staleDuration time.Duration) ([]*Execution, error) {
	staleTime := time.Now().Add(-staleDuration)
	rows, err := s.db.Query(`
		SELECT id, task_id, project_path, status, output, error, duration_ms, pr_url, commit_sha, created_at, completed_at
		FROM executions
		WHERE status = 'queued' AND created_at < ?
		ORDER BY created_at ASC
	`, staleTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var executions []*Execution
	for rows.Next() {
		var exec Execution
		var completedAt sql.NullTime
		if err := rows.Scan(&exec.ID, &exec.TaskID, &exec.ProjectPath, &exec.Status, &exec.Output, &exec.Error, &exec.DurationMs, &exec.PRUrl, &exec.CommitSHA, &exec.CreatedAt, &completedAt); err != nil {
			return nil, err
		}
		if completedAt.Valid {
			exec.CompletedAt = &completedAt.Time
		}
		executions = append(executions, &exec)
	}

	return executions, rows.Err()
}
