package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// AutopilotMetricsRow represents a persisted autopilot metrics snapshot.
type AutopilotMetricsRow struct {
	ID                  int64
	SnapshotAt          time.Time
	IssuesSuccess       int
	IssuesFailed        int
	IssuesRateLimited   int
	PRsMerged           int
	PRsFailed           int
	PRsConflicting      int
	CircuitBreakerTrips int
	APIErrorsTotal      int
	APIErrorRate        float64
	QueueDepth          int
	FailedQueueDepth    int
	ActivePRs           int
	SuccessRate         float64
	AvgCIWaitMs         int64
	AvgMergeTimeMs      int64
	AvgExecutionMs      int64
	// Per-model/direction counters added in GH-2856. Keys use "model|direction"
	// (TokensConsumed, ExecutionsByResult) or plain model string (ExecutionCostUSD).
	TokensConsumed     map[string]int64   // "model|direction" → token count
	ExecutionCostUSD   map[string]float64 // model → cumulative USD cost
	ExecutionsByResult map[string]int64   // "model|result" → execution count
}

// SaveAutopilotMetrics persists an autopilot metrics snapshot to SQLite.
func (s *Store) SaveAutopilotMetrics(row *AutopilotMetricsRow) error {
	tokensJSON := marshalMapJSON(row.TokensConsumed)
	costJSON := marshalMapJSON(row.ExecutionCostUSD)
	execsJSON := marshalMapJSON(row.ExecutionsByResult)

	return s.withRetry("SaveAutopilotMetrics", func() error {
		_, err := s.db.Exec(`
			INSERT INTO autopilot_metrics (
				snapshot_at, issues_success, issues_failed, issues_rate_limited,
				prs_merged, prs_failed, prs_conflicting, circuit_breaker_trips,
				api_errors_total, api_error_rate, queue_depth, failed_queue_depth,
				active_prs, success_rate, avg_ci_wait_ms, avg_merge_time_ms, avg_execution_ms,
				tokens_consumed_json, execution_cost_usd_json, executions_by_result_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			row.SnapshotAt,
			row.IssuesSuccess, row.IssuesFailed, row.IssuesRateLimited,
			row.PRsMerged, row.PRsFailed, row.PRsConflicting,
			row.CircuitBreakerTrips, row.APIErrorsTotal, row.APIErrorRate,
			row.QueueDepth, row.FailedQueueDepth, row.ActivePRs,
			row.SuccessRate, row.AvgCIWaitMs, row.AvgMergeTimeMs, row.AvgExecutionMs,
			tokensJSON, costJSON, execsJSON,
		)
		return err
	})
}

// GetRecentAutopilotMetrics returns the most recent metrics snapshots.
func (s *Store) GetRecentAutopilotMetrics(limit int) ([]*AutopilotMetricsRow, error) {
	rows, err := s.db.Query(`
		SELECT id, snapshot_at, issues_success, issues_failed, issues_rate_limited,
			prs_merged, prs_failed, prs_conflicting, circuit_breaker_trips,
			api_errors_total, api_error_rate, queue_depth, failed_queue_depth,
			active_prs, success_rate, avg_ci_wait_ms, avg_merge_time_ms, avg_execution_ms,
			tokens_consumed_json, execution_cost_usd_json, executions_by_result_json
		FROM autopilot_metrics
		ORDER BY snapshot_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query autopilot metrics: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []*AutopilotMetricsRow
	for rows.Next() {
		r := &AutopilotMetricsRow{}
		var tokensJSON, costJSON, execsJSON sql.NullString
		if err := rows.Scan(
			&r.ID, &r.SnapshotAt, &r.IssuesSuccess, &r.IssuesFailed, &r.IssuesRateLimited,
			&r.PRsMerged, &r.PRsFailed, &r.PRsConflicting, &r.CircuitBreakerTrips,
			&r.APIErrorsTotal, &r.APIErrorRate, &r.QueueDepth, &r.FailedQueueDepth,
			&r.ActivePRs, &r.SuccessRate, &r.AvgCIWaitMs, &r.AvgMergeTimeMs, &r.AvgExecutionMs,
			&tokensJSON, &costJSON, &execsJSON,
		); err != nil {
			return nil, fmt.Errorf("failed to scan autopilot metrics: %w", err)
		}
		r.TokensConsumed = unmarshalStringIntMap(tokensJSON.String)
		r.ExecutionCostUSD = unmarshalStringFloatMap(costJSON.String)
		r.ExecutionsByResult = unmarshalStringIntMap(execsJSON.String)
		result = append(result, r)
	}
	return result, rows.Err()
}

// LatestAutopilotMetrics returns the most recent persisted snapshot, or (nil, nil) if none.
func (s *Store) LatestAutopilotMetrics() (*AutopilotMetricsRow, error) {
	row := s.db.QueryRow(`
		SELECT id, snapshot_at, issues_success, issues_failed, issues_rate_limited,
			prs_merged, prs_failed, prs_conflicting, circuit_breaker_trips,
			api_errors_total, api_error_rate, queue_depth, failed_queue_depth,
			active_prs, success_rate, avg_ci_wait_ms, avg_merge_time_ms, avg_execution_ms,
			tokens_consumed_json, execution_cost_usd_json, executions_by_result_json
		FROM autopilot_metrics
		ORDER BY snapshot_at DESC
		LIMIT 1
	`)
	r := &AutopilotMetricsRow{}
	var tokensJSON, costJSON, execsJSON sql.NullString
	err := row.Scan(
		&r.ID, &r.SnapshotAt, &r.IssuesSuccess, &r.IssuesFailed, &r.IssuesRateLimited,
		&r.PRsMerged, &r.PRsFailed, &r.PRsConflicting, &r.CircuitBreakerTrips,
		&r.APIErrorsTotal, &r.APIErrorRate, &r.QueueDepth, &r.FailedQueueDepth,
		&r.ActivePRs, &r.SuccessRate, &r.AvgCIWaitMs, &r.AvgMergeTimeMs, &r.AvgExecutionMs,
		&tokensJSON, &costJSON, &execsJSON,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan latest autopilot metrics: %w", err)
	}
	r.TokensConsumed = unmarshalStringIntMap(tokensJSON.String)
	r.ExecutionCostUSD = unmarshalStringFloatMap(costJSON.String)
	r.ExecutionsByResult = unmarshalStringIntMap(execsJSON.String)
	return r, nil
}

// PruneAutopilotMetrics deletes snapshots older than the given duration.
func (s *Store) PruneAutopilotMetrics(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	var result sql.Result
	err := s.withRetry("PruneAutopilotMetrics", func() error {
		var execErr error
		result, execErr = s.db.Exec(`DELETE FROM autopilot_metrics WHERE snapshot_at < ?`, cutoff)
		return execErr
	})
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// marshalMapJSON serializes any JSON-serializable value to a string.
// Returns "{}" on nil input, nil map, or marshal error (safe default for DB storage).
func marshalMapJSON(v any) string {
	if v == nil {
		return "{}"
	}
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return "{}"
	}
	return string(b)
}

// unmarshalStringIntMap deserializes a JSON string into map[string]int64.
// Returns an empty map on empty or invalid JSON.
func unmarshalStringIntMap(s string) map[string]int64 {
	m := make(map[string]int64)
	if s == "" || s == "{}" {
		return m
	}
	_ = json.Unmarshal([]byte(s), &m)
	return m
}

// unmarshalStringFloatMap deserializes a JSON string into map[string]float64.
// Returns an empty map on empty or invalid JSON.
func unmarshalStringFloatMap(s string) map[string]float64 {
	m := make(map[string]float64)
	if s == "" || s == "{}" {
		return m
	}
	_ = json.Unmarshal([]byte(s), &m)
	return m
}
