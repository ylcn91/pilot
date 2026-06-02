package main

import (
	"testing"
	"time"
)

func TestQueueTaskBetter(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Hour)

	tests := []struct {
		name      string
		candidate QueueTask
		existing  QueueTask
		want      bool
	}{
		{
			name:      "running beats done",
			candidate: QueueTask{Status: "running", CreatedAt: earlier},
			existing:  QueueTask{Status: "done", CreatedAt: now},
			want:      true,
		},
		{
			name:      "done beats failed",
			candidate: QueueTask{Status: "done", CreatedAt: earlier},
			existing:  QueueTask{Status: "failed", CreatedAt: now},
			want:      true,
		},
		{
			name:      "failed does not beat done",
			candidate: QueueTask{Status: "failed", CreatedAt: now},
			existing:  QueueTask{Status: "done", CreatedAt: earlier},
			want:      false,
		},
		{
			name:      "same status newer wins",
			candidate: QueueTask{Status: "done", CreatedAt: now},
			existing:  QueueTask{Status: "done", CreatedAt: earlier},
			want:      true,
		},
		{
			name:      "same status older loses",
			candidate: QueueTask{Status: "done", CreatedAt: earlier},
			existing:  QueueTask{Status: "done", CreatedAt: now},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := queueTaskBetter(tt.candidate, tt.existing)
			if got != tt.want {
				t.Errorf("queueTaskBetter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHistoryEntryBetter(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Hour)

	tests := []struct {
		name      string
		candidate HistoryEntry
		existing  HistoryEntry
		want      bool
	}{
		{
			name:      "completed beats failed",
			candidate: HistoryEntry{Status: "completed", CompletedAt: earlier},
			existing:  HistoryEntry{Status: "failed", CompletedAt: now},
			want:      true,
		},
		{
			name:      "failed does not beat completed",
			candidate: HistoryEntry{Status: "failed", CompletedAt: now},
			existing:  HistoryEntry{Status: "completed", CompletedAt: earlier},
			want:      false,
		},
		{
			name:      "with PR URL beats without",
			candidate: HistoryEntry{Status: "completed", PRURL: "https://pr/1", CompletedAt: earlier},
			existing:  HistoryEntry{Status: "completed", CompletedAt: now},
			want:      true,
		},
		{
			name:      "without PR URL does not beat with",
			candidate: HistoryEntry{Status: "completed", CompletedAt: now},
			existing:  HistoryEntry{Status: "completed", PRURL: "https://pr/1", CompletedAt: earlier},
			want:      false,
		},
		{
			name:      "same status same PR newer wins",
			candidate: HistoryEntry{Status: "failed", CompletedAt: now},
			existing:  HistoryEntry{Status: "failed", CompletedAt: earlier},
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := historyEntryBetter(tt.candidate, tt.existing)
			if got != tt.want {
				t.Errorf("historyEntryBetter() = %v, want %v", got, tt.want)
			}
		})
	}
}
