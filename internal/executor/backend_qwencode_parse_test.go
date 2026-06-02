package executor

import (
	"testing"
)

func TestQwenCodeBackendParseStreamEvent(t *testing.T) {
	backend := NewQwenCodeBackend(nil)

	tests := []struct {
		name           string
		line           string
		expectType     BackendEventType
		expectTool     string
		expectError    bool
		expectSession  string
		expectMessage  string
		expectTokensIn int64
	}{
		{
			name:          "system init",
			line:          `{"type":"system","subtype":"init","session_id":"qwen-sess-123"}`,
			expectType:    EventTypeInit,
			expectSession: "qwen-sess-123",
		},
		{
			name:       "tool use read_file → Read",
			line:       `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"read_file","input":{"file_path":"/test.go"}}]}}`,
			expectType: EventTypeToolUse,
			expectTool: "Read",
		},
		{
			name:       "tool use write_file → Write",
			line:       `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"write_file","input":{"file_path":"/test.go","content":"hello"}}]}}`,
			expectType: EventTypeToolUse,
			expectTool: "Write",
		},
		{
			name:       "tool use run_shell_command → Bash",
			line:       `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"run_shell_command","input":{"command":"go test ./..."}}]}}`,
			expectType: EventTypeToolUse,
			expectTool: "Bash",
		},
		{
			name:       "tool use grep_search → Grep",
			line:       `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"grep_search","input":{"pattern":"func main"}}]}}`,
			expectType: EventTypeToolUse,
			expectTool: "Grep",
		},
		{
			name:       "tool use glob → Glob",
			line:       `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"glob","input":{"pattern":"**/*.go"}}]}}`,
			expectType: EventTypeToolUse,
			expectTool: "Glob",
		},
		{
			name:       "tool use edit → Edit",
			line:       `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"edit","input":{"file_path":"/test.go"}}]}}`,
			expectType: EventTypeToolUse,
			expectTool: "Edit",
		},
		{
			name:       "tool use list_directory → Bash",
			line:       `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"list_directory","input":{"path":"."}}]}}`,
			expectType: EventTypeToolUse,
			expectTool: "Bash",
		},
		{
			name:       "tool use task → Task (passthrough)",
			line:       `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"task","input":{"prompt":"research this"}}]}}`,
			expectType: EventTypeToolUse,
			expectTool: "Task",
		},
		{
			name:       "MCP tool passes through unchanged",
			line:       `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__github__list_issues","input":{}}]}}`,
			expectType: EventTypeToolUse,
			expectTool: "mcp__github__list_issues",
		},
		{
			name:          "text content",
			line:          `{"type":"assistant","message":{"content":[{"type":"text","text":"Analyzing the codebase..."}]}}`,
			expectType:    EventTypeText,
			expectMessage: "Analyzing the codebase...",
		},
		{
			name:       "result success",
			line:       `{"type":"result","result":"Task completed","is_error":false}`,
			expectType: EventTypeResult,
		},
		{
			name:        "result error",
			line:        `{"type":"result","result":"Failed to compile","is_error":true}`,
			expectType:  EventTypeResult,
			expectError: true,
		},
		{
			name:       "invalid json",
			line:       `not valid json at all`,
			expectType: EventTypeText,
		},
		{
			name:       "user tool_result block (Qwen-style)",
			line:       `{"type":"user","message":{"content":[{"type":"tool_result","text":"file contents here","is_error":false}]}}`,
			expectType: EventTypeToolResult,
		},
		{
			name:        "user tool_result with error",
			line:        `{"type":"user","message":{"content":[{"type":"tool_result","text":"permission denied","is_error":true}]}}`,
			expectType:  EventTypeToolResult,
			expectError: true,
		},
		{
			name:       "user tool_result with content field",
			line:       `{"type":"user","message":{"content":[{"type":"tool_result","content":"file contents here","is_error":false}]}}`,
			expectType: EventTypeToolResult,
		},
		{
			name:           "usage info extracted",
			line:           `{"type":"result","result":"Done","usage":{"input_tokens":5000,"output_tokens":2000},"model":"qwen3-coder-plus"}`,
			expectType:     EventTypeResult,
			expectTokensIn: 5000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := backend.parseStreamEvent(tt.line)

			if event.Type != tt.expectType {
				t.Errorf("Type = %q, want %q", event.Type, tt.expectType)
			}
			if tt.expectTool != "" && event.ToolName != tt.expectTool {
				t.Errorf("ToolName = %q, want %q", event.ToolName, tt.expectTool)
			}
			if tt.expectError && !event.IsError {
				t.Error("IsError should be true")
			}
			if tt.expectSession != "" && event.SessionID != tt.expectSession {
				t.Errorf("SessionID = %q, want %q", event.SessionID, tt.expectSession)
			}
			if tt.expectMessage != "" && event.Message != tt.expectMessage {
				t.Errorf("Message = %q, want %q", event.Message, tt.expectMessage)
			}
			if tt.expectTokensIn > 0 && event.TokensInput != tt.expectTokensIn {
				t.Errorf("TokensInput = %d, want %d", event.TokensInput, tt.expectTokensIn)
			}
			if event.Raw != tt.line {
				t.Errorf("Raw = %q, want %q", event.Raw, tt.line)
			}
		})
	}
}

func TestQwenCodeBackendParseUsageInfo(t *testing.T) {
	backend := NewQwenCodeBackend(nil)

	line := `{"type":"result","result":"Done","usage":{"input_tokens":100,"output_tokens":50},"model":"qwen3-coder-plus"}`
	event := backend.parseStreamEvent(line)

	if event.TokensInput != 100 {
		t.Errorf("TokensInput = %d, want 100", event.TokensInput)
	}
	if event.TokensOutput != 50 {
		t.Errorf("TokensOutput = %d, want 50", event.TokensOutput)
	}
	if event.Model != "qwen3-coder-plus" {
		t.Errorf("Model = %q, want qwen3-coder-plus", event.Model)
	}
}

func TestQwenCodeBackendParseToolInput(t *testing.T) {
	backend := NewQwenCodeBackend(nil)

	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"run_shell_command","input":{"command":"go test ./..."}}]}}`
	event := backend.parseStreamEvent(line)

	if event.ToolName != "Bash" {
		t.Errorf("ToolName = %q, want Bash", event.ToolName)
	}
	if event.ToolInput == nil {
		t.Fatal("ToolInput should not be nil")
	}
	if cmd, ok := event.ToolInput["command"].(string); !ok || cmd != "go test ./..." {
		t.Errorf("ToolInput[command] = %v, want 'go test ./...'", event.ToolInput["command"])
	}
}

func TestNormalizeQwenToolName(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"read_file", "Read"},
		{"write_file", "Write"},
		{"edit", "Edit"},
		{"run_shell_command", "Bash"},
		{"grep_search", "Grep"},
		{"glob", "Glob"},
		{"list_directory", "Bash"},
		{"web_fetch", "WebFetch"},
		{"web_search", "WebSearch"},
		{"todo_write", "TodoWrite"},
		{"save_memory", "TodoWrite"},
		{"task", "Task"},
		{"skill", "Skill"},
		{"lsp", "Bash"},
		{"exit_plan_mode", "ExitPlanMode"},
		// Unknown tools pass through
		{"unknown_tool", "unknown_tool"},
		{"custom_mcp_tool", "custom_mcp_tool"},
		// MCP tools pass through unchanged
		{"mcp__github__create_issue", "mcp__github__create_issue"},
		{"mcp__sqlite__query", "mcp__sqlite__query"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeQwenToolName(tt.input)
			if result != tt.expect {
				t.Errorf("normalizeQwenToolName(%q) = %q, want %q", tt.input, result, tt.expect)
			}
		})
	}
}
