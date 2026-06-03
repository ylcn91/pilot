package replay

import (
	"testing"
)

// TestAnalyzeDecisionPointExtraction verifies that each Navigator decision
// trigger string ("WORKFLOW CHECK", "Decision:", "Approach:") produces a
// DecisionPoint and that the description is truncated to the documented
// length.
func TestAnalyzeDecisionPointExtraction(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, err := NewRecorder("TASK-DP", "/test/project", tmpDir)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	longText := "WORKFLOW CHECK " + repeatRune('x', 200) // > 100 chars to force truncation
	events := []string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"` + longText + `"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Decision: pick approach A"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Approach: implement incrementally"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"plain status text without a trigger"}]}}`,
		`{"type":"result","result":"done"}`,
	}
	for _, e := range events {
		_ = recorder.RecordEvent(e)
	}
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	analyzer, err := NewAnalyzer(recording)
	if err != nil {
		t.Fatalf("NewAnalyzer: %v", err)
	}
	report, err := analyzer.Analyze()
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if len(report.DecisionPoints) != 3 {
		t.Fatalf("expected 3 decision points (one per trigger), got %d", len(report.DecisionPoints))
	}

	// The WORKFLOW CHECK description must be truncated to <= 103 chars
	// (truncate adds a "..." suffix at 100).
	first := report.DecisionPoints[0]
	if len(first.Description) > 103 {
		t.Errorf("decision point description not truncated: len=%d", len(first.Description))
	}
	if first.Sequence == 0 && first.Timestamp.IsZero() {
		t.Error("expected decision point to carry sequence/timestamp metadata")
	}
}

// TestAnalyzePhaseTransitionRecordsPreviousPhase ensures a detected phase
// change closes out the prior phase with a non-empty PhaseAnalysis entry.
func TestAnalyzePhaseTransitionRecordsPreviousPhase(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-PT", "/test/project", tmpDir)
	events := []string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Phase: RESEARCH"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/a.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Phase: VERIFY"}]}}`,
		`{"type":"result","result":"done"}`,
	}
	for _, e := range events {
		_ = recorder.RecordEvent(e)
	}
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	analyzer, _ := NewAnalyzer(recording)
	report, _ := analyzer.Analyze()

	var phases []string
	for _, p := range report.PhaseAnalysis {
		phases = append(phases, p.Phase)
	}

	if len(phases) < 2 {
		t.Fatalf("expected at least 2 phases recorded, got %v", phases)
	}

	foundResearch := false
	foundVerify := false
	for _, p := range phases {
		switch p {
		case "Research":
			foundResearch = true
		case "Verifying":
			foundVerify = true
		}
	}
	if !foundResearch {
		t.Errorf("expected Research phase to be closed out on transition, got %v", phases)
	}
	if !foundVerify {
		t.Errorf("expected Verifying phase as final phase, got %v", phases)
	}
}

func repeatRune(r rune, n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = r
	}
	return string(b)
}
