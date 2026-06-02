package executor

import (
	"strings"
	"testing"
)

func TestExtractNumberedSteps(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name: "standard numbered list",
			text: `1. First item
2. Second item
3. Third item`,
			expected: 3,
		},
		{
			name: "parentheses format",
			text: `1) First item
2) Second item`,
			expected: 2,
		},
		{
			name: "step format",
			text: `Step 1: Do this
Step 2: Do that
Step 3: Finish up`,
			expected: 3,
		},
		{
			name:     "no numbered items",
			text:     "Just some plain text without numbers",
			expected: 0,
		},
		{
			name:     "single item",
			text:     "1. Only one item",
			expected: 0, // Need at least 2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractNumberedSteps(tt.text)
			if len(result) != tt.expected {
				t.Errorf("extractNumberedSteps() returned %d items, want %d", len(result), tt.expected)
			}
		})
	}
}

func TestExtractBulletPoints(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name: "dash bullets",
			text: `- First item
- Second item
- Third item`,
			expected: 3,
		},
		{
			name: "asterisk bullets",
			text: `* First item
* Second item`,
			expected: 2,
		},
		{
			name: "skip completed checkboxes",
			text: `- [x] Completed item
- [ ] Pending item
- [ ] Another pending`,
			expected: 2, // Only uncompleted items
		},
		{
			name:     "no bullets",
			text:     "Just plain text",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBulletPoints(tt.text)
			if len(result) != tt.expected {
				t.Errorf("extractBulletPoints() returned %d items, want %d", len(result), tt.expected)
			}
		})
	}
}

func TestExtractAcceptanceCriteria(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name: "checkbox criteria",
			text: `## Acceptance Criteria
[ ] First criterion
[ ] Second criterion
[ ] Third criterion`,
			expected: 3,
		},
		{
			name: "bullet checkbox criteria",
			text: `- [ ] First
- [ ] Second`,
			expected: 2,
		},
		{
			name:     "no criteria",
			text:     "No acceptance criteria here",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractAcceptanceCriteria(tt.text)
			if len(result) != tt.expected {
				t.Errorf("extractAcceptanceCriteria() returned %d items, want %d", len(result), tt.expected)
			}
		})
	}
}

func TestExtractFileGroups(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name:     "go files",
			text:     "Update internal/executor/runner.go and internal/executor/backend.go",
			expected: 2,
		},
		{
			name:     "mixed files",
			text:     "Modify src/component.tsx, api/handler.go, and test.py",
			expected: 3,
		},
		{
			name:     "no files",
			text:     "Just update the documentation",
			expected: 0,
		},
		{
			name:     "single file",
			text:     "Only change main.go",
			expected: 0, // Need at least 2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractFileGroups(tt.text)
			if len(result) != tt.expected {
				t.Errorf("extractFileGroups() returned %d items, want %d", len(result), tt.expected)
			}
		})
	}
}

func TestGenerateSubtaskID(t *testing.T) {
	tests := []struct {
		parentID string
		index    int
		expected string
	}{
		{"GH-150", 1, "GH-150-1"},
		{"GH-150", 2, "GH-150-2"},
		{"TASK-123", 10, "TASK-123-10"},
		{"TEST", 1, "TEST-1"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := generateSubtaskID(tt.parentID, tt.index)
			if result != tt.expected {
				t.Errorf("generateSubtaskID(%q, %d) = %q, want %q",
					tt.parentID, tt.index, result, tt.expected)
			}
		})
	}
}

func TestTruncateTitle(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"Short", 10, "Short"},
		{"This is a very long title", 15, "This is a ve..."},
		{"No truncation needed", 50, "No truncation needed"},
		{"Multi\nline\ntitle", 20, "Multi line title"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := truncateTitle(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncateTitle(%q, %d) = %q, want %q",
					tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestShouldDecompose(t *testing.T) {
	tests := []struct {
		name     string
		task     *Task
		config   *DecomposeConfig
		expected bool
	}{
		{
			name:     "nil config",
			task:     &Task{Description: "Some long description that should be complex enough"},
			config:   nil,
			expected: false,
		},
		{
			name:     "disabled config",
			task:     &Task{Description: "Some long description that should be complex enough"},
			config:   &DecomposeConfig{Enabled: false},
			expected: false,
		},
		{
			name: "complex task meets criteria",
			task: &Task{
				Description: strings.Repeat("word ", 60) + "refactor the system",
			},
			config: &DecomposeConfig{
				Enabled:             true,
				MinComplexity:       "complex",
				MinDescriptionWords: 50,
			},
			expected: true,
		},
		{
			name: "simple task",
			task: &Task{
				Description: "Fix typo in README",
			},
			config: &DecomposeConfig{
				Enabled:             true,
				MinComplexity:       "complex",
				MinDescriptionWords: 10,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldDecompose(tt.task, tt.config)
			if result != tt.expected {
				t.Errorf("ShouldDecompose() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBuildSubtaskDescription(t *testing.T) {
	parent := &Task{
		ID:    "GH-150",
		Title: "Parent Task Title",
	}

	desc := buildSubtaskDescription(parent, "Do something specific", 2, 5)

	// Check required elements
	if !strings.Contains(desc, "Subtask 2 of 5") {
		t.Error("Expected subtask numbering in description")
	}
	if !strings.Contains(desc, "GH-150") {
		t.Error("Expected parent ID in description")
	}
	if !strings.Contains(desc, "Parent Task Title") {
		t.Error("Expected parent title in description")
	}
	if !strings.Contains(desc, "Do something specific") {
		t.Error("Expected objective in description")
	}

	// Check final subtask note
	finalDesc := buildSubtaskDescription(parent, "Final step", 5, 5)
	if !strings.Contains(finalDesc, "final subtask") {
		t.Error("Expected final subtask note")
	}
}
