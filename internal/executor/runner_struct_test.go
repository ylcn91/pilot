package executor

import (
	"testing"
)

func TestExtractCommitSHA(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "standard commit output",
			content:  "[main abc1234] feat: add feature",
			expected: []string{"abc1234"},
		},
		{
			name:     "branch with slash",
			content:  "[pilot/TASK-123 def5678] fix: bug fix",
			expected: []string{"def5678"},
		},
		{
			name:     "full SHA",
			content:  "[main abc1234567890abcdef1234567890abcdef12] commit msg",
			expected: []string{"abc1234567890abcdef1234567890abcdef12"},
		},
		{
			name:     "multiline with commit",
			content:  "Some output\n[feature/test 1234567] test commit\nMore output",
			expected: []string{"1234567"},
		},
		{
			name:     "no commit",
			content:  "Just some random output",
			expected: nil,
		},
		{
			name:     "invalid SHA format",
			content:  "[main not-a-sha] message",
			expected: nil,
		},
		{
			name:     "multiple commits",
			content:  "[main abc1234] first\n[main def5678] second",
			expected: []string{"abc1234", "def5678"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &progressState{}
			extractCommitSHA(tt.content, state)

			if len(tt.expected) == 0 && len(state.commitSHAs) > 0 {
				t.Errorf("expected no SHAs, got %v", state.commitSHAs)
			}
			if len(tt.expected) > 0 {
				if len(state.commitSHAs) != len(tt.expected) {
					t.Errorf("expected %d SHAs, got %d: %v", len(tt.expected), len(state.commitSHAs), state.commitSHAs)
				}
				for i, sha := range tt.expected {
					if i < len(state.commitSHAs) && state.commitSHAs[i] != sha {
						t.Errorf("SHA[%d] = %q, want %q", i, state.commitSHAs[i], sha)
					}
				}
			}
		})
	}
}

func TestIsValidSHA(t *testing.T) {
	tests := []struct {
		sha   string
		valid bool
	}{
		{"abc1234", true},
		{"ABC1234", true},
		{"1234567890abcdef1234567890abcdef12345678", true},
		{"abc123", false},  // too short
		{"not-sha", false}, // invalid chars
		{"", false},
		{"abc1234567890abcdef1234567890abcdef123456789", false}, // too long (41 chars)
	}

	for _, tt := range tests {
		t.Run(tt.sha, func(t *testing.T) {
			if got := isValidSHA(tt.sha); got != tt.valid {
				t.Errorf("isValidSHA(%q) = %v, want %v", tt.sha, got, tt.valid)
			}
		})
	}
}

func TestTaskStruct(t *testing.T) {
	tests := []struct {
		name string
		task *Task
	}{
		{
			name: "full task",
			task: &Task{
				ID:          "TASK-123",
				Title:       "Add authentication",
				Description: "Implement OAuth2 flow",
				Priority:    1,
				ProjectPath: "/path/to/project",
				Branch:      "pilot/TASK-123",
				Verbose:     true,
				CreatePR:    true,
				BaseBranch:  "main",
				ImagePath:   "",
			},
		},
		{
			name: "minimal task",
			task: &Task{
				ID:          "T-1",
				Description: "Fix bug",
				ProjectPath: "/tmp/proj",
			},
		},
		{
			name: "image task",
			task: &Task{
				ID:          "IMG-1",
				Description: "Analyze screenshot",
				ProjectPath: "/tmp/proj",
				ImagePath:   "/tmp/screenshot.png",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.task.ID == "" {
				t.Error("Task ID should not be empty")
			}
			if tt.task.ProjectPath == "" {
				t.Error("ProjectPath should not be empty")
			}
		})
	}
}

func TestExecutionResultStruct(t *testing.T) {
	result := &ExecutionResult{
		TaskID:           "TASK-123",
		Success:          true,
		Output:           "Task completed successfully",
		Error:            "",
		Duration:         5000000000, // 5 seconds
		PRUrl:            "https://github.com/org/repo/pull/42",
		CommitSHA:        "abc1234",
		TokensInput:      1000,
		TokensOutput:     500,
		TokensTotal:      1500,
		EstimatedCostUSD: 0.015,
		FilesChanged:     3,
		LinesAdded:       100,
		LinesRemoved:     20,
		ModelName:        "claude-sonnet-4-6",
	}

	if result.TaskID != "TASK-123" {
		t.Errorf("TaskID = %q, want TASK-123", result.TaskID)
	}
	if !result.Success {
		t.Error("Success should be true")
	}
	if result.TokensTotal != 1500 {
		t.Errorf("TokensTotal = %d, want 1500", result.TokensTotal)
	}
	if result.CommitSHA != "abc1234" {
		t.Errorf("CommitSHA = %q, want abc1234", result.CommitSHA)
	}
}

func TestMinFunction(t *testing.T) {
	tests := []struct {
		a, b     int
		expected int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{0, 0, 0},
		{-1, 1, -1},
		{100, 50, 50},
		{-10, -20, -20},
	}

	for _, tt := range tests {
		result := min(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("min(%d, %d) = %d, want %d", tt.a, tt.b, result, tt.expected)
		}
	}
}
