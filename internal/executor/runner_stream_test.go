package executor

import (
	"testing"
)

func TestParseStreamEvent(t *testing.T) {
	runner := NewRunner()

	// Track progress calls
	var progressCalls []struct {
		phase   string
		message string
	}
	runner.OnProgress(func(taskID, phase string, progress int, message string) {
		progressCalls = append(progressCalls, struct {
			phase   string
			message string
		}{phase, message})
	})

	tests := []struct {
		name          string
		json          string
		wantResult    string
		wantError     string
		wantProgress  bool
		expectedPhase string
	}{
		{
			name:          "system init",
			json:          `{"type":"system","subtype":"init","session_id":"abc"}`,
			wantProgress:  true,
			expectedPhase: "🚀 Started",
		},
		{
			name:          "tool use Write triggers Implementing phase",
			json:          `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"/tmp/test.go"}}]}}`,
			wantProgress:  true,
			expectedPhase: "Implementing",
		},
		{
			name:          "tool use Read triggers Exploring phase",
			json:          `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/tmp/test.go"}}]}}`,
			wantProgress:  true,
			expectedPhase: "Exploring",
		},
		{
			name:          "git commit triggers Committing phase",
			json:          `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"git commit -m 'test'"}}]}}`,
			wantProgress:  true,
			expectedPhase: "Committing",
		},
		{
			name:       "result success",
			json:       `{"type":"result","subtype":"success","result":"Done!","is_error":false}`,
			wantResult: "Done!",
		},
		{
			name:      "result error",
			json:      `{"type":"result","subtype":"error","result":"Failed","is_error":true}`,
			wantError: "Failed",
		},
		{
			name:         "invalid json",
			json:         `not valid json`,
			wantProgress: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			progressCalls = nil
			state := &progressState{phase: "Starting"}

			result, errMsg := runner.parseStreamEvent("TASK-1", tt.json, state)

			if result != tt.wantResult {
				t.Errorf("result = %q, want %q", result, tt.wantResult)
			}
			if errMsg != tt.wantError {
				t.Errorf("error = %q, want %q", errMsg, tt.wantError)
			}
			if tt.wantProgress && len(progressCalls) == 0 {
				t.Error("expected progress call, got none")
			}
			if tt.expectedPhase != "" && len(progressCalls) > 0 {
				if progressCalls[0].phase != tt.expectedPhase {
					t.Errorf("phase = %q, want %q", progressCalls[0].phase, tt.expectedPhase)
				}
			}
		})
	}
}

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

func TestParseStreamEventUsageTracking(t *testing.T) {
	runner := NewRunner()
	state := &progressState{phase: "Starting"}

	// Event with usage info
	jsonEvent := `{"type":"assistant","message":{"content":[]},"usage":{"input_tokens":100,"output_tokens":50},"model":"claude-sonnet-4-6"}`

	runner.parseStreamEvent("TASK-1", jsonEvent, state)

	if state.tokensInput != 100 {
		t.Errorf("tokensInput = %d, want 100", state.tokensInput)
	}
	if state.tokensOutput != 50 {
		t.Errorf("tokensOutput = %d, want 50", state.tokensOutput)
	}
	if state.modelName != "claude-sonnet-4-6" {
		t.Errorf("modelName = %q, want claude-sonnet-4-6", state.modelName)
	}
}

func TestParseStreamEventEmptyJSON(t *testing.T) {
	runner := NewRunner()
	state := &progressState{phase: "Starting"}

	// Empty object should not panic
	result, errMsg := runner.parseStreamEvent("TASK-1", "{}", state)

	if result != "" {
		t.Errorf("result should be empty, got %q", result)
	}
	if errMsg != "" {
		t.Errorf("errMsg should be empty, got %q", errMsg)
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
