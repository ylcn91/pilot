package replay

import (
	"testing"
)

func TestFormatEvent(t *testing.T) {
	tests := []struct {
		name     string
		event    StreamEvent
		contains []string
	}{
		{
			name: "system init",
			event: StreamEvent{
				Sequence: 1,
				Parsed:   &ParsedEvent{Type: "system", Subtype: "init"},
			},
			contains: []string{"#1", "System initialized"},
		},
		{
			name: "tool call",
			event: StreamEvent{
				Sequence: 2,
				Parsed: &ParsedEvent{
					Type:     "assistant",
					ToolName: "Read",
					ToolInput: map[string]any{
						"file_path": "/test/file.go",
					},
				},
			},
			contains: []string{"#2", "Read", "file.go"},
		},
		{
			name: "text",
			event: StreamEvent{
				Sequence: 3,
				Parsed: &ParsedEvent{
					Type: "assistant",
					Text: "Analyzing the codebase...",
				},
			},
			contains: []string{"#3", "Analyzing"},
		},
		{
			name: "result success",
			event: StreamEvent{
				Sequence: 4,
				Parsed: &ParsedEvent{
					Type:         "result",
					IsError:      false,
					InputTokens:  100,
					OutputTokens: 50,
				},
			},
			contains: []string{"#4", "Completed", "tokens"},
		},
		{
			name: "result error",
			event: StreamEvent{
				Sequence: 5,
				Parsed: &ParsedEvent{
					Type:    "result",
					IsError: true,
					Result:  "Something went wrong",
				},
			},
			contains: []string{"#5", "Error", "Something went wrong"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			output := FormatEvent(&tc.event, false)
			for _, expected := range tc.contains {
				if !containsString(output, expected) {
					t.Errorf("Expected output to contain '%s', got: %s", expected, output)
				}
			}
		})
	}
}

func TestFormatEventUserType(t *testing.T) {
	event := StreamEvent{
		Sequence: 1,
		Parsed:   &ParsedEvent{Type: "user"},
	}

	output := FormatEvent(&event, false)
	if !containsString(output, "Tool result") {
		t.Errorf("Expected 'Tool result' for user type, got: %s", output)
	}
}

func TestFormatEventUnknownType(t *testing.T) {
	event := StreamEvent{
		Sequence: 1,
		Parsed:   &ParsedEvent{Type: "unknown_type"},
	}

	output := FormatEvent(&event, false)
	if !containsString(output, "unknown_type") {
		t.Errorf("Expected type in output, got: %s", output)
	}
}

func TestFormatEventNoParsed(t *testing.T) {
	event := StreamEvent{
		Sequence: 1,
		Type:     "raw_event",
		Parsed:   nil,
	}

	output := FormatEvent(&event, false)
	if !containsString(output, "raw_event") {
		t.Errorf("Expected raw type in output, got: %s", output)
	}
}

func TestFormatEventVerbose(t *testing.T) {
	longText := "This is a very long text that would normally be truncated when not in verbose mode but should be shown in full when verbose is enabled for debugging purposes and to see the complete context of what was happening during execution"

	event := StreamEvent{
		Sequence: 1,
		Parsed: &ParsedEvent{
			Type: "assistant",
			Text: longText,
		},
	}

	// Non-verbose should truncate
	outputShort := FormatEvent(&event, false)
	if containsString(outputShort, "debugging purposes") {
		t.Error("Non-verbose output should truncate long text")
	}

	// Verbose should show full text
	outputFull := FormatEvent(&event, true)
	if !containsString(outputFull, "debugging purposes") {
		t.Error("Verbose output should show full text")
	}
}

func TestFormatEventSystemSubtype(t *testing.T) {
	event := StreamEvent{
		Sequence: 1,
		Parsed:   &ParsedEvent{Type: "system", Subtype: "custom_system"},
	}

	output := FormatEvent(&event, false)
	if !containsString(output, "System:") || !containsString(output, "custom_system") {
		t.Errorf("Expected 'System: custom_system', got: %s", output)
	}
}
