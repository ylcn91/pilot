package autopilot

import (
	"database/sql"
	"time"
)

// Mark records that an issue has been processed for the given source adapter and repo.
// For tracker-style adapters (linear, jira, asana, etc.) pass repo="".
// For VCS-hosted adapters (github, gitlab) pass repo as "owner/repo".
func (s *StateStore) Mark(source, repo, issueID string) error {
	_, err := s.db.Exec(`
		INSERT INTO adapter_processed (adapter, repo, issue_id, processed_at, result)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP, '')
		ON CONFLICT(adapter, issue_id) DO UPDATE SET
			repo = excluded.repo,
			processed_at = CURRENT_TIMESTAMP
	`, source, repo, issueID)
	return err
}

// Unmark removes the processed record for the given source, repo, and issue.
// Used when a failed-label is removed to allow retry.
func (s *StateStore) Unmark(source, repo, issueID string) error {
	_, err := s.db.Exec(
		`DELETE FROM adapter_processed WHERE adapter = ? AND repo = ? AND issue_id = ?`,
		source, repo, issueID,
	)
	return err
}

// IsProcessed reports whether the given issue has been processed.
func (s *StateStore) IsProcessed(source, repo, issueID string) (bool, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM adapter_processed WHERE adapter = ? AND repo = ? AND issue_id = ?`,
		source, repo, issueID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Load returns all processed issue IDs (and their timestamps) for the given source and repo.
func (s *StateStore) Load(source, repo string) (map[string]time.Time, error) {
	rows, err := s.db.Query(
		`SELECT issue_id, processed_at FROM adapter_processed WHERE adapter = ? AND repo = ?`,
		source, repo,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	processed := make(map[string]time.Time)
	for rows.Next() {
		var id string
		var ts time.Time
		if err := rows.Scan(&id, &ts); err != nil {
			return nil, err
		}
		processed[id] = ts
	}
	return processed, nil
}

// Purge removes processed records for the given source that are older than olderThan.
func (s *StateStore) Purge(source string, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	result, err := s.db.Exec(
		`DELETE FROM adapter_processed WHERE adapter = ? AND processed_at < ?`,
		source, cutoff,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// SaveMetadata stores a key-value pair in the metadata table.
func (s *StateStore) SaveMetadata(key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO autopilot_metadata (key, value, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = CURRENT_TIMESTAMP
	`, key, value)
	return err
}

// GetMetadata retrieves a metadata value by key.
// Returns empty string if not found.
func (s *StateStore) GetMetadata(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM autopilot_metadata WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SavePRFailures persists the per-PR failure state.
func (s *StateStore) SavePRFailures(prNumber, failureCount int, lastFailureTime time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO autopilot_pr_failures (pr_number, failure_count, last_failure_time)
		VALUES (?, ?, ?)
		ON CONFLICT(pr_number) DO UPDATE SET
			failure_count = excluded.failure_count,
			last_failure_time = excluded.last_failure_time
	`, prNumber, failureCount, lastFailureTime)
	return err
}

// RemovePRFailures removes per-PR failure state.
func (s *StateStore) RemovePRFailures(prNumber int) error {
	_, err := s.db.Exec(`DELETE FROM autopilot_pr_failures WHERE pr_number = ?`, prNumber)
	return err
}

// LoadAllPRFailures loads all per-PR failure states.
func (s *StateStore) LoadAllPRFailures() (map[int]*prFailureState, error) {
	rows, err := s.db.Query(`
		SELECT pr_number, failure_count, last_failure_time
		FROM autopilot_pr_failures
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	failures := make(map[int]*prFailureState)
	for rows.Next() {
		var prNumber, failureCount int
		var lastFailureTime time.Time

		if err := rows.Scan(&prNumber, &failureCount, &lastFailureTime); err != nil {
			return nil, err
		}

		failures[prNumber] = &prFailureState{
			FailureCount:    failureCount,
			LastFailureTime: lastFailureTime,
		}
	}
	return failures, nil
}
