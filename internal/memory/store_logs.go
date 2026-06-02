package memory

import (
	"database/sql"
	"time"
)

// LogEntry represents a structured execution log entry.
type LogEntry struct {
	ID          int64     `json:"id"`
	ExecutionID string    `json:"executionId,omitempty"`
	Timestamp   time.Time `json:"ts"`
	Level       string    `json:"level"`
	Message     string    `json:"message"`
	Component   string    `json:"component"`
}

// SaveLogEntry persists an execution log entry and notifies all subscribers.
func (s *Store) SaveLogEntry(entry *LogEntry) error {
	err := s.withRetry("SaveLogEntry", func() error {
		result, err := s.db.Exec(`
			INSERT INTO execution_logs (execution_id, timestamp, level, message, component)
			VALUES (?, ?, ?, ?, ?)
		`, entry.ExecutionID, entry.Timestamp, entry.Level, entry.Message, entry.Component)
		if err != nil {
			return err
		}
		id, _ := result.LastInsertId()
		entry.ID = id
		return nil
	})
	if err != nil {
		return err
	}

	// Fan out to subscribers (non-blocking)
	s.logSubMu.RLock()
	for ch := range s.logSubscribers {
		select {
		case ch <- entry:
		default:
			// Slow consumer, drop entry
		}
	}
	s.logSubMu.RUnlock()

	return nil
}

// SubscribeLogs returns a channel that receives new log entries as they are saved.
// The channel is buffered to avoid blocking the writer. Call UnsubscribeLogs to clean up.
func (s *Store) SubscribeLogs() chan *LogEntry {
	ch := make(chan *LogEntry, 64)
	s.logSubMu.Lock()
	s.logSubscribers[ch] = struct{}{}
	s.logSubMu.Unlock()
	return ch
}

// UnsubscribeLogs removes a subscriber channel and closes it.
func (s *Store) UnsubscribeLogs(ch chan *LogEntry) {
	s.logSubMu.Lock()
	delete(s.logSubscribers, ch)
	s.logSubMu.Unlock()
	close(ch)
}

// GetRecentLogs returns the most recent log entries ordered by timestamp descending.
func (s *Store) GetRecentLogs(limit int) ([]*LogEntry, error) {
	rows, err := s.db.Query(`
		SELECT id, COALESCE(execution_id, ''), timestamp, level, message, COALESCE(component, 'executor')
		FROM execution_logs
		ORDER BY timestamp DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var entries []*LogEntry
	for rows.Next() {
		var e LogEntry
		if err := rows.Scan(&e.ID, &e.ExecutionID, &e.Timestamp, &e.Level, &e.Message, &e.Component); err != nil {
			return nil, err
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

// PruneExecutionLogs deletes execution log entries older than the given duration.
// Returns the number of rows deleted. Runs a WAL checkpoint after a large
// prune (>1000 rows) to reclaim disk space promptly.
func (s *Store) PruneExecutionLogs(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	var result sql.Result
	err := s.withRetry("PruneExecutionLogs", func() error {
		var execErr error
		result, execErr = s.db.Exec(`DELETE FROM execution_logs WHERE timestamp < ?`, cutoff)
		return execErr
	})
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n > 1000 {
		_, _ = s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	}
	return n, nil
}
