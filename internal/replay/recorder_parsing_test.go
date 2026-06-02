package replay

import (
	"testing"
)

func TestDetectPhase(t *testing.T) {
	tmpDir := t.TempDir()
	recorder, _ := NewRecorder("TASK-PHASE", "/test/project", tmpDir)
	defer func() { _ = recorder.Finish("completed") }()

	tests := []struct {
		name     string
		parsed   *ParsedEvent
		expected string
	}{
		{
			name:     "Read tool - Exploring",
			parsed:   &ParsedEvent{ToolName: "Read"},
			expected: "Exploring",
		},
		{
			name:     "Glob tool - Exploring",
			parsed:   &ParsedEvent{ToolName: "Glob"},
			expected: "Exploring",
		},
		{
			name:     "Grep tool - Exploring",
			parsed:   &ParsedEvent{ToolName: "Grep"},
			expected: "Exploring",
		},
		{
			name:     "Write tool - Implementing",
			parsed:   &ParsedEvent{ToolName: "Write"},
			expected: "Implementing",
		},
		{
			name:     "Edit tool - Implementing",
			parsed:   &ParsedEvent{ToolName: "Edit"},
			expected: "Implementing",
		},
		{
			name:     "Bash git commit - Committing",
			parsed:   &ParsedEvent{ToolName: "Bash", ToolInput: map[string]any{"command": "git commit -m 'message'"}},
			expected: "Committing",
		},
		{
			name:     "Bash test - Testing",
			parsed:   &ParsedEvent{ToolName: "Bash", ToolInput: map[string]any{"command": "go test ./..."}},
			expected: "Testing",
		},
		{
			name:     "Bash other - empty",
			parsed:   &ParsedEvent{ToolName: "Bash", ToolInput: map[string]any{"command": "ls -la"}},
			expected: "",
		},
		{
			name:     "Text with PHASE RESEARCH",
			parsed:   &ParsedEvent{Text: "PHASE: RESEARCH starting now"},
			expected: "Research",
		},
		{
			name:     "Text with Phase Research",
			parsed:   &ParsedEvent{Text: "Phase: Research starting now"},
			expected: "Research",
		},
		{
			name:     "Text with PHASE IMPL",
			parsed:   &ParsedEvent{Text: "PHASE: IMPL starting now"},
			expected: "Implementing",
		},
		{
			name:     "Text with Phase Implement",
			parsed:   &ParsedEvent{Text: "Phase: Implement starting now"},
			expected: "Implementing",
		},
		{
			name:     "Text with PHASE VERIFY",
			parsed:   &ParsedEvent{Text: "PHASE: VERIFY starting now"},
			expected: "Verifying",
		},
		{
			name:     "Text with Phase Verify",
			parsed:   &ParsedEvent{Text: "Phase: Verify starting now"},
			expected: "Verifying",
		},
		{
			name:     "Text with PHASE COMPLETE",
			parsed:   &ParsedEvent{Text: "PHASE: COMPLETE starting now"},
			expected: "Completing",
		},
		{
			name:     "Text with Phase Complete",
			parsed:   &ParsedEvent{Text: "Phase: Complete starting now"},
			expected: "Completing",
		},
		{
			name:     "Text without phase",
			parsed:   &ParsedEvent{Text: "Just some regular text"},
			expected: "",
		},
		{
			name:     "Unknown tool",
			parsed:   &ParsedEvent{ToolName: "CustomTool"},
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := recorder.detectPhase(tc.parsed)
			if result != tc.expected {
				t.Errorf("Expected phase '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

func TestDetectFileOp(t *testing.T) {
	tmpDir := t.TempDir()
	recorder, _ := NewRecorder("TASK-FILEOP", "/test/project", tmpDir)
	defer func() { _ = recorder.Finish("completed") }()

	tests := []struct {
		toolName string
		expected string
	}{
		{"Read", "read"},
		{"Write", "create"},
		{"Edit", "modify"},
		{"Bash", ""},
		{"Unknown", ""},
	}

	for _, tc := range tests {
		t.Run(tc.toolName, func(t *testing.T) {
			result := recorder.detectFileOp(tc.toolName)
			if result != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

func TestEstimateCostOpus(t *testing.T) {
	tmpDir := t.TempDir()
	recorder, _ := NewRecorder("TASK-OPUS", "/test/project", tmpDir)
	recorder.SetModel("claude-opus-4")

	// Record event with tokens
	_ = recorder.RecordEvent(`{"type":"result","result":"done","usage":{"input_tokens":1000,"output_tokens":1000}}`)
	_ = recorder.Finish("completed")

	recording := recorder.GetRecording()

	// Opus 4.0 (legacy) pricing: 15.00/1M input, 75.00/1M output
	// Expected: (1000 * 15 + 1000 * 75) / 1_000_000 = 0.09
	expectedCost := 0.09
	if recording.TokenUsage.EstimatedCostUSD < expectedCost-0.001 || recording.TokenUsage.EstimatedCostUSD > expectedCost+0.001 {
		t.Errorf("Expected cost ~%.4f, got %.4f", expectedCost, recording.TokenUsage.EstimatedCostUSD)
	}
}

func TestEstimateCostSonnet(t *testing.T) {
	tmpDir := t.TempDir()
	recorder, _ := NewRecorder("TASK-SONNET", "/test/project", tmpDir)
	recorder.SetModel("claude-sonnet-4")

	// Record event with tokens
	_ = recorder.RecordEvent(`{"type":"result","result":"done","usage":{"input_tokens":1000,"output_tokens":1000}}`)
	_ = recorder.Finish("completed")

	recording := recorder.GetRecording()

	// Sonnet pricing: 3.00/1M input, 15.00/1M output
	// Expected: (1000 * 3 + 1000 * 15) / 1_000_000 = 0.018
	expectedCost := 0.018
	if recording.TokenUsage.EstimatedCostUSD < expectedCost-0.001 || recording.TokenUsage.EstimatedCostUSD > expectedCost+0.001 {
		t.Errorf("Expected cost ~%.4f, got %.4f", expectedCost, recording.TokenUsage.EstimatedCostUSD)
	}
}

func TestParseEventTypes(t *testing.T) {
	tmpDir := t.TempDir()
	recorder, _ := NewRecorder("TASK-PARSE", "/test/project", tmpDir)
	defer func() { _ = recorder.Finish("completed") }()

	tests := []struct {
		name        string
		rawJSON     string
		expectType  string
		expectTool  string
		expectText  string
		expectError bool
	}{
		{
			name:       "system event",
			rawJSON:    `{"type":"system","subtype":"init"}`,
			expectType: "system",
		},
		{
			name:       "result event",
			rawJSON:    `{"type":"result","result":"done","is_error":false}`,
			expectType: "result",
		},
		{
			name:        "result error event",
			rawJSON:     `{"type":"result","result":"error message","is_error":true}`,
			expectType:  "result",
			expectError: true,
		},
		{
			name:       "assistant tool use",
			rawJSON:    `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/test.go"}}]}}`,
			expectType: "assistant",
			expectTool: "Read",
		},
		{
			name:       "assistant text",
			rawJSON:    `{"type":"assistant","message":{"content":[{"type":"text","text":"Hello world"}]}}`,
			expectType: "assistant",
			expectText: "Hello world",
		},
		{
			name:       "invalid json",
			rawJSON:    `not valid json`,
			expectType: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed := recorder.parseEvent(tc.rawJSON)

			if tc.expectType == "" {
				if parsed != nil {
					t.Errorf("Expected nil for invalid JSON")
				}
				return
			}

			if parsed == nil {
				t.Fatalf("Expected parsed event, got nil")
			}

			if parsed.Type != tc.expectType {
				t.Errorf("Expected type '%s', got '%s'", tc.expectType, parsed.Type)
			}

			if tc.expectTool != "" && parsed.ToolName != tc.expectTool {
				t.Errorf("Expected tool '%s', got '%s'", tc.expectTool, parsed.ToolName)
			}

			if tc.expectText != "" && parsed.Text != tc.expectText {
				t.Errorf("Expected text '%s', got '%s'", tc.expectText, parsed.Text)
			}

			if parsed.IsError != tc.expectError {
				t.Errorf("Expected IsError=%v, got %v", tc.expectError, parsed.IsError)
			}
		})
	}
}

func TestRecordEventWithPhaseChanges(t *testing.T) {
	tmpDir := t.TempDir()
	recorder, _ := NewRecorder("TASK-PHASECHANGE", "/test/project", tmpDir)

	// Record events that trigger phase changes
	events := []string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/test.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"/new.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"git commit -m 'test'"}}]}}`,
	}

	for _, e := range events {
		_ = recorder.RecordEvent(e)
	}
	_ = recorder.Finish("completed")

	recording := recorder.GetRecording()

	// Should have phase timings
	if len(recording.PhaseTimings) == 0 {
		t.Error("Expected phase timings to be recorded")
	}
}

func TestRecordEventWithFileTracking(t *testing.T) {
	tmpDir := t.TempDir()
	recorder, _ := NewRecorder("TASK-FILES", "/test/project", tmpDir)

	events := []string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/test/a.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"/test/b.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/test/c.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/test/c.go"}}]}}`, // Same file again
	}

	for _, e := range events {
		_ = recorder.RecordEvent(e)
	}
	_ = recorder.Finish("completed")

	// Check that diffs were tracked (3 unique files)
	if len(recorder.diffFiles) != 3 {
		t.Errorf("Expected 3 unique files tracked, got %d", len(recorder.diffFiles))
	}
}
