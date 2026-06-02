package executor

import (
	"reflect"
	"testing"
)

func TestNewCodexExecBackend(t *testing.T) {
	tests := []struct {
		name          string
		config        *CodexExecConfig
		expectCommand string
		expectSandbox string
	}{
		{
			name:          "nil config uses defaults",
			config:        nil,
			expectCommand: "codex",
			expectSandbox: "workspace-write",
		},
		{
			name:          "empty command uses default",
			config:        &CodexExecConfig{Command: ""},
			expectCommand: "codex",
			expectSandbox: "workspace-write",
		},
		{
			name:          "custom command",
			config:        &CodexExecConfig{Command: "/custom/codex", Sandbox: "read-only"},
			expectCommand: "/custom/codex",
			expectSandbox: "read-only",
		},
		{
			name:          "bypass does not force sandbox",
			config:        &CodexExecConfig{BypassApprovalsAndSandbox: true},
			expectCommand: "codex",
			expectSandbox: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := NewCodexExecBackend(tt.config)
			if backend == nil {
				t.Fatal("NewCodexExecBackend returned nil")
			}
			if backend.config.Command != tt.expectCommand {
				t.Errorf("Command = %q, want %q", backend.config.Command, tt.expectCommand)
			}
			if backend.config.Sandbox != tt.expectSandbox {
				t.Errorf("Sandbox = %q, want %q", backend.config.Sandbox, tt.expectSandbox)
			}
		})
	}
}

func TestCodexExecBackendName(t *testing.T) {
	backend := NewCodexExecBackend(nil)
	if backend.Name() != BackendTypeCodexExec {
		t.Errorf("Name() = %q, want %q", backend.Name(), BackendTypeCodexExec)
	}
}

func TestCodexExecBackendIsAvailable(t *testing.T) {
	backend := NewCodexExecBackend(&CodexExecConfig{
		Command: "/nonexistent/path/to/codex",
	})
	if backend.IsAvailable() {
		t.Error("IsAvailable() should return false for non-existent command")
	}
}

func TestCodexExecBackendBuildArgs(t *testing.T) {
	backend := NewCodexExecBackend(&CodexExecConfig{
		Command:          "codex",
		Model:            "gpt-5.1-codex",
		Effort:           "medium",
		Sandbox:          "workspace-write",
		Ephemeral:        true,
		OutputSchemaPath: "schema.json",
		ExtraArgs:        []string{"--skip-git-repo-check"},
	})

	got := backend.buildArgs(ExecuteOptions{
		Prompt:      "Implement task",
		ProjectPath: "/repo",
		Model:       "gpt-5.1-codex-max",
		Effort:      "high",
	})

	want := []string{
		"exec", "--json",
		"-C", "/repo",
		"--sandbox", "workspace-write",
		"--model", "gpt-5.1-codex-max",
		"-c", `model_reasoning_effort="high"`,
		"--ephemeral",
		"--output-schema", "schema.json",
		"--skip-git-repo-check",
		"Implement task",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildArgs() = %#v, want %#v", got, want)
	}
}

func TestCodexExecBackendBuildArgsBypass(t *testing.T) {
	backend := NewCodexExecBackend(&CodexExecConfig{
		BypassApprovalsAndSandbox: true,
	})

	got := backend.buildArgs(ExecuteOptions{
		Prompt:      "Task",
		ProjectPath: "/repo",
	})

	want := []string{
		"exec", "--json",
		"-C", "/repo",
		"--dangerously-bypass-approvals-and-sandbox",
		"Task",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildArgs() = %#v, want %#v", got, want)
	}
}

func TestCodexExecBackendBuildArgsResume(t *testing.T) {
	backend := NewCodexExecBackend(&CodexExecConfig{
		UseSessionResume: true,
		Model:            "gpt-5.1-codex",
	})

	got := backend.buildArgs(ExecuteOptions{
		Prompt:          "Continue",
		ResumeSessionID: "019e874b-4ef7-7802-83fd-2f7f5bbffbcf",
	})

	want := []string{
		"exec", "resume", "--json",
		"--model", "gpt-5.1-codex",
		"019e874b-4ef7-7802-83fd-2f7f5bbffbcf",
		"Continue",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildArgs() = %#v, want %#v", got, want)
	}
}

func TestCodexExecBackendParseStreamEvent(t *testing.T) {
	backend := NewCodexExecBackend(nil)

	tests := []struct {
		name            string
		line            string
		expectType      BackendEventType
		expectSession   string
		expectMessage   string
		expectTool      string
		expectToolText  string
		expectError     bool
		expectTokensIn  int64
		expectCacheIn   int64
		expectTokensOut int64
	}{
		{
			name:          "thread started",
			line:          `{"type":"thread.started","thread_id":"019e874b-4ef7-7802-83fd-2f7f5bbffbcf"}`,
			expectType:    EventTypeInit,
			expectSession: "019e874b-4ef7-7802-83fd-2f7f5bbffbcf",
		},
		{
			name:          "turn started",
			line:          `{"type":"turn.started"}`,
			expectType:    EventTypeProgress,
			expectMessage: "Codex turn started",
		},
		{
			name:          "agent message",
			line:          `{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"OK"}}`,
			expectType:    EventTypeText,
			expectMessage: "OK",
		},
		{
			name:       "command started",
			line:       `{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"/bin/zsh -lc pwd","aggregated_output":"","exit_code":null,"status":"in_progress"}}`,
			expectType: EventTypeToolUse,
			expectTool: "Bash",
		},
		{
			name:           "command completed",
			line:           `{"type":"item.completed","item":{"id":"item_0","type":"command_execution","command":"/bin/zsh -lc pwd","aggregated_output":"/repo\n","exit_code":0,"status":"completed"}}`,
			expectType:     EventTypeToolResult,
			expectTool:     "Bash",
			expectToolText: "/repo\n",
		},
		{
			name:        "command failed",
			line:        `{"type":"item.completed","item":{"id":"item_0","type":"command_execution","command":"/bin/zsh -lc false","aggregated_output":"","exit_code":1,"status":"completed"}}`,
			expectType:  EventTypeToolResult,
			expectTool:  "Bash",
			expectError: true,
		},
		{
			name:            "turn completed usage",
			line:            `{"type":"turn.completed","usage":{"input_tokens":22036,"cached_input_tokens":2432,"output_tokens":42,"reasoning_output_tokens":35}}`,
			expectType:      EventTypeResult,
			expectTokensIn:  22036,
			expectCacheIn:   2432,
			expectTokensOut: 42,
		},
		{
			name:          "stream error",
			line:          `{"type":"stream_error","message":"connection failed"}`,
			expectType:    EventTypeError,
			expectMessage: "connection failed",
			expectError:   true,
		},
		{
			name:          "invalid json",
			line:          `not json`,
			expectType:    EventTypeText,
			expectMessage: "not json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := backend.parseStreamEvent(tt.line)
			if event.Type != tt.expectType {
				t.Errorf("Type = %q, want %q", event.Type, tt.expectType)
			}
			if tt.expectSession != "" && event.SessionID != tt.expectSession {
				t.Errorf("SessionID = %q, want %q", event.SessionID, tt.expectSession)
			}
			if tt.expectMessage != "" && event.Message != tt.expectMessage {
				t.Errorf("Message = %q, want %q", event.Message, tt.expectMessage)
			}
			if tt.expectTool != "" && event.ToolName != tt.expectTool {
				t.Errorf("ToolName = %q, want %q", event.ToolName, tt.expectTool)
			}
			if tt.expectToolText != "" && event.ToolResult != tt.expectToolText {
				t.Errorf("ToolResult = %q, want %q", event.ToolResult, tt.expectToolText)
			}
			if event.IsError != tt.expectError {
				t.Errorf("IsError = %v, want %v", event.IsError, tt.expectError)
			}
			if event.TokensInput != tt.expectTokensIn {
				t.Errorf("TokensInput = %d, want %d", event.TokensInput, tt.expectTokensIn)
			}
			if event.CacheReadInputTokens != tt.expectCacheIn {
				t.Errorf("CacheReadInputTokens = %d, want %d", event.CacheReadInputTokens, tt.expectCacheIn)
			}
			if event.TokensOutput != tt.expectTokensOut {
				t.Errorf("TokensOutput = %d, want %d", event.TokensOutput, tt.expectTokensOut)
			}
			if event.Raw != tt.line {
				t.Errorf("Raw = %q, want %q", event.Raw, tt.line)
			}
		})
	}
}

func TestCodexExecErrorFormatting(t *testing.T) {
	err := &CodexExecError{
		Type:    CodexExecErrorTypeRateLimit,
		Message: "Codex rate limit reached",
		Stderr:  "429 too many requests",
	}
	if got := err.Error(); got != "rate_limit: Codex rate limit reached (stderr: 429 too many requests)" {
		t.Errorf("Error() = %q", got)
	}
}

func TestBackendFactoryCodexExec(t *testing.T) {
	config := &BackendConfig{
		Type: BackendTypeCodexExec,
		CodexExec: &CodexExecConfig{
			Command: "codex",
		},
	}

	backend, err := NewBackend(config)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	if backend == nil {
		t.Fatal("NewBackend() returned nil")
	}
	if backend.Name() != BackendTypeCodexExec {
		t.Errorf("Name() = %q, want %q", backend.Name(), BackendTypeCodexExec)
	}
}
