package memory

import (
	"database/sql"
	"time"
)

// BriefRecord represents a record of a brief that was sent.
type BriefRecord struct {
	ID        int64
	SentAt    time.Time
	Channel   string // e.g., "telegram", "slack", "email"
	BriefType string // e.g., "daily", "weekly"
	Recipient string // optional recipient identifier
}

// RecordBriefSent records that a brief was sent to a channel.
func (s *Store) RecordBriefSent(record *BriefRecord) error {
	return s.withRetry("RecordBriefSent", func() error {
		result, err := s.db.Exec(`
			INSERT INTO brief_history (sent_at, channel, brief_type, recipient)
			VALUES (?, ?, ?, ?)
		`, record.SentAt, record.Channel, record.BriefType, record.Recipient)
		if err != nil {
			return err
		}
		id, _ := result.LastInsertId()
		record.ID = id
		return nil
	})
}

// GetLastBriefSent returns the most recent brief record for a given channel.
// Returns nil if no brief has been sent to the channel.
func (s *Store) GetLastBriefSent(channel string) (*BriefRecord, error) {
	row := s.db.QueryRow(`
		SELECT id, sent_at, channel, brief_type, COALESCE(recipient, '')
		FROM brief_history
		WHERE channel = ?
		ORDER BY sent_at DESC
		LIMIT 1
	`, channel)

	var record BriefRecord
	err := row.Scan(&record.ID, &record.SentAt, &record.Channel, &record.BriefType, &record.Recipient)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}
