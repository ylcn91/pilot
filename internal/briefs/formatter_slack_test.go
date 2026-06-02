package briefs

import (
	"strings"
	"testing"
	"time"
)

func TestSlackFormatter(t *testing.T) {
	brief := createTestBrief()
	formatter := NewSlackFormatter()

	text, err := formatter.Format(brief)
	if err != nil {
		t.Fatalf("failed to format: %v", err)
	}

	// Check for Slack-specific formatting
	expectations := []string{
		":bar_chart: *Pilot Daily Brief*",
		":white_check_mark: Completed (2)",
		"`TASK-001`",
		"<https://github.com/test/pr/1|PR ready>",
		":arrows_counterclockwise: In Progress (1)",
		"▓", // Progress bar filled
		"░", // Progress bar empty
		":no_entry: Blocked (1)",
		":clipboard: Upcoming (1)",
		":chart_with_upwards_trend: Metrics",
		"*85%*",
	}

	for _, expected := range expectations {
		if !strings.Contains(text, expected) {
			t.Errorf("expected %q in output, not found", expected)
		}
	}
}

func TestSlackFormatterBlocks(t *testing.T) {
	brief := createTestBrief()
	formatter := NewSlackFormatter()

	blocks := formatter.SlackBlocks(brief)

	if len(blocks) == 0 {
		t.Fatal("expected blocks, got none")
	}

	// First block should be header
	if blocks[0]["type"] != "header" {
		t.Errorf("expected header block first, got %s", blocks[0]["type"])
	}

	// Should have section blocks for each section
	sectionCount := 0
	for _, block := range blocks {
		if block["type"] == "section" {
			sectionCount++
		}
	}

	// At minimum: completed, in progress, blocked, upcoming = 4 sections
	if sectionCount < 4 {
		t.Errorf("expected at least 4 section blocks, got %d", sectionCount)
	}

	// Should have a divider
	hasDivider := false
	for _, block := range blocks {
		if block["type"] == "divider" {
			hasDivider = true
			break
		}
	}
	if !hasDivider {
		t.Error("expected divider block")
	}

	// Should have context for metrics
	hasContext := false
	for _, block := range blocks {
		if block["type"] == "context" {
			hasContext = true
			break
		}
	}
	if !hasContext {
		t.Error("expected context block for metrics")
	}
}

func TestSlackProgressBar(t *testing.T) {
	tests := []struct {
		progress int
		filled   int
		empty    int
	}{
		{0, 0, 10},
		{10, 1, 9},
		{50, 5, 5},
		{65, 6, 4},
		{100, 10, 0},
	}

	for _, tt := range tests {
		t.Run(string(rune('0'+tt.progress/10)), func(t *testing.T) {
			result := generateSlackProgressBar(tt.progress)

			filledCount := strings.Count(result, "▓")
			emptyCount := strings.Count(result, "░")

			if filledCount != tt.filled {
				t.Errorf("expected %d filled, got %d", tt.filled, filledCount)
			}
			if emptyCount != tt.empty {
				t.Errorf("expected %d empty, got %d", tt.empty, emptyCount)
			}
		})
	}
}

func TestSlackFormatterWithLongError(t *testing.T) {
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
				// Error longer than 80 characters should be truncated
				Error: "This is a very long error message that should definitely be truncated because it is way too long for the Slack format",
			},
		},
		Upcoming: []TaskSummary{},
		Metrics:  BriefMetrics{},
	}

	formatter := NewSlackFormatter()
	text, err := formatter.Format(brief)
	if err != nil {
		t.Fatalf("failed to format: %v", err)
	}

	// Should contain truncated error with "..."
	if !strings.Contains(text, "...") {
		t.Error("expected truncated error with '...'")
	}
}

func TestSlackFormatterBlocksWithLongError(t *testing.T) {
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
				// Error longer than 50 characters should be truncated in blocks
				Error: "This error message is longer than fifty characters and will be truncated",
			},
		},
		Upcoming: []TaskSummary{},
		Metrics:  BriefMetrics{},
	}

	formatter := NewSlackFormatter()
	blocks := formatter.SlackBlocks(brief)

	if len(blocks) == 0 {
		t.Fatal("expected blocks, got none")
	}

	// Find the blocked section and verify truncation
	found := false
	for _, block := range blocks {
		if block["type"] == "section" {
			if text, ok := block["text"].(map[string]interface{}); ok {
				if textStr, ok := text["text"].(string); ok {
					if strings.Contains(textStr, "Blocked") && strings.Contains(textStr, "...") {
						found = true
						break
					}
				}
			}
		}
	}

	if !found {
		t.Error("expected blocked section with truncated error")
	}
}

func TestSlackFormatterWithCompletedTasksNoPR(t *testing.T) {
	brief := &Brief{
		GeneratedAt: time.Now(),
		Period: BriefPeriod{
			Start: time.Now().Add(-24 * time.Hour),
			End:   time.Now(),
		},
		Completed: []TaskSummary{
			{
				ID:         "TASK-001",
				Title:      "Task without PR",
				Status:     "completed",
				PRUrl:      "", // No PR URL
				DurationMs: 60000,
			},
		},
		InProgress: []TaskSummary{},
		Blocked:    []BlockedTask{},
		Upcoming:   []TaskSummary{},
		Metrics:    BriefMetrics{CompletedCount: 1, TotalTasks: 1, SuccessRate: 1.0},
	}

	formatter := NewSlackFormatter()
	text, err := formatter.Format(brief)
	if err != nil {
		t.Fatalf("failed to format: %v", err)
	}

	// Should contain task ID but not PR link
	if !strings.Contains(text, "TASK-001") {
		t.Error("expected task ID in output")
	}
	if strings.Contains(text, "PR ready") {
		t.Error("should not contain PR link for task without PR")
	}
}

func TestSlackFormatterBlocksWithEmptySections(t *testing.T) {
	brief := &Brief{
		GeneratedAt: time.Now(),
		Period: BriefPeriod{
			Start: time.Now().Add(-24 * time.Hour),
			End:   time.Now(),
		},
		Completed:  []TaskSummary{},
		InProgress: []TaskSummary{},
		Blocked:    []BlockedTask{}, // Empty blocked - should not have blocked section
		Upcoming:   []TaskSummary{},
		Metrics:    BriefMetrics{},
	}

	formatter := NewSlackFormatter()
	blocks := formatter.SlackBlocks(brief)

	// Should not have blocked section when empty
	for _, block := range blocks {
		if block["type"] == "section" {
			if text, ok := block["text"].(map[string]interface{}); ok {
				if textStr, ok := text["text"].(string); ok {
					if strings.Contains(textStr, "Blocked") {
						t.Error("should not have blocked section when empty")
					}
				}
			}
		}
	}
}
