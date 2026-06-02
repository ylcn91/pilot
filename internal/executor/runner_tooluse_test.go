package executor

import (
	"testing"
)

func TestProgressStateStruct(t *testing.T) {
	state := &progressState{
		phase:        "Implementing",
		filesRead:    5,
		filesWrite:   3,
		commands:     10,
		hasNavigator: true,
		navPhase:     "IMPL",
		navIteration: 2,
		navProgress:  45,
		exitSignal:   false,
		commitSHAs:   []string{"abc1234", "def5678"},
		tokensInput:  1000,
		tokensOutput: 500,
		modelName:    "claude-sonnet-4-6",
	}

	if state.phase != "Implementing" {
		t.Errorf("phase = %q, want Implementing", state.phase)
	}
	if len(state.commitSHAs) != 2 {
		t.Errorf("commitSHAs count = %d, want 2", len(state.commitSHAs))
	}
	if state.tokensInput+state.tokensOutput != 1500 {
		t.Error("Token sum calculation incorrect")
	}
}

func TestHandleToolUseGlob(t *testing.T) {
	runner := NewRunner()

	var lastPhase string
	runner.OnProgress(func(taskID, phase string, progress int, message string) {
		lastPhase = phase
	})

	state := &progressState{phase: "Starting"}
	runner.handleToolUse("TASK-1", "Glob", map[string]interface{}{
		"pattern": "**/*.go",
	}, state)

	if state.filesRead != 1 {
		t.Errorf("filesRead = %d, want 1", state.filesRead)
	}
	if lastPhase != "Exploring" {
		t.Errorf("phase = %q, want Exploring", lastPhase)
	}
}

func TestHandleToolUseGrep(t *testing.T) {
	runner := NewRunner()

	var lastPhase string
	runner.OnProgress(func(taskID, phase string, progress int, message string) {
		lastPhase = phase
	})

	state := &progressState{phase: "Starting"}
	runner.handleToolUse("TASK-1", "Grep", map[string]interface{}{
		"pattern": "func main",
	}, state)

	if state.filesRead != 1 {
		t.Errorf("filesRead = %d, want 1", state.filesRead)
	}
	if lastPhase != "Exploring" {
		t.Errorf("phase = %q, want Exploring", lastPhase)
	}
}

func TestHandleToolUseEdit(t *testing.T) {
	runner := NewRunner()

	var lastPhase string
	var lastMessage string
	runner.OnProgress(func(taskID, phase string, progress int, message string) {
		lastPhase = phase
		lastMessage = message
	})

	state := &progressState{phase: "Starting"}
	runner.handleToolUse("TASK-1", "Edit", map[string]interface{}{
		"file_path": "/path/to/file.go",
	}, state)

	if state.filesWrite != 1 {
		t.Errorf("filesWrite = %d, want 1", state.filesWrite)
	}
	if lastPhase != "Implementing" {
		t.Errorf("phase = %q, want Implementing", lastPhase)
	}
	if !contains(lastMessage, "file.go") {
		t.Errorf("message should mention file name, got %q", lastMessage)
	}
}

func TestHandleToolUseBashTests(t *testing.T) {
	tests := []struct {
		name          string
		command       string
		expectedPhase string
	}{
		{"pytest", "pytest tests/", "Testing"},
		{"jest", "npm run jest", "Testing"},
		{"go test", "go test ./...", "Testing"},
		{"npm test", "npm test", "Testing"},
		{"make test", "make test", "Testing"},
		{"npm install", "npm install", "Installing"},
		{"pip install", "pip install -r requirements.txt", "Installing"},
		{"go mod", "go mod tidy", "Installing"},
		{"git checkout", "git checkout -b feature", "Branching"},
		{"git branch", "git branch new-branch", "Branching"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := NewRunner()

			var lastPhase string
			runner.OnProgress(func(taskID, phase string, progress int, message string) {
				lastPhase = phase
			})

			state := &progressState{phase: "Starting"}
			runner.handleToolUse("TASK-1", "Bash", map[string]interface{}{
				"command": tt.command,
			}, state)

			if lastPhase != tt.expectedPhase {
				t.Errorf("phase = %q, want %q for command %q", lastPhase, tt.expectedPhase, tt.command)
			}
		})
	}
}

func TestHandleToolUseAgentWrite(t *testing.T) {
	runner := NewRunner()

	var progressCalls int
	runner.OnProgress(func(taskID, phase string, progress int, message string) {
		progressCalls++
	})

	state := &progressState{phase: "Starting"}

	// Writing to .agent directory should set hasNavigator
	runner.handleToolUse("TASK-1", "Write", map[string]interface{}{
		"file_path": "/project/.agent/tasks/TASK-1.md",
	}, state)

	if !state.hasNavigator {
		t.Error("hasNavigator should be true after writing to .agent/")
	}
}

func TestHandleToolUseContextMarker(t *testing.T) {
	runner := NewRunner()

	var lastPhase string
	runner.OnProgress(func(taskID, phase string, progress int, message string) {
		lastPhase = phase
	})

	state := &progressState{phase: "Starting"}
	runner.handleToolUse("TASK-1", "Write", map[string]interface{}{
		"file_path": "/project/.agent/.context-markers/marker-123.md",
	}, state)

	if lastPhase != "Checkpoint" {
		t.Errorf("phase = %q, want Checkpoint", lastPhase)
	}
	if !state.hasNavigator {
		t.Error("hasNavigator should be true")
	}
}

func TestHandleToolUseTask(t *testing.T) {
	runner := NewRunner()

	var lastPhase string
	var lastMessage string
	runner.OnProgress(func(taskID, phase string, progress int, message string) {
		lastPhase = phase
		lastMessage = message
	})

	state := &progressState{phase: "Starting"}
	runner.handleToolUse("TASK-1", "Task", map[string]interface{}{
		"description": "Run unit tests and verify",
	}, state)

	if lastPhase != "Delegating" {
		t.Errorf("phase = %q, want Delegating", lastPhase)
	}
	if !contains(lastMessage, "Spawning") {
		t.Errorf("message should contain Spawning, got %q", lastMessage)
	}
}

func TestProcessBackendEvent(t *testing.T) {
	runner := NewRunner()

	var lastPhase string
	var lastMessage string
	runner.OnProgress(func(taskID, phase string, progress int, message string) {
		lastPhase = phase
		lastMessage = message
	})

	tests := []struct {
		name          string
		event         BackendEvent
		expectedPhase string
		expectedMsg   string
	}{
		{
			name: "init event",
			event: BackendEvent{
				Type:    EventTypeInit,
				Message: "Backend initialized",
			},
			expectedPhase: "🚀 Started",
			expectedMsg:   "Backend initialized",
		},
		{
			name: "tool use Read",
			event: BackendEvent{
				Type:      EventTypeToolUse,
				ToolName:  "Read",
				ToolInput: map[string]interface{}{"file_path": "/test.go"},
			},
			expectedPhase: "Exploring",
		},
		{
			name: "tool use Write",
			event: BackendEvent{
				Type:      EventTypeToolUse,
				ToolName:  "Write",
				ToolInput: map[string]interface{}{"file_path": "/output.go"},
			},
			expectedPhase: "Implementing",
		},
		{
			name: "text with Navigator session",
			event: BackendEvent{
				Type:    EventTypeText,
				Message: "Navigator Session Started\n━━━━━━━━━",
			},
			expectedPhase: "Navigator",
		},
		{
			name: "text with EXIT_SIGNAL",
			event: BackendEvent{
				Type:    EventTypeText,
				Message: "EXIT_SIGNAL: true",
			},
			expectedPhase: "Finishing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lastPhase = ""
			lastMessage = ""
			state := &progressState{phase: "Starting"}

			runner.processBackendEvent("TASK-1", tt.event, state)

			if tt.expectedPhase != "" && lastPhase != tt.expectedPhase {
				t.Errorf("phase = %q, want %q", lastPhase, tt.expectedPhase)
			}
			if tt.expectedMsg != "" && lastMessage != tt.expectedMsg {
				t.Errorf("message = %q, want %q", lastMessage, tt.expectedMsg)
			}
		})
	}
}

func TestProcessBackendEventTokenTracking(t *testing.T) {
	runner := NewRunner()
	state := &progressState{}

	// Process multiple events with token usage
	events := []BackendEvent{
		{Type: EventTypeText, TokensInput: 100, TokensOutput: 50},
		{Type: EventTypeText, TokensInput: 200, TokensOutput: 100},
		{Type: EventTypeResult, TokensInput: 50, TokensOutput: 25, Model: "claude-sonnet-4-6"},
	}

	for _, event := range events {
		runner.processBackendEvent("TASK-1", event, state)
	}

	expectedInput := int64(350)
	expectedOutput := int64(175)

	if state.tokensInput != expectedInput {
		t.Errorf("tokensInput = %d, want %d", state.tokensInput, expectedInput)
	}
	if state.tokensOutput != expectedOutput {
		t.Errorf("tokensOutput = %d, want %d", state.tokensOutput, expectedOutput)
	}
	if state.modelName != "claude-sonnet-4-6" {
		t.Errorf("modelName = %q, want claude-sonnet-4-6", state.modelName)
	}
}

func TestProcessBackendEventToolResult(t *testing.T) {
	runner := NewRunner()
	state := &progressState{}

	// Tool result with commit SHA
	event := BackendEvent{
		Type:       EventTypeToolResult,
		ToolResult: "[main abc1234] feat: add feature",
	}

	runner.processBackendEvent("TASK-1", event, state)

	if len(state.commitSHAs) != 1 {
		t.Fatalf("commitSHAs length = %d, want 1", len(state.commitSHAs))
	}
	if state.commitSHAs[0] != "abc1234" {
		t.Errorf("commitSHA = %q, want abc1234", state.commitSHAs[0])
	}
}

func TestProcessBackendEventProgressPhase(t *testing.T) {
	runner := NewRunner()

	var lastPhase string
	runner.OnProgress(func(taskID, phase string, progress int, message string) {
		lastPhase = phase
	})

	state := &progressState{phase: "Starting"}

	// Progress event with phase
	event := BackendEvent{
		Type:  EventTypeProgress,
		Phase: "IMPL",
	}

	runner.processBackendEvent("TASK-1", event, state)

	if lastPhase != "Implement" {
		t.Errorf("phase = %q, want Implement", lastPhase)
	}
}
