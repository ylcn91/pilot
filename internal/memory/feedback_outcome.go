package memory

import (
	"math"
	"time"
)

// RecordPatternOutcome records a success or failure for a pattern in a specific
// project/task-type context. Uses UPSERT on the (pattern_id, project_id, task_type)
// unique constraint to accumulate counts and refresh last_used.
func (s *Store) RecordPatternOutcome(patternID, projectID, taskType, model string, success bool) error {
	return s.withRetry("RecordPatternOutcome", func() error {
		if success {
			_, err := s.db.Exec(`
				INSERT INTO pattern_performance (pattern_id, project_id, task_type, model, success_count, failure_count, last_used)
				VALUES (?, ?, ?, ?, 1, 0, CURRENT_TIMESTAMP)
				ON CONFLICT(pattern_id, project_id, task_type) DO UPDATE SET
					success_count = pattern_performance.success_count + 1,
					model = excluded.model,
					last_used = CURRENT_TIMESTAMP
			`, patternID, projectID, taskType, model)
			return err
		}
		_, err := s.db.Exec(`
			INSERT INTO pattern_performance (pattern_id, project_id, task_type, model, success_count, failure_count, last_used)
			VALUES (?, ?, ?, ?, 0, 1, CURRENT_TIMESTAMP)
			ON CONFLICT(pattern_id, project_id, task_type) DO UPDATE SET
				failure_count = pattern_performance.failure_count + 1,
				model = excluded.model,
				last_used = CURRENT_TIMESTAMP
		`, patternID, projectID, taskType, model)
		return err
	})
}

// GetContextualConfidence computes a contextual confidence score for a pattern
// in a given project and task type. The score combines:
//   - Project-specific success rate (success / total outcomes)
//   - Recency decay: 1.0 / (1.0 + daysSinceLastUse/30)
//
// Returns 0.5 (neutral) when no outcome data exists.
func (s *Store) GetContextualConfidence(patternID, projectID, taskType string) float64 {
	var successCount, failureCount int
	var lastUsed time.Time

	err := s.db.QueryRow(`
		SELECT success_count, failure_count, last_used
		FROM pattern_performance
		WHERE pattern_id = ? AND project_id = ? AND task_type = ?
	`, patternID, projectID, taskType).Scan(&successCount, &failureCount, &lastUsed)
	if err != nil {
		return 0.5 // No data — neutral default
	}

	total := successCount + failureCount
	if total == 0 {
		return 0.5
	}

	successRate := float64(successCount) / float64(total)
	daysSince := time.Since(lastUsed).Hours() / 24.0
	recencyDecay := 1.0 / (1.0 + daysSince/30.0)

	return math.Min(1.0, successRate*recencyDecay)
}
