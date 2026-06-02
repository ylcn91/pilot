package telegram

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPendingTask tests pending task management via commsHandler
func TestPendingTask(t *testing.T) {
	ch := newTestCommsHandler()

	// Initially no pending task
	if got := ch.GetPendingTask("chat1"); got != nil {
		t.Fatalf("expected no pending task, got %+v", got)
	}

	// Send a task message to trigger pending task creation
	// The commsHandler creates pending tasks via HandleMessage + intent detection.
	// For a focused unit test, verify GetPendingTask returns nil initially.
	// Full lifecycle is tested in comms/handler_test.go.
}

// TestExtractTaskNumber tests extracting task number from filename
func TestExtractTaskNumber(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"TASK-01-description.md", "01"},
		{"TASK-07-voice-support.md", "07"},
		{"TASK-12-another-task.md", "12"},
		{"task-99-test.md", "99"},
		{"TASK-123-long-number.md", "123"},
		{"README.md", ""},
		{"index.md", ""},
		{"TASK-.md", ""},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := extractTaskNumber(tt.filename)
			if got != tt.expected {
				t.Errorf("extractTaskNumber(%q) = %q, want %q", tt.filename, got, tt.expected)
			}
		})
	}
}

// TestParseTaskFile tests parsing task file metadata
func TestParseTaskFile(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name       string
		content    string
		wantStatus string
		wantTitle  string
	}{
		{
			name: "complete task",
			content: `# TASK-01: Complete Example
**Status**: complete

Description.`,
			wantStatus: "complete",
			wantTitle:  "Complete Example",
		},
		{
			name: "in progress task",
			content: `# TASK-02: In Progress Task
**Status**: in-progress

Description.`,
			wantStatus: "in-progress",
			wantTitle:  "In Progress Task",
		},
		{
			name: "backlog task",
			content: `# TASK-03: Backlog Task
**Status**: backlog

Description.`,
			wantStatus: "backlog",
			wantTitle:  "Backlog Task",
		},
		{
			name: "no explicit status field",
			content: `# TASK-04: Simple Task

Description only, no metadata.`,
			wantStatus: "pending",
			wantTitle:  "Simple Task",
		},
		{
			name: "emoji status",
			content: `# TASK-05: Done with Emoji
**Status**: done

Description.`,
			wantStatus: "done",
			wantTitle:  "Done with Emoji",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := filepath.Join(tmpDir, tt.name+".md")
			if err := os.WriteFile(filePath, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			status, title := parseTaskFile(filePath)

			if status != tt.wantStatus {
				t.Errorf("status = %q, want %q", status, tt.wantStatus)
			}
			if title != tt.wantTitle {
				t.Errorf("title = %q, want %q", title, tt.wantTitle)
			}
		})
	}
}

// TestParseTaskFileNonExistent tests parsing non-existent file
func TestParseTaskFileNonExistent(t *testing.T) {
	status, title := parseTaskFile("/nonexistent/file.md")

	if status != "pending" {
		t.Errorf("status = %q, want %q", status, "pending")
	}
	if title != "" {
		t.Errorf("title = %q, want empty", title)
	}
}

// TestRunningTask tests running task structure via commsHandler
func TestRunningTask(t *testing.T) {
	ch := newTestCommsHandler()

	// Initially no running task
	if got := ch.GetRunningTask("chat1"); got != nil {
		t.Fatalf("expected no running task, got %+v", got)
	}

	// Full lifecycle (execute → running → done) tested in comms/handler_test.go.
}

// TestPendingTaskStruct tests PendingTask struct
func TestPendingTaskStruct(t *testing.T) {
	now := time.Now()
	task := &PendingTask{
		TaskID:      "TASK-01",
		Description: "Test description",
		ChatID:      "12345",
		MessageID:   100,
		CreatedAt:   now,
	}

	if task.TaskID != "TASK-01" {
		t.Errorf("TaskID = %q, want TASK-01", task.TaskID)
	}
	if task.Description != "Test description" {
		t.Errorf("Description = %q, want Test description", task.Description)
	}
	if task.ChatID != "12345" {
		t.Errorf("ChatID = %q, want 12345", task.ChatID)
	}
	if task.MessageID != 100 {
		t.Errorf("MessageID = %d, want 100", task.MessageID)
	}
	if !task.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", task.CreatedAt, now)
	}
}

// TestTaskInfo tests TaskInfo struct
func TestTaskInfo(t *testing.T) {
	info := &TaskInfo{
		ID:       "07",
		FullID:   "TASK-07",
		Title:    "Voice Support",
		Status:   "backlog",
		FilePath: "/path/to/TASK-07.md",
	}

	if info.ID != "07" {
		t.Errorf("ID = %q, want 07", info.ID)
	}
	if info.FullID != "TASK-07" {
		t.Errorf("FullID = %q, want TASK-07", info.FullID)
	}
	if info.Title != "Voice Support" {
		t.Errorf("Title = %q, want Voice Support", info.Title)
	}
	if info.Status != "backlog" {
		t.Errorf("Status = %q, want backlog", info.Status)
	}
	if info.FilePath != "/path/to/TASK-07.md" {
		t.Errorf("FilePath = %q, want /path/to/TASK-07.md", info.FilePath)
	}
}

// TestParseTaskFileVariations tests more task file formats
func TestParseTaskFileVariations(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name       string
		content    string
		wantStatus string
	}{
		{
			name: "status with emoji",
			content: `# TASK-01: Task
**Status**: complete`,
			wantStatus: "complete",
		},
		{
			name: "status wip",
			content: `# TASK-02: Task
**Status**: wip`,
			wantStatus: "wip",
		},
		{
			name: "status done",
			content: `# TASK-03: Task
**Status**: done`,
			wantStatus: "done",
		},
		{
			name: "lowercase status",
			content: `# task-04: task
status: pending`,
			wantStatus: "pending",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := filepath.Join(tmpDir, tt.name+".md")
			if err := os.WriteFile(filePath, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			status, _ := parseTaskFile(filePath)
			if status != tt.wantStatus {
				t.Errorf("status = %q, want %q", status, tt.wantStatus)
			}
		})
	}
}
