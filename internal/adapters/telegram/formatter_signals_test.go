package telegram

import (
	"strings"
	"testing"
)

func TestCleanInternalSignals(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "clean text stays clean",
			input:    "Created file.go\nModified main.go",
			expected: "Created file.go\nModified main.go",
		},
		{
			name:     "removes EXIT_SIGNAL",
			input:    "Task done\nEXIT_SIGNAL: true\nCompleted",
			expected: "Task done\nCompleted",
		},
		{
			name:     "removes LOOP COMPLETE",
			input:    "Done\nLOOP COMPLETE\nEnd",
			expected: "Done\nEnd",
		},
		{
			name:     "removes NAVIGATOR_STATUS block",
			input:    "Start\n━━━━━━━━━━\nNAVIGATOR_STATUS\nPhase: IMPL\nIteration: 2\n━━━━━━━━━━\nContinuing",
			expected: "Start\nContinuing",
		},
		{
			name:     "removes Phase and Progress lines",
			input:    "Working\nPhase: VERIFY\nProgress: 80%\nDone",
			expected: "Working\nDone",
		},
		{
			name:     "trims leading empty lines",
			input:    "\n\n\nActual content",
			expected: "Actual content",
		},
		{
			name:     "trims trailing empty lines",
			input:    "Content\n\n\n",
			expected: "Content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanInternalSignals(tt.input)
			if got != tt.expected {
				t.Errorf("cleanInternalSignals() =\n%q\nwant\n%q", got, tt.expected)
			}
		})
	}
}

// TestCleanInternalSignalsNavigatorBlock tests NAVIGATOR_STATUS block removal
func TestCleanInternalSignalsNavigatorBlock(t *testing.T) {
	input := `Start of output
━━━━━━━━━━
NAVIGATOR_STATUS
Phase: IMPL
Iteration: 2
Progress: 50%
━━━━━━━━━━
End of output`

	got := cleanInternalSignals(input)

	if strings.Contains(got, "NAVIGATOR_STATUS") {
		t.Error("cleanInternalSignals should remove NAVIGATOR_STATUS")
	}
	if strings.Contains(got, "Phase: IMPL") {
		t.Error("cleanInternalSignals should remove Phase line")
	}
	if !strings.Contains(got, "Start of output") {
		t.Error("cleanInternalSignals should keep Start of output")
	}
	if !strings.Contains(got, "End of output") {
		t.Error("cleanInternalSignals should keep End of output")
	}
}

// TestCleanInternalSignalsAllSignals tests all signal types
func TestCleanInternalSignalsAllSignals(t *testing.T) {
	signals := []string{
		"EXIT_SIGNAL: true",
		"EXIT_SIGNAL:true",
		"LOOP COMPLETE",
		"TASK MODE COMPLETE",
		"Iteration: 5",
		"Completion Indicators: done",
		"Exit Conditions: met",
		"State Hash: abc123",
		"Next Action: verify",
	}

	for _, signal := range signals {
		t.Run(signal, func(t *testing.T) {
			input := "Before\n" + signal + "\nAfter"
			got := cleanInternalSignals(input)

			if strings.Contains(got, signal) {
				t.Errorf("cleanInternalSignals should remove %q, got:\n%s", signal, got)
			}
		})
	}
}

// TestInternalSignalsArray tests the internal signals slice
func TestInternalSignalsArray(t *testing.T) {
	expectedSignals := []string{
		"EXIT_SIGNAL: true",
		"EXIT_SIGNAL:true",
		"LOOP COMPLETE",
		"TASK MODE COMPLETE",
		"NAVIGATOR_STATUS",
	}

	for _, expected := range expectedSignals {
		found := false
		for _, signal := range internalSignals {
			if signal == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("internalSignals should contain %q", expected)
		}
	}
}

// TestCleanInternalSignalsWithSeparators tests separator handling
func TestCleanInternalSignalsWithSeparators(t *testing.T) {
	input := `Before content
━━━━━━━━━━
More text
━━━━━━━━━━
After content`

	got := cleanInternalSignals(input)

	// Separators should be removed
	if strings.Contains(got, "━━━━━━━━━━") {
		t.Errorf("cleanInternalSignals should remove separators, got:\n%s", got)
	}
}

// TestExtractSummary tests summary extraction from output text
func TestExtractSummary(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		empty    bool
	}{
		{
			name:  "empty input",
			input: "",
			empty: true,
		},
		{
			name:     "finds created files",
			input:    "Created `handler.go` in internal/",
			contains: []string{"📁 Created:", "handler.go"},
		},
		{
			name:     "finds modified files",
			input:    "Modified main.go",
			contains: []string{"📝 Modified:", "main.go"},
		},
		{
			name:     "finds added files",
			input:    "Added new feature to app.tsx",
			contains: []string{"➕ Added:", "app.tsx"},
		},
		{
			name:     "multiple patterns",
			input:    "Created auth.go\nModified config.go",
			contains: []string{"auth.go", "config.go"},
		},
		{
			name:  "no matches",
			input: "Some random text without file operations",
			empty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSummary(tt.input)
			if tt.empty {
				if got != "" {
					t.Errorf("extractSummary() = %q, want empty", got)
				}
				return
			}
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("extractSummary() = %q, want to contain %q", got, want)
				}
			}
		})
	}
}

// TestExtractSummaryPatterns tests all extraction patterns
func TestExtractSummaryPatterns(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{
			name:     "created pattern",
			input:    "Created `newfile.go`",
			contains: []string{"📁 Created:", "newfile.go"},
		},
		{
			name:     "modified pattern",
			input:    "Modified existing.go",
			contains: []string{"📝 Modified:", "existing.go"},
		},
		{
			name:     "added pattern",
			input:    "Added feature.ts to the project",
			contains: []string{"➕ Added:", "feature.ts"},
		},
		{
			name:     "deleted pattern",
			input:    "Deleted old_file.txt",
			contains: []string{"🗑 Deleted:", "old_file.txt"},
		},
		{
			name:     "multiple files",
			input:    "Created a.go\nModified b.go\nAdded c.go",
			contains: []string{"a.go", "b.go", "c.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSummary(tt.input)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("extractSummary() = %q, want to contain %q", got, want)
				}
			}
		})
	}
}

// TestExtractSummaryLimit tests the 5-item limit
func TestExtractSummaryLimit(t *testing.T) {
	// The function extracts max 5 matches per pattern, and we have multiple patterns
	// With only one pattern type, we get max 5 items
	input := `Created a.go
Created b.go
Created c.go
Created d.go
Created e.go
Created f.go
Created g.go`

	got := extractSummary(input)

	// Count Created entries - should be 5 (max per pattern)
	count := strings.Count(got, "Created:")
	if count != 5 {
		t.Errorf("extractSummary() has %d items, want 5", count)
	}
}
