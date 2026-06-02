package quality

import (
	"testing"
	"time"
)

func TestGenerateReport(t *testing.T) {
	tests := []struct {
		name          string
		results       *CheckResults
		attempt       int
		expectPassed  bool
		expectSummary string
		expectGateLen int
	}{
		{
			name: "all passed",
			results: &CheckResults{
				AllPassed: true,
				TotalTime: 5 * time.Second,
				Results: []*Result{
					{GateName: "build", Status: StatusPassed, Duration: 2 * time.Second},
					{GateName: "test", Status: StatusPassed, Duration: 3 * time.Second},
				},
			},
			attempt:       0,
			expectPassed:  true,
			expectSummary: "All 2 quality gates passed",
			expectGateLen: 2,
		},
		{
			name: "mixed results",
			results: &CheckResults{
				AllPassed: false,
				TotalTime: 10 * time.Second,
				Results: []*Result{
					{GateName: "build", Status: StatusPassed, Duration: 2 * time.Second},
					{GateName: "test", Status: StatusFailed, Duration: 5 * time.Second, Error: "tests failed"},
					{GateName: "lint", Status: StatusSkipped, Duration: 0},
				},
			},
			attempt:       1,
			expectPassed:  false,
			expectSummary: "1 passed, 1 failed, 1 skipped",
			expectGateLen: 3,
		},
		{
			name: "all failed",
			results: &CheckResults{
				AllPassed: false,
				TotalTime: 3 * time.Second,
				Results: []*Result{
					{GateName: "build", Status: StatusFailed, Duration: 2 * time.Second, Error: "compile error"},
					{GateName: "test", Status: StatusFailed, Duration: 1 * time.Second, Error: "test error"},
				},
			},
			attempt:       2,
			expectPassed:  false,
			expectSummary: "0 passed, 2 failed, 0 skipped",
			expectGateLen: 2,
		},
		{
			name: "with coverage",
			results: &CheckResults{
				AllPassed: true,
				TotalTime: 8 * time.Second,
				Results: []*Result{
					{GateName: "coverage", Status: StatusPassed, Duration: 8 * time.Second, Coverage: 85.5},
				},
			},
			attempt:       0,
			expectPassed:  true,
			expectSummary: "All 1 quality gates passed",
			expectGateLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := GenerateReport("task-123", tt.results, tt.attempt)

			if report.TaskID != "task-123" {
				t.Errorf("expected TaskID 'task-123', got '%s'", report.TaskID)
			}
			if report.Passed != tt.expectPassed {
				t.Errorf("expected Passed %v, got %v", tt.expectPassed, report.Passed)
			}
			if report.Summary != tt.expectSummary {
				t.Errorf("expected Summary '%s', got '%s'", tt.expectSummary, report.Summary)
			}
			if len(report.Gates) != tt.expectGateLen {
				t.Errorf("expected %d gates, got %d", tt.expectGateLen, len(report.Gates))
			}
			if report.Attempt != tt.attempt {
				t.Errorf("expected Attempt %d, got %d", tt.attempt, report.Attempt)
			}
			if report.TotalTime != tt.results.TotalTime {
				t.Errorf("expected TotalTime %v, got %v", tt.results.TotalTime, report.TotalTime)
			}
		})
	}
}

func TestGenerateReport_GateDetails(t *testing.T) {
	results := &CheckResults{
		AllPassed: false,
		TotalTime: 10 * time.Second,
		Results: []*Result{
			{
				GateName: "coverage",
				Status:   StatusPassed,
				Duration: 5 * time.Second,
				Coverage: 92.5,
			},
			{
				GateName: "test",
				Status:   StatusFailed,
				Duration: 3 * time.Second,
				Error:    "assertion failed",
			},
		},
	}

	report := GenerateReport("task-detail", results, 0)

	// Check coverage gate details
	coverageGate := report.Gates[0]
	if coverageGate.Name != "coverage" {
		t.Errorf("expected gate name 'coverage', got '%s'", coverageGate.Name)
	}
	if coverageGate.Status != string(StatusPassed) {
		t.Errorf("expected status 'passed', got '%s'", coverageGate.Status)
	}
	if coverageGate.Coverage != 92.5 {
		t.Errorf("expected coverage 92.5, got %f", coverageGate.Coverage)
	}

	// Check failed gate details
	testGate := report.Gates[1]
	if testGate.Error != "assertion failed" {
		t.Errorf("expected error 'assertion failed', got '%s'", testGate.Error)
	}
}

func TestFormatReportForNotification(t *testing.T) {
	tests := []struct {
		name             string
		report           *GateReport
		expectContains   []string
		expectNotContain []string
	}{
		{
			name: "passed report",
			report: &GateReport{
				TaskID:    "task-1",
				Passed:    true,
				Summary:   "All 2 quality gates passed",
				TotalTime: 5 * time.Second,
				Gates: []GateReportItem{
					{Name: "build", Status: string(StatusPassed), Duration: 2 * time.Second},
					{Name: "test", Status: string(StatusPassed), Duration: 3 * time.Second},
				},
			},
			expectContains:   []string{"Quality Gates Passed", "task-1", "All 2 quality gates passed", "build", "test"},
			expectNotContain: []string{"Quality Gates Failed"},
		},
		{
			name: "failed report",
			report: &GateReport{
				TaskID:    "task-2",
				Passed:    false,
				Summary:   "1 passed, 1 failed",
				TotalTime: 8 * time.Second,
				Gates: []GateReportItem{
					{Name: "build", Status: string(StatusPassed), Duration: 2 * time.Second},
					{Name: "test", Status: string(StatusFailed), Duration: 6 * time.Second, Error: "tests failed"},
				},
			},
			expectContains:   []string{"Quality Gates Failed", "task-2", "tests failed"},
			expectNotContain: []string{"Quality Gates Passed"},
		},
		{
			name: "report with coverage",
			report: &GateReport{
				TaskID:    "task-3",
				Passed:    true,
				Summary:   "All gates passed",
				TotalTime: 10 * time.Second,
				Gates: []GateReportItem{
					{Name: "coverage", Status: string(StatusPassed), Duration: 10 * time.Second, Coverage: 85.3},
				},
			},
			expectContains: []string{"85.3%"},
		},
		{
			name: "report with skipped gate",
			report: &GateReport{
				TaskID:    "task-4",
				Passed:    true,
				Summary:   "1 passed, 0 failed, 1 skipped",
				TotalTime: 5 * time.Second,
				Gates: []GateReportItem{
					{Name: "build", Status: string(StatusPassed), Duration: 5 * time.Second},
					{Name: "optional", Status: string(StatusSkipped), Duration: 0},
				},
			},
			expectContains: []string{"optional"},
		},
		{
			name: "report with pending gate",
			report: &GateReport{
				TaskID:    "task-5",
				Passed:    false,
				Summary:   "0 passed, 0 failed",
				TotalTime: 0,
				Gates: []GateReportItem{
					{Name: "pending-gate", Status: string(StatusPending), Duration: 0},
				},
			},
			expectContains: []string{"pending-gate"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := FormatReportForNotification(tt.report)

			for _, expected := range tt.expectContains {
				if !containsString(output, expected) {
					t.Errorf("expected output to contain '%s', got: %s", expected, output)
				}
			}

			for _, notExpected := range tt.expectNotContain {
				if containsString(output, notExpected) {
					t.Errorf("expected output NOT to contain '%s', got: %s", notExpected, output)
				}
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
