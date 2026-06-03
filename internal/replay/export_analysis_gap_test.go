package replay

import (
	"strings"
	"testing"
	"time"
)

// TestRenderHTMLTokenBreakdown covers the by-phase token bar rendering branch
// (len(ByPhase) > 0) which the higher-level export tests reach only
// incidentally.
func TestRenderHTMLTokenBreakdown(t *testing.T) {
	report := &AnalysisReport{
		Recording: &Recording{
			TokenUsage: &TokenUsage{TotalTokens: 1000},
		},
		TokenBreakdown: TokenBreakdown{
			ByPhase: map[string]TokenUsage{
				"Research": {InputTokens: 300, OutputTokens: 100, TotalTokens: 400},
			},
		},
	}

	html := renderHTMLTokenBreakdown(report)

	if !strings.Contains(html, "Token Breakdown") {
		t.Error("expected Token Breakdown header")
	}
	if !strings.Contains(html, "By Phase") {
		t.Error("expected By Phase sub-header when ByPhase is populated")
	}
	if !strings.Contains(html, "Research") {
		t.Error("expected the phase label to render")
	}
	if !strings.Contains(html, "phase-research") {
		t.Error("expected lowercased phase class for the chart bar")
	}
	// 400 of 1000 = 40.0%
	if !strings.Contains(html, "40.0%") {
		t.Errorf("expected computed percentage in output, got: %s", html)
	}
}

// TestRenderHTMLTokenBreakdownEmpty covers the branch where ByPhase is empty:
// the section header still renders but no chart bars are emitted.
func TestRenderHTMLTokenBreakdownEmpty(t *testing.T) {
	report := &AnalysisReport{
		Recording:      &Recording{TokenUsage: &TokenUsage{TotalTokens: 0}},
		TokenBreakdown: TokenBreakdown{ByPhase: map[string]TokenUsage{}},
	}

	html := renderHTMLTokenBreakdown(report)

	if !strings.Contains(html, "Token Breakdown") {
		t.Error("expected Token Breakdown header even when empty")
	}
	if strings.Contains(html, "By Phase") {
		t.Error("did not expect By Phase sub-header when ByPhase is empty")
	}
}

// TestRenderHTMLPhaseChart covers the phase-timing chart rendering with a
// multi-word phase name to exercise the space-to-hyphen class transform.
func TestRenderHTMLPhaseChart(t *testing.T) {
	report := &AnalysisReport{
		PhaseAnalysis: []PhaseAnalysis{
			{Phase: "Research", Duration: 30 * time.Second, Percentage: 25.0, EventCount: 3},
			{Phase: "In Progress", Duration: 90 * time.Second, Percentage: 75.0, EventCount: 9},
		},
	}

	html := renderHTMLPhaseChart(report, 120*time.Second)

	if !strings.Contains(html, "Phase Timing") {
		t.Error("expected Phase Timing header")
	}
	if !strings.Contains(html, "phase-research") {
		t.Error("expected lowercased single-word phase class")
	}
	if !strings.Contains(html, "phase-in-progress") {
		t.Error("expected spaces replaced with hyphens in phase class")
	}
	if !strings.Contains(html, "75.0%") {
		t.Error("expected percentage rendered in chart value")
	}
}

// TestExportToMarkdownCancelledStatus covers the cancelled status icon branch
// in ExportToMarkdown which the existing markdown tests (completed/failed) do
// not reach.
func TestExportToMarkdownCancelledStatus(t *testing.T) {
	recording := &Recording{
		ID:          "TG-CANCEL",
		TaskID:      "TASK-CANCEL",
		ProjectPath: "/test/project",
		Status:      "cancelled",
		StartTime:   fixedTime,
		EndTime:     fixedTime.Add(time.Minute),
		Duration:    time.Minute,
		EventCount:  1,
	}

	md, err := ExportToMarkdown(recording, nil, nil)
	if err != nil {
		t.Fatalf("ExportToMarkdown: %v", err)
	}

	if !strings.Contains(md, "⚠️ cancelled") {
		t.Errorf("expected cancelled status icon, got: %s", md)
	}
}

// TestExportToMarkdownPhaseTimingTable covers the phase-timing table branch
// (report != nil && len(PhaseAnalysis) > 0) directly with a synthetic report.
func TestExportToMarkdownPhaseTimingTable(t *testing.T) {
	recording := &Recording{
		ID:         "TG-PHASE",
		TaskID:     "TASK-PHASE",
		Status:     "completed",
		StartTime:  fixedTime,
		EndTime:    fixedTime.Add(time.Minute),
		Duration:   time.Minute,
		EventCount: 2,
	}
	report := &AnalysisReport{
		PhaseAnalysis: []PhaseAnalysis{
			{Phase: "Research", Duration: 30 * time.Second, Percentage: 50.0, EventCount: 1},
		},
		ToolUsage: []ToolUsageStats{
			{Tool: "Read", Count: 1},
		},
	}

	md, err := ExportToMarkdown(recording, nil, report)
	if err != nil {
		t.Fatalf("ExportToMarkdown: %v", err)
	}

	if !strings.Contains(md, "## Phase Timing") {
		t.Error("expected Phase Timing section")
	}
	if !strings.Contains(md, "| Research |") {
		t.Error("expected phase row in the phase timing table")
	}
	if !strings.Contains(md, "## Tool Usage") {
		t.Error("expected Tool Usage section")
	}
	if !strings.Contains(md, "| Read |") {
		t.Error("expected tool row in the tool usage table")
	}
}
