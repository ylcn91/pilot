package briefs

import (
	"strings"
	"testing"
	"time"
)

func TestEmailFormatter(t *testing.T) {
	brief := createTestBrief()
	formatter := NewEmailFormatter()

	html, err := formatter.Format(brief)
	if err != nil {
		t.Fatalf("failed to format: %v", err)
	}

	// Check for HTML structure
	expectations := []string{
		"<!DOCTYPE html>",
		"<html>",
		"<head>",
		"<style>",
		"Pilot Daily Brief",
		"TASK-001",
		"href=\"https://github.com/test/pr/1\"",
		"progress-bar",
		"progress-fill",
		"65%",
		"TASK-004",
		"auth_test.go:42",
		"85%",
		"12m",
	}

	for _, expected := range expectations {
		if !strings.Contains(html, expected) {
			t.Errorf("expected %q in output, not found", expected)
		}
	}
}

func TestEmailFormatterSubject(t *testing.T) {
	brief := createTestBrief()
	formatter := NewEmailFormatter()

	subject := formatter.Subject(brief)

	expected := "Pilot Daily Brief — Jan 26, 2026"
	if subject != expected {
		t.Errorf("expected %q, got %q", expected, subject)
	}
}

func TestEmailFormatterWithLongError(t *testing.T) {
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
				// Error longer than 100 characters should be truncated
				Error: "This is a very very very long error message that should definitely be truncated because it is much too long for the email format output display",
			},
		},
		Upcoming: []TaskSummary{},
		Metrics:  BriefMetrics{},
	}

	formatter := NewEmailFormatter()
	text, err := formatter.Format(brief)
	if err != nil {
		t.Fatalf("failed to format: %v", err)
	}

	// Should contain truncated error with "..."
	if !strings.Contains(text, "...") {
		t.Error("expected truncated error with '...'")
	}
}
