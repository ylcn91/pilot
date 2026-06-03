package executor

import (
	"testing"
)

func TestFormatToolMessage(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    map[string]interface{}
		want     string
	}{
		{
			name:     "Write tool",
			toolName: "Write",
			input:    map[string]interface{}{"file_path": "/path/to/file.go"},
			want:     "Writing file.go",
		},
		{
			name:     "Bash tool",
			toolName: "Bash",
			input:    map[string]interface{}{"command": "go test ./..."},
			want:     "Running: go test ./...",
		},
		{
			name:     "Read tool",
			toolName: "Read",
			input:    map[string]interface{}{"file_path": "/src/main.go"},
			want:     "Reading main.go",
		},
		{
			name:     "Unknown tool",
			toolName: "CustomTool",
			input:    map[string]interface{}{},
			want:     "Using CustomTool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatToolMessage(tt.toolName, tt.input)
			if got != tt.want {
				t.Errorf("formatToolMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncateText(t *testing.T) {
	tests := []struct {
		text   string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"this is a longer text", 10, "this is..."},
		{"with\nnewlines", 20, "with newlines"},
	}

	for _, tt := range tests {
		got := truncateText(tt.text, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateText(%q, %d) = %q, want %q", tt.text, tt.maxLen, got, tt.want)
		}
	}
}

func TestFormatToolMessageAdditional(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    map[string]interface{}
		want     string
	}{
		{
			name:     "Edit tool",
			toolName: "Edit",
			input:    map[string]interface{}{"file_path": "/src/main.go"},
			want:     "Editing main.go",
		},
		{
			name:     "Glob tool",
			toolName: "Glob",
			input:    map[string]interface{}{"pattern": "**/*.ts"},
			want:     "Searching: **/*.ts",
		},
		{
			name:     "Grep tool",
			toolName: "Grep",
			input:    map[string]interface{}{"pattern": "TODO"},
			want:     "Grep: TODO",
		},
		{
			name:     "Task tool",
			toolName: "Task",
			input:    map[string]interface{}{"description": "Run linter"},
			want:     "Spawning: Run linter",
		},
		{
			name:     "Bash long command",
			toolName: "Bash",
			input:    map[string]interface{}{"command": "this is a very long command that should be truncated"},
			want:     "Running: this is a very long command that shou...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatToolMessage(tt.toolName, tt.input)
			if got != tt.want {
				t.Errorf("formatToolMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStreamEventStructs(t *testing.T) {
	event := StreamEvent{
		Type:    "assistant",
		Subtype: "message",
		Message: &AssistantMsg{
			Content: []ContentBlock{
				{Type: "text", Text: "Hello"},
				{Type: "tool_use", Name: "Read", Input: map[string]interface{}{"file_path": "/test.go"}},
			},
		},
		Result:  "",
		IsError: false,
		Usage: &UsageInfo{
			InputTokens:  100,
			OutputTokens: 50,
		},
		Model: "claude-sonnet-4-6",
	}

	if event.Type != "assistant" {
		t.Errorf("Type = %q, want assistant", event.Type)
	}
	if len(event.Message.Content) != 2 {
		t.Errorf("Content length = %d, want 2", len(event.Message.Content))
	}
	if event.Usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", event.Usage.InputTokens)
	}
}

func TestToolResultContentStruct(t *testing.T) {
	result := ToolResultContent{
		ToolUseID: "tool-123",
		Type:      "tool_result",
		Content:   "[main abc1234] feat: add feature",
		IsError:   false,
	}

	if result.ToolUseID != "tool-123" {
		t.Errorf("ToolUseID = %q, want tool-123", result.ToolUseID)
	}
	if result.IsError {
		t.Error("IsError should be false")
	}
}
