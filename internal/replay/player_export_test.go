package replay

import (
	"testing"
)

func TestExportToHTML(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-HTML", "/test/project", tmpDir)
	_ = recorder.RecordEvent(`{"type":"system","subtype":"init"}`)
	_ = recorder.RecordEvent(`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello world"}]}}`)
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	events, _ := LoadStreamEvents(recording)

	html, err := ExportToHTML(recording, events)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Check for expected HTML elements
	if !containsString(html, "<!DOCTYPE html>") {
		t.Error("HTML should contain DOCTYPE")
	}
	if !containsString(html, recording.ID) {
		t.Error("HTML should contain recording ID")
	}
	if !containsString(html, "Hello world") {
		t.Error("HTML should contain event text")
	}
}

func TestExportToJSON(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-JSON", "/test/project", tmpDir)
	_ = recorder.RecordEvent(`{"type":"system","subtype":"init"}`)
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	events, _ := LoadStreamEvents(recording)

	jsonData, err := ExportToJSON(recording, events)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if len(jsonData) == 0 {
		t.Error("JSON export should not be empty")
	}

	// Verify it's valid JSON (contains expected fields)
	if !containsString(string(jsonData), "recording") {
		t.Error("JSON should contain recording field")
	}
	if !containsString(string(jsonData), "events") {
		t.Error("JSON should contain events field")
	}
}

func TestExportToHTMLWithErrors(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-HTMLERR", "/test/project", tmpDir)
	_ = recorder.RecordEvent(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/test"}}]}}`)
	_ = recorder.RecordEvent(`{"type":"result","result":"File not found","is_error":true}`)
	_ = recorder.Finish("failed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	events, _ := LoadStreamEvents(recording)

	html, err := ExportToHTML(recording, events)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if !containsString(html, "event-error") {
		t.Error("HTML should contain error event class")
	}
}

func TestExportToHTMLWithTokens(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-HTMLTOK", "/test/project", tmpDir)
	_ = recorder.RecordEvent(`{"type":"result","result":"done","usage":{"input_tokens":1000,"output_tokens":500}}`)
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	events, _ := LoadStreamEvents(recording)

	html, err := ExportToHTML(recording, events)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if !containsString(html, "Tokens") {
		t.Error("HTML should contain token information")
	}
	if !containsString(html, "Cost") {
		t.Error("HTML should contain cost information")
	}
}

func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "ampersand",
			input:    "a & b",
			expected: "a &amp; b",
		},
		{
			name:     "less than",
			input:    "a < b",
			expected: "a &lt; b",
		},
		{
			name:     "greater than",
			input:    "a > b",
			expected: "a &gt; b",
		},
		{
			name:     "quote",
			input:    `a "b" c`,
			expected: "a &quot;b&quot; c",
		},
		{
			name:     "all characters",
			input:    `<a href="test">A & B</a>`,
			expected: "&lt;a href=&quot;test&quot;&gt;A &amp; B&lt;/a&gt;",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := escapeHTML(tc.input)
			if result != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}
