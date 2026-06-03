package replay

import (
	"testing"
)

// TestParseEventMultiBlockMessage proves that an assistant message containing
// both a text block and a tool_use block preserves both pieces of information,
// rather than the last block silently clobbering the earlier one.
func TestParseEventMultiBlockMessage(t *testing.T) {
	tmpDir := t.TempDir()
	recorder, _ := NewRecorder("TASK-MULTIBLOCK", "/test/project", tmpDir)
	defer func() { _ = recorder.Finish("completed") }()

	t.Run("text then tool_use preserves both", func(t *testing.T) {
		raw := `{"type":"assistant","message":{"content":[` +
			`{"type":"text","text":"Reading the file now"},` +
			`{"type":"tool_use","name":"Read","input":{"file_path":"/test.go"}}` +
			`]}}`

		parsed := recorder.parseEvent(raw)
		if parsed == nil {
			t.Fatalf("Expected parsed event, got nil")
		}
		if parsed.Text != "Reading the file now" {
			t.Errorf("Expected text preserved, got %q", parsed.Text)
		}
		if parsed.ToolName != "Read" {
			t.Errorf("Expected tool name 'Read', got %q", parsed.ToolName)
		}
		if parsed.FilePath != "/test.go" {
			t.Errorf("Expected file path '/test.go', got %q", parsed.FilePath)
		}
		if parsed.FileOperation != "read" {
			t.Errorf("Expected file operation 'read', got %q", parsed.FileOperation)
		}
	})

	t.Run("tool_use then text preserves both", func(t *testing.T) {
		raw := `{"type":"assistant","message":{"content":[` +
			`{"type":"tool_use","name":"Write","input":{"file_path":"/out.go"}},` +
			`{"type":"text","text":"Wrote the file"}` +
			`]}}`

		parsed := recorder.parseEvent(raw)
		if parsed == nil {
			t.Fatalf("Expected parsed event, got nil")
		}
		if parsed.Text != "Wrote the file" {
			t.Errorf("Expected text preserved, got %q", parsed.Text)
		}
		if parsed.ToolName != "Write" {
			t.Errorf("Expected tool name 'Write', got %q", parsed.ToolName)
		}
	})

	t.Run("multiple text blocks are accumulated", func(t *testing.T) {
		raw := `{"type":"assistant","message":{"content":[` +
			`{"type":"text","text":"first"},` +
			`{"type":"text","text":"second"}` +
			`]}}`

		parsed := recorder.parseEvent(raw)
		if parsed == nil {
			t.Fatalf("Expected parsed event, got nil")
		}
		if parsed.Text != "first\nsecond" {
			t.Errorf("Expected accumulated text \"first\\nsecond\", got %q", parsed.Text)
		}
	})
}
