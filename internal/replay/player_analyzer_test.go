package replay

import (
	"testing"
)

func TestAnalyzer(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a recording with diverse events
	recorder, _ := NewRecorder("TASK-ANALYZE", "/test/project", tmpDir)

	events := []string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Phase: RESEARCH"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/test/a.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Grep","input":{"pattern":"func"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Phase: IMPL"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"/test/b.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/test/c.go"}}]}}`,
		`{"type":"result","result":"done","usage":{"input_tokens":500,"output_tokens":200}}`,
	}

	for _, e := range events {
		_ = recorder.RecordEvent(e)
	}
	_ = recorder.Finish("completed")

	// Analyze
	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	analyzer, err := NewAnalyzer(recording)
	if err != nil {
		t.Fatalf("Failed to create analyzer: %v", err)
	}

	report, err := analyzer.Analyze()
	if err != nil {
		t.Fatalf("Analysis failed: %v", err)
	}

	// Verify tool usage was tracked
	if len(report.ToolUsage) == 0 {
		t.Error("Expected tool usage to be tracked")
	}

	// Check specific tools
	foundRead := false
	foundWrite := false
	for _, tool := range report.ToolUsage {
		if tool.Tool == "Read" {
			foundRead = true
			if tool.Count != 1 {
				t.Errorf("Expected 1 Read call, got %d", tool.Count)
			}
		}
		if tool.Tool == "Write" {
			foundWrite = true
		}
	}

	if !foundRead {
		t.Error("Read tool should be in usage stats")
	}
	if !foundWrite {
		t.Error("Write tool should be in usage stats")
	}
}

func TestFormatReport(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-REPORT", "/test/project", tmpDir)
	recorder.SetModel("claude-sonnet-4-6")

	events := []string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Phase: RESEARCH"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/test/a.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Grep","input":{"pattern":"func"}}]}}`,
		`{"type":"result","result":"error","is_error":true}`,
		`{"type":"result","result":"done","usage":{"input_tokens":500,"output_tokens":200}}`,
	}

	for _, e := range events {
		_ = recorder.RecordEvent(e)
	}
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	analyzer, _ := NewAnalyzer(recording)
	report, _ := analyzer.Analyze()

	formatted := FormatReport(report)

	// Check for expected sections
	expectedContains := []string{
		"EXECUTION ANALYSIS REPORT",
		"Recording:",
		"Task:",
		"TASK-REPORT",
		"Status:",
		"Duration:",
		"Events:",
		"TOOL USAGE",
	}

	for _, expected := range expectedContains {
		if !containsString(formatted, expected) {
			t.Errorf("Expected report to contain '%s'", expected)
		}
	}
}

func TestFormatReportWithTokenUsage(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-TOKENS", "/test/project", tmpDir)
	_ = recorder.RecordEvent(`{"type":"result","result":"done","usage":{"input_tokens":1000,"output_tokens":500}}`)
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	analyzer, _ := NewAnalyzer(recording)
	report, _ := analyzer.Analyze()

	formatted := FormatReport(report)

	// Check for token usage section
	if !containsString(formatted, "TOKEN USAGE") {
		t.Error("Expected report to contain TOKEN USAGE section")
	}
	if !containsString(formatted, "Input:") {
		t.Error("Expected report to contain Input token count")
	}
	if !containsString(formatted, "Output:") {
		t.Error("Expected report to contain Output token count")
	}
}

func TestFormatReportWithErrors(t *testing.T) {
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
	analyzer, _ := NewAnalyzer(recording)
	report, _ := analyzer.Analyze()

	formatted := FormatReport(report)

	if !containsString(formatted, "ERRORS") {
		t.Error("Expected report to contain ERRORS section")
	}
}

func TestAnalyzerWithPhaseChanges(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-PHASES", "/test/project", tmpDir)
	events := []string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Phase: RESEARCH - analyzing"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/test/a.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"WORKFLOW CHECK: decision point"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Decision: use approach A"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Approach: implement feature"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Phase: implementing feature"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"/test/b.go"}}]}}`,
		`{"type":"result","result":"done"}`,
	}

	for _, e := range events {
		_ = recorder.RecordEvent(e)
	}
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	analyzer, _ := NewAnalyzer(recording)
	report, _ := analyzer.Analyze()

	// Check decision points were captured
	if len(report.DecisionPoints) == 0 {
		t.Error("Expected decision points to be captured")
	}

	// Check phase analysis
	if len(report.PhaseAnalysis) == 0 {
		t.Error("Expected phase analysis")
	}
}
