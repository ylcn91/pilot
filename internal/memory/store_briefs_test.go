package memory

import (
	"testing"
	"time"
)

func TestBriefHistory(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(*Store)
		channel       string
		wantNil       bool
		wantBriefType string
		wantRecipient string
	}{
		{
			name:    "empty table returns nil",
			setup:   func(s *Store) {},
			channel: "telegram",
			wantNil: true,
		},
		{
			name: "single insert returns that record",
			setup: func(s *Store) {
				_ = s.RecordBriefSent(&BriefRecord{
					SentAt:    time.Now(),
					Channel:   "telegram",
					BriefType: "daily",
					Recipient: "user123",
				})
			},
			channel:       "telegram",
			wantNil:       false,
			wantBriefType: "daily",
			wantRecipient: "user123",
		},
		{
			name: "multiple inserts returns most recent",
			setup: func(s *Store) {
				// Insert older record first
				_ = s.RecordBriefSent(&BriefRecord{
					SentAt:    time.Now().Add(-2 * time.Hour),
					Channel:   "slack",
					BriefType: "daily",
					Recipient: "old-user",
				})
				// Insert newer record
				_ = s.RecordBriefSent(&BriefRecord{
					SentAt:    time.Now().Add(-1 * time.Hour),
					Channel:   "slack",
					BriefType: "weekly",
					Recipient: "new-user",
				})
				// Insert most recent
				_ = s.RecordBriefSent(&BriefRecord{
					SentAt:    time.Now(),
					Channel:   "slack",
					BriefType: "daily",
					Recipient: "latest-user",
				})
			},
			channel:       "slack",
			wantNil:       false,
			wantBriefType: "daily",
			wantRecipient: "latest-user",
		},
		{
			name: "filters by channel",
			setup: func(s *Store) {
				_ = s.RecordBriefSent(&BriefRecord{
					SentAt:    time.Now(),
					Channel:   "telegram",
					BriefType: "daily",
					Recipient: "tg-user",
				})
				_ = s.RecordBriefSent(&BriefRecord{
					SentAt:    time.Now(),
					Channel:   "slack",
					BriefType: "weekly",
					Recipient: "slack-user",
				})
			},
			channel:       "telegram",
			wantNil:       false,
			wantBriefType: "daily",
			wantRecipient: "tg-user",
		},
		{
			name: "non-existent channel returns nil",
			setup: func(s *Store) {
				_ = s.RecordBriefSent(&BriefRecord{
					SentAt:    time.Now(),
					Channel:   "telegram",
					BriefType: "daily",
				})
			},
			channel: "email",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			store, err := NewStore(tmpDir)
			if err != nil {
				t.Fatalf("NewStore: %v", err)
			}
			defer func() { _ = store.Close() }()

			tt.setup(store)

			record, err := store.GetLastBriefSent(tt.channel)
			if err != nil {
				t.Fatalf("GetLastBriefSent: %v", err)
			}

			if tt.wantNil {
				if record != nil {
					t.Errorf("expected nil, got %+v", record)
				}
				return
			}

			if record == nil {
				t.Fatal("expected non-nil record, got nil")
			}

			if record.Channel != tt.channel {
				t.Errorf("Channel = %q, want %q", record.Channel, tt.channel)
			}
			if record.BriefType != tt.wantBriefType {
				t.Errorf("BriefType = %q, want %q", record.BriefType, tt.wantBriefType)
			}
			if record.Recipient != tt.wantRecipient {
				t.Errorf("Recipient = %q, want %q", record.Recipient, tt.wantRecipient)
			}
			if record.ID == 0 {
				t.Error("ID should be set after insert")
			}
		})
	}
}

func TestRecordBriefSent_SetsID(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	record := &BriefRecord{
		SentAt:    time.Now(),
		Channel:   "telegram",
		BriefType: "daily",
	}

	if record.ID != 0 {
		t.Error("ID should be 0 before insert")
	}

	if err := store.RecordBriefSent(record); err != nil {
		t.Fatalf("RecordBriefSent: %v", err)
	}

	if record.ID == 0 {
		t.Error("ID should be set after insert")
	}
}
