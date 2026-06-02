package briefs

import (
	"strings"
	"testing"
	"time"
)

func TestPlainTextFormatter(t *testing.T) {
	brief := createTestBrief()
	formatter := NewPlainTextFormatter()

	text, err := formatter.Format(brief)
	if err != nil {
		t.Fatalf("failed to format: %v", err)
	}

	// Check for expected content
	expectations := []string{
		"PILOT DAILY BRIEF",
		"Jan 26, 2026",
		"COMPLETED (2)",
		"TASK-001",
		"https://github.com/test/pr/1",
		"IN PROGRESS (1)",
		"TASK-003",
		"65%",
		"FAILED (1)",
		"TASK-004",
		"auth_test.go:42",
		"UPCOMING (1)",
		"TASK-005",
		"METRICS",
		"85%",
		"12m",
	}

	for _, expected := range expectations {
		if !strings.Contains(text, expected) {
			t.Errorf("expected %q in output, not found", expected)
		}
	}
}

func TestEmptyBriefFormatting(t *testing.T) {
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

	// All formatters should handle empty briefs
	formatters := []struct {
		name      string
		formatter Formatter
	}{
		{"plain", NewPlainTextFormatter()},
		{"slack", NewSlackFormatter()},
		{"email", NewEmailFormatter()},
	}

	for _, f := range formatters {
		t.Run(f.name, func(t *testing.T) {
			text, err := f.formatter.Format(brief)
			if err != nil {
				t.Fatalf("failed to format empty brief: %v", err)
			}
			if text == "" {
				t.Error("expected non-empty output for empty brief")
			}
		})
	}
}

func TestPlainTextFormatterWithLongError(t *testing.T) {
	brief := &Brief{
		GeneratedAt: time.Now(),
		Period: BriefPeriod{
			Start: time.Now().Add(-24 * time.Hour),
			End:   time.Now(),
		},
		Completed:  []TaskSummary{},
		InProgress: []TaskSummary{},
		Blocked: []BlockedTask{
			{
				TaskSummary: TaskSummary{
					ID:     "TASK-001",
					Status: "failed",
				},
				// Error longer than 60 characters should be truncated
				Error: "This is a very long error message that should be truncated because it exceeds the maximum length allowed in the plain text formatter output",
			},
		},
		Upcoming: []TaskSummary{},
		Metrics:  BriefMetrics{},
	}

	formatter := NewPlainTextFormatter()
	text, err := formatter.Format(brief)
	if err != nil {
		t.Fatalf("failed to format: %v", err)
	}

	// Should contain truncated error with "..."
	if !strings.Contains(text, "...") {
		t.Error("expected truncated error with '...'")
	}
}

func TestPlainTextFormatterWithMultilineError(t *testing.T) {
	brief := &Brief{
		GeneratedAt: time.Now(),
		Period: BriefPeriod{
			Start: time.Now().Add(-24 * time.Hour),
			End:   time.Now(),
		},
		Completed:  []TaskSummary{},
		InProgress: []TaskSummary{},
		Blocked: []BlockedTask{
			{
				TaskSummary: TaskSummary{
					ID:     "TASK-001",
					Status: "failed",
				},
				// Multi-line error - should only show first line
				Error: "First line of error\nSecond line\nThird line",
			},
		},
		Upcoming: []TaskSummary{},
		Metrics:  BriefMetrics{},
	}

	formatter := NewPlainTextFormatter()
	text, err := formatter.Format(brief)
	if err != nil {
		t.Fatalf("failed to format: %v", err)
	}

	// Should contain first line
	if !strings.Contains(text, "First line of error") {
		t.Error("expected first line of error")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		ms       int64
		expected string
	}{
		{0, "N/A"},
		{30000, "30s"},
		{60000, "1m"},
		{90000, "1m"},
		{300000, "5m"},
		{720000, "12m"},
		{3600000, "1h 0m"},
		{5400000, "1h 30m"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatDuration(tt.ms)
			if result != tt.expected {
				t.Errorf("formatDuration(%d) = %q, want %q", tt.ms, result, tt.expected)
			}
		})
	}
}

func TestFormatDurationEdgeCases(t *testing.T) {
	tests := []struct {
		ms       int64
		expected string
	}{
		{45000, "45s"},     // 45 seconds
		{59000, "59s"},     // Just under a minute
		{61000, "1m"},      // Just over a minute
		{119000, "1m"},     // Just under 2 minutes
		{3599000, "59m"},   // Just under an hour
		{3601000, "1h 0m"}, // Just over an hour
		{7200000, "2h 0m"}, // 2 hours exact
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatDuration(tt.ms)
			if result != tt.expected {
				t.Errorf("formatDuration(%d) = %q, want %q", tt.ms, result, tt.expected)
			}
		})
	}
}
