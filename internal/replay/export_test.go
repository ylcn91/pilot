package replay

import (
	"strings"
	"testing"
)

func TestExportHTMLReport(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-REPORT", "/test/project", tmpDir)
	recorder.SetModel("claude-sonnet-4-6")
	recorder.SetBranch("feature/test")
	recorder.SetNavigator(true)

	events := []string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Phase: RESEARCH"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/test/a.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Grep","input":{"pattern":"func"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Phase: implementing"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"/test/b.go"}}]}}`,
		`{"type":"result","result":"done","usage":{"input_tokens":5000,"output_tokens":2000}}`,
	}

	for _, e := range events {
		_ = recorder.RecordEvent(e)
	}
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	eventsLoaded, _ := LoadStreamEvents(recording)
	analyzer, _ := NewAnalyzer(recording)
	report, _ := analyzer.Analyze()

	html, err := ExportHTMLReport(recording, eventsLoaded, report)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Check for expected HTML elements
	expectedContains := []string{
		"<!DOCTYPE html>",
		recording.ID,
		"TASK-REPORT",
		"Phase Timing",
		"Tool Usage",
		"Token Breakdown",
		"Execution Timeline",
		"claude-sonnet",
		"Navigator",
	}

	for _, expected := range expectedContains {
		if !strings.Contains(html, expected) {
			t.Errorf("HTML should contain '%s'", expected)
		}
	}
}

func TestExportHTMLReportWithErrors(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-ERR", "/test/project", tmpDir)
	events := []string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/nonexistent"}}]}}`,
		`{"type":"result","result":"File not found","is_error":true}`,
	}
	for _, e := range events {
		_ = recorder.RecordEvent(e)
	}
	_ = recorder.Finish("failed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	eventsLoaded, _ := LoadStreamEvents(recording)
	analyzer, _ := NewAnalyzer(recording)
	report, _ := analyzer.Analyze()

	html, err := ExportHTMLReport(recording, eventsLoaded, report)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if !strings.Contains(html, "Errors") {
		t.Error("HTML should contain Errors section")
	}
	if !strings.Contains(html, "status-failed") {
		t.Error("HTML should have failed status badge")
	}
}

func TestExportToMarkdown(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-MD", "/test/project", tmpDir)
	recorder.SetBranch("test-branch")
	recorder.SetModel("claude-sonnet-4-6")
	recorder.SetNavigator(true)

	events := []string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/test"}}]}}`,
		`{"type":"result","result":"done","usage":{"input_tokens":1000,"output_tokens":500}}`,
	}
	for _, e := range events {
		_ = recorder.RecordEvent(e)
	}
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	eventsLoaded, _ := LoadStreamEvents(recording)
	analyzer, _ := NewAnalyzer(recording)
	report, _ := analyzer.Analyze()

	md, err := ExportToMarkdown(recording, eventsLoaded, report)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	expectedContains := []string{
		"# Execution Recording:",
		"## Summary",
		"| Property | Value |",
		"TASK-MD",
		"test-branch",
		"## Token Usage",
		"## Tool Usage",
		"## Event Timeline",
		"<details>",
	}

	for _, expected := range expectedContains {
		if !strings.Contains(md, expected) {
			t.Errorf("Markdown should contain '%s'", expected)
		}
	}
}

func TestExportToMarkdownWithErrors(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-MDERR", "/test/project", tmpDir)
	events := []string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"exit 1"}}]}}`,
		`{"type":"result","result":"Command failed","is_error":true}`,
	}
	for _, e := range events {
		_ = recorder.RecordEvent(e)
	}
	_ = recorder.Finish("failed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	eventsLoaded, _ := LoadStreamEvents(recording)
	analyzer, _ := NewAnalyzer(recording)
	report, _ := analyzer.Analyze()

	md, err := ExportToMarkdown(recording, eventsLoaded, report)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if !strings.Contains(md, "## Errors") {
		t.Error("Markdown should contain Errors section")
	}
	if !strings.Contains(md, "❌ failed") {
		t.Error("Markdown should show failed status")
	}
}

func TestExportHTMLReportNilReport(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-NIL", "/test/project", tmpDir)
	_ = recorder.RecordEvent(`{"type":"system","subtype":"init"}`)
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	events, _ := LoadStreamEvents(recording)

	// Should not panic with nil report
	html, err := ExportHTMLReport(recording, events, nil)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if !strings.Contains(html, "TG-") {
		t.Error("HTML should still contain recording ID")
	}
}

func TestExportToMarkdownNilReport(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-MDNIL", "/test/project", tmpDir)
	_ = recorder.RecordEvent(`{"type":"system","subtype":"init"}`)
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	events, _ := LoadStreamEvents(recording)

	// Should not panic with nil report
	md, err := ExportToMarkdown(recording, events, nil)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if !strings.Contains(md, "# Execution Recording") {
		t.Error("Markdown should contain header")
	}
}
