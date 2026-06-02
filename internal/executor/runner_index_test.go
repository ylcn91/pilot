package executor

import (
	"os"
	"strings"
	"testing"
)

func TestExtractTaskNumber(t *testing.T) {
	tests := []struct {
		taskID   string
		expected string
	}{
		{"GH-57", "57"},
		{"GH-123", "123"},
		{"TASK-42", "42"},
		{"TASK-999", "999"},
		{"OTHER-1", "OTHER-1"},
		{"57", "57"},
	}

	for _, tt := range tests {
		t.Run(tt.taskID, func(t *testing.T) {
			result := extractTaskNumber(tt.taskID)
			if result != tt.expected {
				t.Errorf("extractTaskNumber(%q) = %q, want %q", tt.taskID, result, tt.expected)
			}
		})
	}
}

func TestContainsTaskNumber(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		taskNum  string
		expected bool
	}{
		{"pipe spaced", "| 57 | Title | Status |", "57", true},
		{"no space", "|57 | Title |", "57", true},
		{"GH format", "| GH-57 | Title |", "57", true},
		{"TASK format", "| TASK-57 | Title |", "57", true},
		{"different number", "| 58 | Title |", "57", false},
		{"partial match", "| 157 | Title |", "57", false},
		{"in text", "Task GH-57 is done", "57", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsTaskNumber(tt.line, tt.taskNum)
			if result != tt.expected {
				t.Errorf("containsTaskNumber(%q, %q) = %v, want %v", tt.line, tt.taskNum, result, tt.expected)
			}
		})
	}
}

func TestSyncNavigatorIndex(t *testing.T) {
	runner := NewRunner()

	// Create temp directory with Navigator structure
	tmpDir := t.TempDir()
	agentDir := tmpDir + "/.agent"
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("failed to create .agent dir: %v", err)
	}

	// Create sample DEVELOPMENT-README.md
	indexContent := `# Development Navigator

## Active Work

### In Progress

| GH# | Title | Status |
|-----|-------|--------|
| 57 | Navigator Index Auto-Sync | 🔄 Pilot executing |
| 58 | Other Task | 🔄 Pilot executing |

### Backlog

| Priority | Topic |
|----------|-------|
| P1 | Future work |

## Completed (2026-01-28)

| Item | What |
|------|------|
| GH-52 | Previous task |
`

	indexPath := agentDir + "/DEVELOPMENT-README.md"
	if err := os.WriteFile(indexPath, []byte(indexContent), 0644); err != nil {
		t.Fatalf("failed to write index: %v", err)
	}

	// Test sync
	task := &Task{
		ID:          "GH-57",
		Title:       "Navigator Index Auto-Sync",
		ProjectPath: tmpDir,
	}

	err := runner.syncNavigatorIndex(task, "completed", task.ProjectPath)
	if err != nil {
		t.Fatalf("syncNavigatorIndex failed: %v", err)
	}

	// Read updated index
	updatedContent, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("failed to read updated index: %v", err)
	}

	contentStr := string(updatedContent)

	// Task should be removed from In Progress
	if strings.Contains(contentStr, "| 57 | Navigator Index Auto-Sync | 🔄 Pilot executing |") {
		t.Error("Task should be removed from In Progress section")
	}

	// Task should be in Completed section
	if !strings.Contains(contentStr, "| GH-57 | Navigator Index Auto-Sync |") {
		t.Error("Task should be added to Completed section")
	}

	// Other task should still be in progress
	if !strings.Contains(contentStr, "| 58 | Other Task | 🔄 Pilot executing |") {
		t.Error("Other tasks should remain in In Progress")
	}
}

func TestSyncNavigatorIndexNoIndex(t *testing.T) {
	runner := NewRunner()

	// Test with non-existent index
	task := &Task{
		ID:          "GH-99",
		ProjectPath: t.TempDir(),
	}

	err := runner.syncNavigatorIndex(task, "completed", task.ProjectPath)
	if err != nil {
		t.Errorf("syncNavigatorIndex should not error for missing index: %v", err)
	}
}

func TestSyncNavigatorIndexTaskNotInProgress(t *testing.T) {
	runner := NewRunner()

	// Create temp directory with Navigator structure
	tmpDir := t.TempDir()
	agentDir := tmpDir + "/.agent"
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("failed to create .agent dir: %v", err)
	}

	// Create index without our task
	indexContent := `# Development Navigator

### In Progress

| GH# | Title | Status |
|-----|-------|--------|
| 58 | Other Task | 🔄 Pilot executing |

## Completed

| Item | What |
|------|------|
`

	indexPath := agentDir + "/DEVELOPMENT-README.md"
	if err := os.WriteFile(indexPath, []byte(indexContent), 0644); err != nil {
		t.Fatalf("failed to write index: %v", err)
	}

	// Test sync for task not in progress
	task := &Task{
		ID:          "GH-99",
		ProjectPath: tmpDir,
	}

	err := runner.syncNavigatorIndex(task, "completed", task.ProjectPath)
	if err != nil {
		t.Fatalf("syncNavigatorIndex failed: %v", err)
	}

	// Index should be unchanged
	updatedContent, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("failed to read updated index: %v", err)
	}

	if !strings.Contains(string(updatedContent), "| 58 | Other Task | 🔄 Pilot executing |") {
		t.Error("Index should remain unchanged when task not found")
	}
}
