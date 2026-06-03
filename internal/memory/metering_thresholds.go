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

// defaultUsageThresholds returns the historical monthly thresholds used by
// CheckUsageThresholds: a $100 cost limit and a 500-task limit. They are the
// defaults populated into Store.usageThresholds so the limits become a field
// rather than inline literals.
func defaultUsageThresholds() []UsageThreshold {
	return []UsageThreshold{
		{MetricType: "cost", Threshold: 100.0, Period: "monthly"},
		{MetricType: "tasks", Threshold: 500, Period: "monthly"},
	}
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

	for _, t := range s.usageThresholds {
		switch t.MetricType {
		case "cost":
			if summary.TotalCost > t.Threshold {
				alerts = append(alerts, fmt.Sprintf("Monthly cost threshold exceeded: $%.2f", summary.TotalCost))
			}
		case "tasks":
			if float64(summary.TaskCount) > t.Threshold {
				alerts = append(alerts, fmt.Sprintf("Monthly task limit approaching: %d tasks", summary.TaskCount))
			}
		}
	}

	return alerts, nil
}
