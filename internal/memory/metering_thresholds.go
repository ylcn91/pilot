package memory

import (
	"fmt"
	"time"
)

// UsageThreshold defines an alert threshold for usage
type UsageThreshold struct {
	UserID      string
	MetricType  string // "cost", "tasks", "tokens"
	Threshold   float64
	Period      string // "daily", "weekly", "monthly"
	LastAlerted time.Time
}

// CheckUsageThresholds checks if any thresholds are exceeded
func (s *Store) CheckUsageThresholds(userID string) ([]string, error) {
	// Get current month's usage
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	summary, err := s.GetUsageSummary(UsageQuery{
		UserID: userID,
		Start:  monthStart,
		End:    now,
	})
	if err != nil {
		return nil, err
	}

	var alerts []string

	// Example thresholds (would be configurable in production)
	if summary.TotalCost > 100.0 {
		alerts = append(alerts, fmt.Sprintf("Monthly cost threshold exceeded: $%.2f", summary.TotalCost))
	}

	if summary.TaskCount > 500 {
		alerts = append(alerts, fmt.Sprintf("Monthly task limit approaching: %d tasks", summary.TaskCount))
	}

	return alerts, nil
}
