package executor

import (
	"testing"
)

func TestIsSinglePackageScope(t *testing.T) {
	tests := []struct {
		name        string
		subtasks    []PlannedSubtask
		description string
		expected    bool
	}{
		{
			name: "all files in same directory",
			subtasks: []PlannedSubtask{
				{Title: "Add onboard command skeleton", Description: "Create cmd/pilot/onboard.go with cobra command"},
				{Title: "Add onboard helpers", Description: "Create cmd/pilot/onboard_helpers.go with validation"},
				{Title: "Add onboard tests", Description: "Create cmd/pilot/onboard_test.go"},
			},
			description: "Implement pilot onboard command in cmd/pilot/",
			expected:    true,
		},
		{
			name: "files across different directories",
			subtasks: []PlannedSubtask{
				{Title: "Add database migration", Description: "Create internal/memory/store.go changes"},
				{Title: "Add API endpoint", Description: "Create internal/gateway/server.go handler"},
				{Title: "Add dashboard panel", Description: "Update internal/dashboard/tui.go"},
			},
			description: "Add user management across the stack",
			expected:    false,
		},
		{
			name: "no file references but same component in titles",
			subtasks: []PlannedSubtask{
				{Title: "onboard command skeleton and persona selection", Description: "Create the base command"},
				{Title: "onboard project setup stage", Description: "Add project configuration"},
				{Title: "onboard ticket source setup", Description: "Configure ticket sources"},
				{Title: "onboard notification setup", Description: "Set up notifications"},
				{Title: "onboard tests and deprecation", Description: "Add test coverage"},
			},
			description: "Implement interactive onboarding wizard",
			expected:    true, // "onboard" appears in >80% of titles
		},
		{
			name: "no file references and different components",
			subtasks: []PlannedSubtask{
				{Title: "Set up database schema", Description: "Create tables"},
				{Title: "Build authentication service", Description: "JWT tokens"},
				{Title: "Create frontend components", Description: "React forms"},
			},
			description: "Full-stack user auth",
			expected:    false,
		},
		{
			name: "single subtask always false (no conflict possible)",
			subtasks: []PlannedSubtask{
				{Title: "Do everything", Description: "Single task"},
			},
			description: "Simple task",
			expected:    false, // detectSameComponentFromTitles requires >=2
		},
		{
			name: "files in description only, same directory",
			subtasks: []PlannedSubtask{
				{Title: "Add types and constants", Description: "Define types"},
				{Title: "Add main logic", Description: "Implement core"},
			},
			description: "Changes to internal/executor/runner.go and internal/executor/complexity.go",
			expected:    true,
		},
		{
			name: "mixed: some files in same dir, task desc has different dir",
			subtasks: []PlannedSubtask{
				{Title: "Update runner", Description: "Modify internal/executor/runner.go"},
				{Title: "Update config", Description: "Modify internal/config/config.go"},
			},
			description: "Cross-cutting change",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSinglePackageScope(tt.subtasks, tt.description)
			if result != tt.expected {
				t.Errorf("isSinglePackageScope() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestExtractUniqueDirectories(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name:     "same directory",
			text:     "Update cmd/pilot/onboard.go and cmd/pilot/onboard_test.go",
			expected: 1,
		},
		{
			name:     "different directories",
			text:     "internal/executor/runner.go and internal/config/config.go",
			expected: 2,
		},
		{
			name:     "no file paths",
			text:     "Just some plain text without files",
			expected: 0,
		},
		{
			name:     "files without directory prefix",
			text:     "Update main.go and utils.go", // no slash → no directory extracted
			expected: 0,
		},
		{
			name:     "deeply nested same parent",
			text:     "internal/executor/runner.go internal/executor/epic.go internal/executor/decompose.go",
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dirs := extractUniqueDirectories(tt.text)
			if len(dirs) != tt.expected {
				t.Errorf("extractUniqueDirectories() returned %d dirs, want %d: %v", len(dirs), tt.expected, dirs)
			}
		})
	}
}

func TestDetectSameComponentFromTitles(t *testing.T) {
	tests := []struct {
		name     string
		subtasks []PlannedSubtask
		expected bool
	}{
		{
			name: "same component repeated",
			subtasks: []PlannedSubtask{
				{Title: "onboard command skeleton"},
				{Title: "onboard project setup"},
				{Title: "onboard ticket source"},
				{Title: "onboard notifications"},
				{Title: "onboard tests"},
			},
			expected: true,
		},
		{
			name: "different components",
			subtasks: []PlannedSubtask{
				{Title: "database migration"},
				{Title: "API endpoints"},
				{Title: "frontend components"},
			},
			expected: false,
		},
		{
			name: "common words don't count (stop words filtered)",
			subtasks: []PlannedSubtask{
				{Title: "Add the database layer"},
				{Title: "Create the API routes"},
				{Title: "Update the frontend"},
			},
			expected: false, // "the" and "add/create/update" are stop words
		},
		{
			name: "dashboard appears in all",
			subtasks: []PlannedSubtask{
				{Title: "dashboard layout component"},
				{Title: "dashboard data fetching"},
				{Title: "dashboard state management"},
			},
			expected: true,
		},
		{
			name:     "single subtask",
			subtasks: []PlannedSubtask{{Title: "only one"}},
			expected: false,
		},
		{
			name:     "empty list",
			subtasks: []PlannedSubtask{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectSameComponentFromTitles(tt.subtasks)
			if result != tt.expected {
				t.Errorf("detectSameComponentFromTitles() = %v, want %v", result, tt.expected)
			}
		})
	}
}
