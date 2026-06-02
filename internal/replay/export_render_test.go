package replay

import (
	"strings"
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{30 * time.Second, "30.0s"},
		{90 * time.Second, "1m 30s"},
		{65 * time.Minute, "1h 5m"},
		{2*time.Hour + 30*time.Minute, "2h 30m"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			result := formatDuration(tc.duration)
			if result != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		number   int64
		expected string
	}{
		{500, "500"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{1000000, "1.0M"},
		{2500000, "2.5M"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			result := formatNumber(tc.number)
			if result != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

func TestFormatEventForTimeline(t *testing.T) {
	tests := []struct {
		name     string
		event    *StreamEvent
		contains string
	}{
		{
			name:     "system init",
			event:    &StreamEvent{Parsed: &ParsedEvent{Type: "system", Subtype: "init"}},
			contains: "System initialized",
		},
		{
			name:     "system other",
			event:    &StreamEvent{Parsed: &ParsedEvent{Type: "system", Subtype: "config"}},
			contains: "System: config",
		},
		{
			name: "tool with detail",
			event: &StreamEvent{
				Parsed: &ParsedEvent{
					Type:      "assistant",
					ToolName:  "Read",
					ToolInput: map[string]any{"file_path": "/path/to/file.go"},
				},
			},
			contains: "Read:",
		},
		{
			name:     "tool without detail",
			event:    &StreamEvent{Parsed: &ParsedEvent{Type: "assistant", ToolName: "Read"}},
			contains: "Read",
		},
		{
			name:     "text event",
			event:    &StreamEvent{Parsed: &ParsedEvent{Type: "assistant", Text: "Analyzing code..."}},
			contains: "Analyzing",
		},
		{
			name:     "user event",
			event:    &StreamEvent{Parsed: &ParsedEvent{Type: "user"}},
			contains: "Tool result",
		},
		{
			name:     "result with tokens",
			event:    &StreamEvent{Parsed: &ParsedEvent{Type: "result", InputTokens: 100, OutputTokens: 50}},
			contains: "100 in",
		},
		{
			name:     "result error",
			event:    &StreamEvent{Parsed: &ParsedEvent{Type: "result", IsError: true, Result: "Failed"}},
			contains: "Error:",
		},
		{
			name:     "result success no tokens",
			event:    &StreamEvent{Parsed: &ParsedEvent{Type: "result"}},
			contains: "Completed",
		},
		{
			name:     "unknown type",
			event:    &StreamEvent{Parsed: &ParsedEvent{Type: "custom"}},
			contains: "custom",
		},
		{
			name:     "nil parsed",
			event:    &StreamEvent{Type: "raw"},
			contains: "raw",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := formatEventForTimeline(tc.event)
			if !strings.Contains(result, tc.contains) {
				t.Errorf("Expected result to contain '%s', got: '%s'", tc.contains, result)
			}
		})
	}
}

func TestHTMLReportStyles(t *testing.T) {
	styles := htmlReportStyles()

	// Should contain key CSS rules
	expectedRules := []string{
		"--bg-primary",
		"--accent-blue",
		".header",
		".summary-card",
		".timeline",
		".error-item",
	}

	for _, rule := range expectedRules {
		if !strings.Contains(styles, rule) {
			t.Errorf("Styles should contain '%s'", rule)
		}
	}
}

func TestRenderHTMLHeader(t *testing.T) {
	recording := &Recording{
		ID:          "TG-123",
		TaskID:      "TASK-456",
		ProjectPath: "/test/project",
		Status:      "completed",
		StartTime:   time.Now(),
	}

	html := renderHTMLHeader(recording)

	if !strings.Contains(html, "TG-123") {
		t.Error("Header should contain recording ID")
	}
	if !strings.Contains(html, "TASK-456") {
		t.Error("Header should contain task ID")
	}
	if !strings.Contains(html, "status-completed") {
		t.Error("Header should have completed status class")
	}
}

func TestRenderHTMLSummary(t *testing.T) {
	recording := &Recording{
		ID:         "TG-123",
		Duration:   5 * time.Minute,
		EventCount: 100,
		TokenUsage: &TokenUsage{
			InputTokens:      5000,
			OutputTokens:     2000,
			TotalTokens:      7000,
			EstimatedCostUSD: 0.0245,
		},
		Metadata: &Metadata{
			ModelName:    "claude-sonnet-4-6",
			HasNavigator: true,
		},
	}

	html := renderHTMLSummary(recording)

	if !strings.Contains(html, "Duration") {
		t.Error("Summary should contain Duration")
	}
	if !strings.Contains(html, "100") {
		t.Error("Summary should contain event count")
	}
	if !strings.Contains(html, "7.0K") {
		t.Error("Summary should contain total tokens")
	}
	if !strings.Contains(html, "Navigator") {
		t.Error("Summary should mention Navigator")
	}
}

func TestRenderHTMLToolUsage(t *testing.T) {
	report := &AnalysisReport{
		ToolUsage: []ToolUsageStats{
			{Tool: "Read", Count: 10, ErrorCount: 0},
			{Tool: "Write", Count: 5, ErrorCount: 1},
		},
	}

	html := renderHTMLToolUsage(report)

	if !strings.Contains(html, "Tool Usage") {
		t.Error("Should contain Tool Usage header")
	}
	if !strings.Contains(html, "Read") {
		t.Error("Should contain Read tool")
	}
	if !strings.Contains(html, "errors") {
		t.Error("Should mention errors for Write tool")
	}
}

func TestRenderHTMLErrors(t *testing.T) {
	report := &AnalysisReport{
		Errors: []ErrorEvent{
			{
				Sequence:  5,
				Timestamp: time.Now(),
				Phase:     "Research",
				Tool:      "Read",
				Message:   "File not found",
			},
		},
	}

	html := renderHTMLErrors(report)

	if !strings.Contains(html, "Errors (1)") {
		t.Error("Should show error count")
	}
	if !strings.Contains(html, "File not found") {
		t.Error("Should contain error message")
	}
	if !strings.Contains(html, "Research") {
		t.Error("Should contain phase")
	}
}

func TestRenderHTMLTimeline(t *testing.T) {
	events := []*StreamEvent{
		{
			Sequence:  1,
			Timestamp: time.Now(),
			Parsed:    &ParsedEvent{Type: "system", Subtype: "init"},
		},
		{
			Sequence:  2,
			Timestamp: time.Now(),
			Parsed:    &ParsedEvent{Type: "assistant", ToolName: "Read", ToolInput: map[string]any{"file_path": "/test"}},
		},
	}

	html := renderHTMLTimeline(events)

	if !strings.Contains(html, "Execution Timeline") {
		t.Error("Should contain timeline header")
	}
	if !strings.Contains(html, "2 events") {
		t.Error("Should show event count")
	}
	if !strings.Contains(html, "timeline-event") {
		t.Error("Should contain event elements")
	}
}

func TestStatusClasses(t *testing.T) {
	tests := []struct {
		status   string
		expected string
	}{
		{"completed", "status-completed"},
		{"failed", "status-failed"},
		{"cancelled", "status-cancelled"},
	}

	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			recording := &Recording{
				ID:        "TG-123",
				Status:    tc.status,
				StartTime: time.Now(),
			}
			html := renderHTMLHeader(recording)
			if !strings.Contains(html, tc.expected) {
				t.Errorf("Should contain status class '%s'", tc.expected)
			}
		})
	}
}
