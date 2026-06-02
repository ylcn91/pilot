package replay

import (
	"testing"
)

func TestFormatToolCallAllTools(t *testing.T) {
	tests := []struct {
		name     string
		parsed   *ParsedEvent
		contains string
	}{
		{
			name: "Read tool",
			parsed: &ParsedEvent{
				Type:      "assistant",
				ToolName:  "Read",
				ToolInput: map[string]any{"file_path": "/path/to/file.go"},
			},
			contains: "Read",
		},
		{
			name: "Write tool",
			parsed: &ParsedEvent{
				Type:      "assistant",
				ToolName:  "Write",
				ToolInput: map[string]any{"file_path": "/path/to/new.go"},
			},
			contains: "Write",
		},
		{
			name: "Edit tool",
			parsed: &ParsedEvent{
				Type:      "assistant",
				ToolName:  "Edit",
				ToolInput: map[string]any{"file_path": "/path/to/edit.go"},
			},
			contains: "Edit",
		},
		{
			name: "Bash tool",
			parsed: &ParsedEvent{
				Type:      "assistant",
				ToolName:  "Bash",
				ToolInput: map[string]any{"command": "go test ./..."},
			},
			contains: "Bash",
		},
		{
			name: "Glob tool",
			parsed: &ParsedEvent{
				Type:      "assistant",
				ToolName:  "Glob",
				ToolInput: map[string]any{"pattern": "**/*.go"},
			},
			contains: "Glob",
		},
		{
			name: "Grep tool",
			parsed: &ParsedEvent{
				Type:      "assistant",
				ToolName:  "Grep",
				ToolInput: map[string]any{"pattern": "funcName"},
			},
			contains: "Grep",
		},
		{
			name: "Task tool",
			parsed: &ParsedEvent{
				Type:      "assistant",
				ToolName:  "Task",
				ToolInput: map[string]any{"description": "Run tests"},
			},
			contains: "Task",
		},
		{
			name: "Skill tool",
			parsed: &ParsedEvent{
				Type:      "assistant",
				ToolName:  "Skill",
				ToolInput: map[string]any{"skill": "commit"},
			},
			contains: "Skill",
		},
		{
			name: "Unknown tool",
			parsed: &ParsedEvent{
				Type:      "assistant",
				ToolName:  "CustomTool",
				ToolInput: map[string]any{},
			},
			contains: "CustomTool",
		},
		{
			name: "Tool without detail",
			parsed: &ParsedEvent{
				Type:      "assistant",
				ToolName:  "Read",
				ToolInput: map[string]any{},
			},
			contains: "Read",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := formatToolCall(tc.parsed)
			if !containsString(result, tc.contains) {
				t.Errorf("Expected output to contain '%s', got: %s", tc.contains, result)
			}
		})
	}
}

func TestGetToolIcon(t *testing.T) {
	tests := []struct {
		tool     string
		expected string
	}{
		{"Read", "\U0001F4D6"},
		{"Write", "\u270F\uFE0F"},
		{"Edit", "\U0001F4DD"},
		{"Bash", "\U0001F4BB"},
		{"Glob", "\U0001F50D"},
		{"Grep", "\U0001F50E"},
		{"Task", "\U0001F916"},
		{"Skill", "\u26A1"},
		{"WebFetch", "\U0001F310"},
		{"WebSearch", "\U0001F50D"},
		{"UnknownTool", "\U0001F527"},
	}

	for _, tc := range tests {
		t.Run(tc.tool, func(t *testing.T) {
			icon := getToolIcon(tc.tool)
			if icon != tc.expected {
				t.Errorf("Expected icon '%s' for tool %s, got '%s'", tc.expected, tc.tool, icon)
			}
		})
	}
}

func TestDetectPhaseFromText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{
			name:     "research phase",
			text:     "Phase: RESEARCH starting",
			expected: "Research",
		},
		{
			name:     "implement phase",
			text:     "Phase: implementing now",
			expected: "Implementing",
		},
		{
			name:     "verify phase",
			text:     "Phase: verify code",
			expected: "Verifying",
		},
		{
			name:     "complete phase",
			text:     "Phase: complete now",
			expected: "Completing",
		},
		{
			name:     "init phase",
			text:     "Phase: init started",
			expected: "Init",
		},
		{
			name:     "loop mode",
			text:     "LOOP MODE ACTIVATED",
			expected: "Init",
		},
		{
			name:     "task mode",
			text:     "TASK MODE ACTIVATED",
			expected: "Init",
		},
		{
			name:     "no phase",
			text:     "Just some regular text",
			expected: "",
		},
		{
			name:     "phase prefix without match",
			text:     "Phase: random stuff",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := detectPhaseFromText(tc.text)
			if result != tc.expected {
				t.Errorf("Expected phase '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

func TestShortenPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "short path",
			path:     "a/b",
			expected: "a/b",
		},
		{
			name:     "three parts",
			path:     "a/b/c",
			expected: "a/b/c",
		},
		{
			name:     "long path",
			path:     "/home/user/projects/app/src/file.go",
			expected: ".../src/file.go",
		},
		{
			name:     "very long path",
			path:     "/a/b/c/d/e/f/g.txt",
			expected: ".../f/g.txt",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := shortenPath(tc.path)
			if result != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "short string",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "exact length",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "needs truncation",
			input:    "hello world this is long",
			maxLen:   10,
			expected: "hello w...",
		},
		{
			name:     "with newlines",
			input:    "hello\nworld",
			maxLen:   20,
			expected: "hello world",
		},
		{
			name:     "with whitespace",
			input:    "  hello  ",
			maxLen:   10,
			expected: "hello",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := truncate(tc.input, tc.maxLen)
			if result != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

func TestFormatToolDetail(t *testing.T) {
	tests := []struct {
		name     string
		parsed   *ParsedEvent
		expected string
	}{
		{
			name: "Read with file_path",
			parsed: &ParsedEvent{
				ToolName:  "Read",
				ToolInput: map[string]any{"file_path": "/path/to/file.go"},
			},
			expected: "/path/to/file.go",
		},
		{
			name: "Write with file_path",
			parsed: &ParsedEvent{
				ToolName:  "Write",
				ToolInput: map[string]any{"file_path": "/path/to/new.go"},
			},
			expected: "/path/to/new.go",
		},
		{
			name: "Edit with file_path",
			parsed: &ParsedEvent{
				ToolName:  "Edit",
				ToolInput: map[string]any{"file_path": "/path/to/edit.go"},
			},
			expected: "/path/to/edit.go",
		},
		{
			name: "Bash with command",
			parsed: &ParsedEvent{
				ToolName:  "Bash",
				ToolInput: map[string]any{"command": "go test ./..."},
			},
			expected: "go test ./...",
		},
		{
			name: "Glob with pattern",
			parsed: &ParsedEvent{
				ToolName:  "Glob",
				ToolInput: map[string]any{"pattern": "**/*.go"},
			},
			expected: "**/*.go",
		},
		{
			name: "Grep with pattern",
			parsed: &ParsedEvent{
				ToolName:  "Grep",
				ToolInput: map[string]any{"pattern": "funcName"},
			},
			expected: "funcName",
		},
		{
			name: "Unknown tool",
			parsed: &ParsedEvent{
				ToolName:  "CustomTool",
				ToolInput: map[string]any{"custom": "value"},
			},
			expected: "",
		},
		{
			name: "Read without file_path",
			parsed: &ParsedEvent{
				ToolName:  "Read",
				ToolInput: map[string]any{},
			},
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := formatToolDetail(tc.parsed)
			if result != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}
