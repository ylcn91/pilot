package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// GetUsageSummary returns aggregated usage for a period
func (s *Store) GetUsageSummary(query UsageQuery) (*UsageSummary, error) {
	summary := &UsageSummary{
		UserID:      query.UserID,
		ProjectID:   query.ProjectID,
		PeriodStart: query.Start,
		PeriodEnd:   query.End,
	}

	// Build WHERE clause
	var args []interface{}
	whereClause := "WHERE timestamp >= ? AND timestamp < ?"
	args = append(args, query.Start, query.End)

	if query.UserID != "" {
		whereClause += " AND user_id = ?"
		args = append(args, query.UserID)
	}

	if query.ProjectID != "" {
		whereClause += " AND project_id = ?"
		args = append(args, query.ProjectID)
	}

	// Get task metrics
	row := s.db.QueryRow(`
		SELECT COALESCE(COUNT(*), 0), COALESCE(SUM(total_cost), 0)
		FROM usage_events
		`+whereClause+` AND event_type = 'task'
	`, args...)
	if err := row.Scan(&summary.TaskCount, &summary.TaskCost); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get task metrics: %w", err)
	}

	// Get token metrics
	row = s.db.QueryRow(`
		SELECT
			COALESCE(SUM(CAST(json_extract(metadata, '$.input_tokens') AS INTEGER)), 0),
			COALESCE(SUM(CAST(json_extract(metadata, '$.output_tokens') AS INTEGER)), 0),
			COALESCE(SUM(quantity), 0),
			COALESCE(SUM(total_cost), 0)
		FROM usage_events
		`+whereClause+` AND event_type = 'token'
	`, args...)
	if err := row.Scan(&summary.TokensInput, &summary.TokensOutput, &summary.TokensTotal, &summary.TokenCost); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get token metrics: %w", err)
	}

	// Get compute metrics
	row = s.db.QueryRow(`
		SELECT COALESCE(SUM(quantity), 0), COALESCE(SUM(total_cost), 0)
		FROM usage_events
		`+whereClause+` AND event_type = 'compute'
	`, args...)
	if err := row.Scan(&summary.ComputeMinutes, &summary.ComputeCost); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get compute metrics: %w", err)
	}

	// Get storage metrics
	row = s.db.QueryRow(`
		SELECT COALESCE(SUM(quantity), 0), COALESCE(SUM(total_cost), 0)
		FROM usage_events
		`+whereClause+` AND event_type = 'storage'
	`, args...)
	if err := row.Scan(&summary.StorageBytes, &summary.StorageCost); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get storage metrics: %w", err)
	}

	// Get API call metrics
	row = s.db.QueryRow(`
		SELECT COALESCE(COUNT(*), 0), COALESCE(SUM(total_cost), 0)
		FROM usage_events
		`+whereClause+` AND event_type = 'api_call'
	`, args...)
	if err := row.Scan(&summary.APICallCount, &summary.APICallCost); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get API call metrics: %w", err)
	}

	// Calculate total
	summary.TotalCost = summary.TaskCost + summary.TokenCost + summary.ComputeCost + summary.StorageCost + summary.APICallCost

	return summary, nil
}

// GetDailyUsage returns usage aggregated by day
func (s *Store) GetDailyUsage(query UsageQuery) ([]*DailyUsage, error) {
	var args []interface{}
	whereClause := "WHERE timestamp >= ? AND timestamp < ?"
	args = append(args, query.Start, query.End)

	if query.UserID != "" {
		whereClause += " AND user_id = ?"
		args = append(args, query.UserID)
	}

	if query.ProjectID != "" {
		whereClause += " AND project_id = ?"
		args = append(args, query.ProjectID)
	}

	rows, err := s.db.Query(`
		SELECT
			substr(timestamp, 1, 10) as day,
			event_type,
			COUNT(*) as event_count,
			COALESCE(SUM(quantity), 0) as total_quantity,
			COALESCE(SUM(total_cost), 0) as total_cost
		FROM usage_events
		`+whereClause+`
		GROUP BY substr(timestamp, 1, 10), event_type
		ORDER BY day DESC, event_type
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get daily usage: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// Aggregate by day
	dailyMap := make(map[string]*DailyUsage)
	for rows.Next() {
		var dateStr string
		var eventType string
		var count, quantity int64
		var cost float64

		if err := rows.Scan(&dateStr, &eventType, &count, &quantity, &cost); err != nil {
			return nil, err
		}

		if _, ok := dailyMap[dateStr]; !ok {
			date, _ := time.Parse("2006-01-02", dateStr)
			dailyMap[dateStr] = &DailyUsage{Date: date}
		}

		du := dailyMap[dateStr]
		du.TotalCost += cost

		switch UsageEventType(eventType) {
		case EventTypeTask:
			du.TaskCount = count
			du.TaskCost = cost
		case EventTypeToken:
			du.TokenCount = quantity
			du.TokenCost = cost
		case EventTypeCompute:
			du.ComputeMinutes = quantity
			du.ComputeCost = cost
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Convert to slice
	var result []*DailyUsage
	for _, du := range dailyMap {
		result = append(result, du)
	}

	return result, nil
}

// DailyUsage represents usage for a single day
type DailyUsage struct {
	Date           time.Time `json:"date"`
	TaskCount      int64     `json:"task_count"`
	TaskCost       float64   `json:"task_cost"`
	TokenCount     int64     `json:"token_count"`
	TokenCost      float64   `json:"token_cost"`
	ComputeMinutes int64     `json:"compute_minutes"`
	ComputeCost    float64   `json:"compute_cost"`
	TotalCost      float64   `json:"total_cost"`
}

// GetUsageByProject returns usage aggregated by project
func (s *Store) GetUsageByProject(query UsageQuery) ([]*ProjectUsage, error) {
	var args []interface{}
	whereClause := "WHERE timestamp >= ? AND timestamp < ?"
	args = append(args, query.Start, query.End)

	if query.UserID != "" {
		whereClause += " AND user_id = ?"
		args = append(args, query.UserID)
	}

	rows, err := s.db.Query(`
		SELECT
			project_id,
			COUNT(DISTINCT CASE WHEN event_type = 'task' THEN id END) as task_count,
			COALESCE(SUM(CASE WHEN event_type = 'token' THEN quantity ELSE 0 END), 0) as tokens,
			COALESCE(SUM(CASE WHEN event_type = 'compute' THEN quantity ELSE 0 END), 0) as compute_mins,
			COALESCE(SUM(total_cost), 0) as total_cost
		FROM usage_events
		`+whereClause+`
		GROUP BY project_id
		ORDER BY total_cost DESC
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get project usage: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []*ProjectUsage
	for rows.Next() {
		var pu ProjectUsage
		if err := rows.Scan(&pu.ProjectID, &pu.TaskCount, &pu.TokenCount, &pu.ComputeMinutes, &pu.TotalCost); err != nil {
			return nil, err
		}
		result = append(result, &pu)
	}

	return result, rows.Err()
}

// ProjectUsage represents usage for a single project
type ProjectUsage struct {
	ProjectID      string  `json:"project_id"`
	TaskCount      int64   `json:"task_count"`
	TokenCount     int64   `json:"token_count"`
	ComputeMinutes int64   `json:"compute_minutes"`
	TotalCost      float64 `json:"total_cost"`
}

// GetUsageEvents returns raw usage events for export/audit
func (s *Store) GetUsageEvents(query UsageQuery, limit int) ([]*UsageEvent, error) {
	var args []interface{}
	whereClause := "WHERE timestamp >= ? AND timestamp < ?"
	args = append(args, query.Start, query.End)

	if query.UserID != "" {
		whereClause += " AND user_id = ?"
		args = append(args, query.UserID)
	}

	if query.ProjectID != "" {
		whereClause += " AND project_id = ?"
		args = append(args, query.ProjectID)
	}

	if query.EventType != "" {
		whereClause += " AND event_type = ?"
		args = append(args, query.EventType)
	}

	args = append(args, limit)

	rows, err := s.db.Query(`
		SELECT id, timestamp, user_id, project_id, event_type, quantity, unit_cost, total_cost, metadata, COALESCE(execution_id, '')
		FROM usage_events
		`+whereClause+`
		ORDER BY timestamp DESC
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get usage events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []*UsageEvent
	for rows.Next() {
		var e UsageEvent
		var metadataStr string
		var eventType string
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.UserID, &e.ProjectID, &eventType, &e.Quantity, &e.UnitCost, &e.TotalCost, &metadataStr, &e.ExecutionID); err != nil {
			return nil, err
		}
		e.EventType = UsageEventType(eventType)
		if metadataStr != "" {
			if err := json.Unmarshal([]byte(metadataStr), &e.Metadata); err != nil {
				slog.Warn("failed to unmarshal usage event metadata",
					slog.String("event_id", e.ID),
					slog.Any("error", err))
			}
		}
		events = append(events, &e)
	}

	return events, rows.Err()
}
