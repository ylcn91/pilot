package telegram

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFastListTasksEmpty tests fast list when no tasks directory
func TestFastListTasksEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	h := &Handler{
		projectPath: tmpDir,
	}

	result := h.fastListTasks()
	if result != "" {
		t.Errorf("expected empty result for non-existent tasks dir, got %q", result)
	}
}

// TestFastListTasksWithTasks tests fast list with actual tasks
func TestFastListTasksWithTasks(t *testing.T) {
	tmpDir := t.TempDir()
	tasksDir := filepath.Join(tmpDir, ".agent", "tasks")
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create task files
	tasks := []struct {
		filename string
		content  string
	}{
		{
			"TASK-01-done.md",
			"# TASK-01: Done Task\n**Status**: complete\n",
		},
		{
			"TASK-02-progress.md",
			"# TASK-02: In Progress\n**Status**: in-progress\n",
		},
		{
			"TASK-03-pending.md",
			"# TASK-03: Pending Task\n**Status**: backlog\n",
		},
	}

	for _, task := range tasks {
		if err := os.WriteFile(filepath.Join(tasksDir, task.filename), []byte(task.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	h := &Handler{
		projectPath: tmpDir,
	}

	result := h.fastListTasks()

	if result == "" {
		t.Error("expected non-empty result")
	}

	// Should contain sections
	wantContains := []string{"In Progress", "Backlog", "Recently done", "Progress:"}
	for _, want := range wantContains {
		if !containsSubstr(result, want) {
			t.Errorf("result should contain %q, got:\n%s", want, result)
		}
	}
}

// TestFastReadStatusEmpty tests status reading when file doesn't exist
func TestFastReadStatusEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	h := &Handler{
		projectPath: tmpDir,
	}

	result := h.fastReadStatus()
	if result != "" {
		t.Errorf("expected empty result for non-existent readme, got %q", result)
	}
}

// TestFastGrepTodosNoFiles tests grep when no source files
func TestFastGrepTodosNoFiles(t *testing.T) {
	tmpDir := t.TempDir()
	h := &Handler{
		projectPath: tmpDir,
	}

	result := h.fastGrepTodos()
	if !containsSubstr(result, "No TODOs") {
		t.Errorf("expected 'No TODOs' message, got %q", result)
	}
}

// TestFastGrepTodosWithTodos tests grep with actual TODO comments
func TestFastGrepTodosWithTodos(t *testing.T) {
	tmpDir := t.TempDir()
	cmdDir := filepath.Join(tmpDir, "cmd")
	if err := os.MkdirAll(cmdDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := `package main

// TODO: implement this feature
func main() {
	// FIXME: broken code here
}`

	if err := os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	h := &Handler{
		projectPath: tmpDir,
	}

	result := h.fastGrepTodos()

	wantContains := []string{"TODO", "FIXME", "cmd/main.go"}
	for _, want := range wantContains {
		if !containsSubstr(result, want) {
			t.Errorf("result should contain %q, got:\n%s", want, result)
		}
	}
}

// TestTryFastAnswer tests the fast answer path
func TestTryFastAnswer(t *testing.T) {
	tmpDir := t.TempDir()
	h := &Handler{
		projectPath: tmpDir,
	}

	tests := []struct {
		name      string
		question  string
		wantEmpty bool
	}{
		{
			name:      "tasks question",
			question:  "what tasks are there",
			wantEmpty: true, // No tasks dir, so returns empty
		},
		{
			name:      "status question",
			question:  "what is the current status",
			wantEmpty: true, // No readme, so returns empty
		},
		{
			name:      "todos question",
			question:  "show me all todos",
			wantEmpty: false, // Returns "No TODOs" message
		},
		{
			name:      "unrelated question",
			question:  "how do I run tests",
			wantEmpty: true, // Falls back to Claude
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.tryFastAnswer(tt.question)
			isEmpty := got == ""
			if isEmpty != tt.wantEmpty {
				t.Errorf("tryFastAnswer(%q) isEmpty = %v, want %v (got: %q)", tt.question, isEmpty, tt.wantEmpty, got)
			}
		})
	}
}

// TestFastReadStatusWithContent tests status reading with valid content
func TestFastReadStatusWithContent(t *testing.T) {
	tmpDir := t.TempDir()
	agentDir := filepath.Join(tmpDir, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := `# Development README

## Current State

| Component | Status |
|-----------|--------|
| Gateway | Complete |
| Adapter | In Progress |

## Active Tasks
- Task 1
- Task 2
`

	if err := os.WriteFile(filepath.Join(agentDir, "DEVELOPMENT-README.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	h := &Handler{
		projectPath: tmpDir,
	}

	result := h.fastReadStatus()

	if result == "" {
		t.Error("expected non-empty result")
	}
	if !containsSubstr(result, "Project Status") {
		t.Errorf("result should contain 'Project Status', got:\n%s", result)
	}
}

// TestFastReadStatusNoRelevantSection tests status with no relevant sections
func TestFastReadStatusNoRelevantSection(t *testing.T) {
	tmpDir := t.TempDir()
	agentDir := filepath.Join(tmpDir, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := `# Development README

Just some random content without any status sections.
`

	if err := os.WriteFile(filepath.Join(agentDir, "DEVELOPMENT-README.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	h := &Handler{
		projectPath: tmpDir,
	}

	result := h.fastReadStatus()

	// Should return empty because no relevant sections found
	if result != "" {
		t.Errorf("expected empty result for no relevant sections, got:\n%s", result)
	}
}
