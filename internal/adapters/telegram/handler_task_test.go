package telegram

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveTaskID tests task ID resolution from user input
func TestResolveTaskID(t *testing.T) {
	// Create temp directory with task files
	tmpDir := t.TempDir()
	tasksDir := filepath.Join(tmpDir, ".agent", "tasks")
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create test task files
	task07Content := `# TASK-07: Test Voice Support
**Status**: backlog

Description here.`
	task12Content := `# TASK-12: Another Task
**Status**: in-progress

Description here.`

	if err := os.WriteFile(filepath.Join(tasksDir, "TASK-07-voice-support.md"), []byte(task07Content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tasksDir, "TASK-12-another.md"), []byte(task12Content), 0644); err != nil {
		t.Fatal(err)
	}

	h := &Handler{
		projectPath: tmpDir,
	}

	tests := []struct {
		name       string
		input      string
		wantID     string
		wantFullID string
		wantNil    bool
	}{
		{
			name:       "simple number",
			input:      "7",
			wantID:     "07",
			wantFullID: "TASK-07",
		},
		{
			name:       "padded number",
			input:      "07",
			wantID:     "07",
			wantFullID: "TASK-07",
		},
		{
			name:       "with TASK prefix",
			input:      "TASK-07",
			wantID:     "07",
			wantFullID: "TASK-07",
		},
		{
			name:       "lowercase task prefix",
			input:      "task-12",
			wantID:     "12",
			wantFullID: "TASK-12",
		},
		{
			name:       "with space",
			input:      "task 7",
			wantID:     "07",
			wantFullID: "TASK-07",
		},
		{
			name:       "with hash",
			input:      "#12",
			wantID:     "12",
			wantFullID: "TASK-12",
		},
		{
			name:    "non-existent task",
			input:   "99",
			wantNil: true,
		},
		{
			name:    "invalid input",
			input:   "abc",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.resolveTaskID(tt.input)

			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}

			if got == nil {
				t.Fatal("expected non-nil TaskInfo")
			}

			if got.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", got.ID, tt.wantID)
			}
			if got.FullID != tt.wantFullID {
				t.Errorf("FullID = %q, want %q", got.FullID, tt.wantFullID)
			}
		})
	}
}

// TestResolveTaskFromDescription tests extracting task ID from natural language
func TestResolveTaskFromDescription(t *testing.T) {
	tmpDir := t.TempDir()
	tasksDir := filepath.Join(tmpDir, ".agent", "tasks")
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		t.Fatal(err)
	}

	task07Content := `# TASK-07: Test Task
**Status**: backlog`

	if err := os.WriteFile(filepath.Join(tasksDir, "TASK-07-test.md"), []byte(task07Content), 0644); err != nil {
		t.Fatal(err)
	}

	h := &Handler{
		projectPath: tmpDir,
	}

	tests := []struct {
		name        string
		description string
		wantFullID  string
		wantNil     bool
	}{
		{
			name:        "start task",
			description: "start task 07",
			wantFullID:  "TASK-07",
		},
		{
			name:        "run task",
			description: "run task 7",
			wantFullID:  "TASK-07",
		},
		{
			name:        "execute task",
			description: "execute task-07",
			wantFullID:  "TASK-07",
		},
		{
			name:        "just number",
			description: "07",
			wantFullID:  "TASK-07",
		},
		{
			name:        "do number",
			description: "do 7",
			wantFullID:  "TASK-07",
		},
		{
			name:        "no task reference",
			description: "create a new file",
			wantNil:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.resolveTaskFromDescription(tt.description)

			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}

			if got == nil {
				t.Fatal("expected non-nil TaskInfo")
			}

			if got.FullID != tt.wantFullID {
				t.Errorf("FullID = %q, want %q", got.FullID, tt.wantFullID)
			}
		})
	}
}

// TestLoadTaskDescription tests loading full task content
func TestLoadTaskDescription(t *testing.T) {
	tmpDir := t.TempDir()
	tasksDir := filepath.Join(tmpDir, ".agent", "tasks")
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		t.Fatal(err)
	}

	taskContent := `# TASK-01: Test Task
**Status**: backlog

Full task description here.`

	taskFile := filepath.Join(tasksDir, "TASK-01-test.md")
	if err := os.WriteFile(taskFile, []byte(taskContent), 0644); err != nil {
		t.Fatal(err)
	}

	h := &Handler{
		projectPath: tmpDir,
	}

	tests := []struct {
		name     string
		taskInfo *TaskInfo
		wantLen  int
		wantZero bool
	}{
		{
			name: "valid task info",
			taskInfo: &TaskInfo{
				ID:       "01",
				FullID:   "TASK-01",
				FilePath: taskFile,
			},
			wantLen: len(taskContent),
		},
		{
			name:     "nil task info",
			taskInfo: nil,
			wantZero: true,
		},
		{
			name: "empty file path",
			taskInfo: &TaskInfo{
				ID:       "01",
				FullID:   "TASK-01",
				FilePath: "",
			},
			wantZero: true,
		},
		{
			name: "non-existent file",
			taskInfo: &TaskInfo{
				ID:       "99",
				FullID:   "TASK-99",
				FilePath: "/nonexistent/file.md",
			},
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.loadTaskDescription(tt.taskInfo)

			if tt.wantZero {
				if got != "" {
					t.Errorf("expected empty string, got %q", got)
				}
				return
			}

			if len(got) != tt.wantLen {
				t.Errorf("description length = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

// TestResolveTaskIDWithVariousFormats tests more format variations
func TestResolveTaskIDWithVariousFormats(t *testing.T) {
	tmpDir := t.TempDir()
	tasksDir := filepath.Join(tmpDir, ".agent", "tasks")
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create task with different naming convention
	task5Content := `# TASK-5: Short ID Task
**Status**: backlog`

	if err := os.WriteFile(filepath.Join(tasksDir, "TASK-5-short.md"), []byte(task5Content), 0644); err != nil {
		t.Fatal(err)
	}

	h := &Handler{
		projectPath: tmpDir,
	}

	tests := []struct {
		input   string
		wantNil bool
	}{
		{"5", false},
		{"05", false},
		{"task-5", false},
		{"TASK-5", false},
		{" 5 ", false}, // with whitespace
		{"task 5", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := h.resolveTaskID(tt.input)
			if tt.wantNil && got != nil {
				t.Errorf("expected nil for %q", tt.input)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("expected non-nil for %q", tt.input)
			}
		})
	}
}

// TestResolveTaskIDNoTasksDir tests when tasks directory doesn't exist
func TestResolveTaskIDNoTasksDir(t *testing.T) {
	h := &Handler{
		projectPath: "/nonexistent/path",
	}

	got := h.resolveTaskID("07")
	if got != nil {
		t.Error("expected nil when tasks dir doesn't exist")
	}
}

// TestResolveTaskFromDescriptionVariations tests more description patterns
func TestResolveTaskFromDescriptionVariations(t *testing.T) {
	tmpDir := t.TempDir()
	tasksDir := filepath.Join(tmpDir, ".agent", "tasks")
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		t.Fatal(err)
	}

	task3Content := `# TASK-03: Test Task
**Status**: backlog`
	if err := os.WriteFile(filepath.Join(tasksDir, "TASK-03-test.md"), []byte(task3Content), 0644); err != nil {
		t.Fatal(err)
	}

	h := &Handler{
		projectPath: tmpDir,
	}

	tests := []struct {
		desc    string
		wantNil bool
	}{
		{"execute 3", false},
		{"run task 03", false},
		{"start task-03", false},
		{"do 3", false},
		{"please help with something", true}, // no task reference
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := h.resolveTaskFromDescription(tt.desc)
			if tt.wantNil && got != nil {
				t.Errorf("expected nil for %q", tt.desc)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("expected non-nil for %q", tt.desc)
			}
		})
	}
}
