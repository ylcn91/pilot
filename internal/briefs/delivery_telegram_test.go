package briefs

import (
	"log/slog"
	"testing"
	"time"
)

func TestFormatTelegramTokens(t *testing.T) {
	tests := []struct {
		tokens   int64
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{10000, "10.0k"},
		{999999, "1000.0k"},
		{1000000, "1.0M"},
		{1500000, "1.5M"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatTelegramTokens(tt.tokens)
			if result != tt.expected {
				t.Errorf("formatTelegramTokens(%d) = %q, want %q", tt.tokens, result, tt.expected)
			}
		})
	}
}

func TestFormatTelegramBrief(t *testing.T) {
	config := &BriefConfig{}
	service := &DeliveryService{
		config:   config,
		logger:   slog.Default(),
		slackFmt: NewSlackFormatter(),
		emailFmt: NewEmailFormatter(),
		plainFmt: NewPlainTextFormatter(),
	}

	brief := createTestBrief()
	text := service.formatTelegramBrief(brief)

	// Check that key elements are present
	expectedElements := []string{
		"Daily Brief",
		"Jan 26, 2026",
		"tasks completed",
		"avg duration",
		"tokens used",
		"estimated cost",
		"Completed:",
		"TASK-001",
		"TASK-002",
		"Failed:",
		"TASK-004",
		"Queue",
		"TASK-005",
	}

	for _, elem := range expectedElements {
		if !containsString(text, elem) {
			t.Errorf("expected %q in Telegram brief, not found", elem)
		}
	}
}

func TestFormatTelegramBriefEmpty(t *testing.T) {
	config := &BriefConfig{}
	service := &DeliveryService{
		config:   config,
		logger:   slog.Default(),
		slackFmt: NewSlackFormatter(),
		emailFmt: NewEmailFormatter(),
		plainFmt: NewPlainTextFormatter(),
	}

	brief := &Brief{
		GeneratedAt: time.Now(),
		Period: BriefPeriod{
			Start: time.Now().Add(-24 * time.Hour),
			End:   time.Now(),
		},
		Completed:  []TaskSummary{},
		InProgress: []TaskSummary{},
		Blocked:    []BlockedTask{},
		Upcoming:   []TaskSummary{},
		Metrics:    BriefMetrics{},
	}

	text := service.formatTelegramBrief(brief)

	// Should still have header and metrics
	if text == "" {
		t.Error("expected non-empty text for empty brief")
	}

	if !containsString(text, "Daily Brief") {
		t.Error("expected 'Daily Brief' in output")
	}
}
