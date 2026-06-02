package quality

import (
	"strings"
	"testing"
)

func TestParseCoverageOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected float64
	}{
		{
			name:     "go coverage",
			output:   "ok  	github.com/test/pkg	0.123s	coverage: 85.3% of statements",
			expected: 85.3,
		},
		{
			name:     "go coverage simple",
			output:   "coverage: 100.0% of statements",
			expected: 100.0,
		},
		{
			name:     "jest coverage",
			output:   "Statements   : 75.5% ( 100/132 )",
			expected: 75.5,
		},
		{
			name:     "python coverage",
			output:   "TOTAL                                              85%",
			expected: 85.0,
		},
		{
			name:     "no coverage",
			output:   "all tests passed",
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCoverageOutput(tt.output)
			if got != tt.expected {
				t.Errorf("parseCoverageOutput() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFormatErrorFeedback(t *testing.T) {
	results := &CheckResults{
		TaskID:    "test-task",
		AllPassed: false,
		Results: []*Result{
			{
				GateName: "build",
				Status:   StatusPassed,
			},
			{
				GateName: "test",
				Status:   StatusFailed,
				Output:   "FAIL: TestSomething\nexpected 5, got 10",
			},
			{
				GateName: "lint",
				Status:   StatusFailed,
				Output:   "main.go:10: unused variable 'x'",
			},
		},
	}

	feedback := FormatErrorFeedback(results)

	if !strings.Contains(feedback, "Quality Gate Failures") {
		t.Error("expected feedback to contain header")
	}
	if !strings.Contains(feedback, "test Gate") {
		t.Error("expected feedback to contain test gate")
	}
	if !strings.Contains(feedback, "lint Gate") {
		t.Error("expected feedback to contain lint gate")
	}
	if strings.Contains(feedback, "build Gate") {
		t.Error("feedback should not contain passing build gate")
	}
	if !strings.Contains(feedback, "expected 5, got 10") {
		t.Error("expected feedback to contain test error output")
	}
}

func TestShouldRetry(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		results  *CheckResults
		attempt  int
		expected bool
	}{
		{
			name: "should retry on first failure",
			config: &Config{
				OnFailure: FailureConfig{
					Action:     ActionRetry,
					MaxRetries: 2,
				},
			},
			results:  &CheckResults{AllPassed: false},
			attempt:  0,
			expected: true,
		},
		{
			name: "should not retry when passed",
			config: &Config{
				OnFailure: FailureConfig{
					Action:     ActionRetry,
					MaxRetries: 2,
				},
			},
			results:  &CheckResults{AllPassed: true},
			attempt:  0,
			expected: false,
		},
		{
			name: "should not retry when exhausted",
			config: &Config{
				OnFailure: FailureConfig{
					Action:     ActionRetry,
					MaxRetries: 2,
				},
			},
			results:  &CheckResults{AllPassed: false},
			attempt:  2,
			expected: false,
		},
		{
			name: "should not retry when action is fail",
			config: &Config{
				OnFailure: FailureConfig{
					Action:     ActionFail,
					MaxRetries: 2,
				},
			},
			results:  &CheckResults{AllPassed: false},
			attempt:  0,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldRetry(tt.config, tt.results, tt.attempt)
			if got != tt.expected {
				t.Errorf("ShouldRetry() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFormatErrorFeedback_TruncatesLongOutput(t *testing.T) {
	// Create output longer than 2000 characters
	longOutput := strings.Repeat("x", 3000)

	results := &CheckResults{
		TaskID:    "truncate-test",
		AllPassed: false,
		Results: []*Result{
			{
				GateName: "long-output",
				Status:   StatusFailed,
				Output:   longOutput,
			},
		},
	}

	feedback := FormatErrorFeedback(results)

	if len(feedback) >= len(longOutput) {
		t.Error("expected feedback to be truncated")
	}
	if !strings.Contains(feedback, "truncated") {
		t.Error("expected truncation notice in feedback")
	}
}

func TestFormatErrorFeedback_NoFailedGates(t *testing.T) {
	results := &CheckResults{
		TaskID:    "all-passed",
		AllPassed: true,
		Results: []*Result{
			{GateName: "build", Status: StatusPassed},
			{GateName: "test", Status: StatusPassed},
		},
	}

	feedback := FormatErrorFeedback(results)

	// Should still have header but no gate sections
	if !strings.Contains(feedback, "Quality Gate Failures") {
		t.Error("expected header in feedback")
	}
	if strings.Contains(feedback, "build Gate") {
		t.Error("should not contain passed gates")
	}
}

func TestParseCoverageOutput_MultiplePatterns(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected float64
	}{
		{
			name: "go coverage multiline",
			output: `=== RUN TestSomething
--- PASS: TestSomething
PASS
coverage: 78.5% of statements
ok  	github.com/test/pkg	1.234s`,
			expected: 78.5,
		},
		{
			name: "jest lines coverage",
			output: `Lines        : 65.2% ( 45/69 )
Statements   : 70.1% ( 100/142 )`,
			expected: 70.1, // Should get statements (last match)
		},
		{
			name: "python coverage with file breakdown",
			output: `Name                      Stmts   Miss  Cover
---------------------------------------------
mymodule/file1.py            50     10    80%
mymodule/file2.py            30      5    83%
---------------------------------------------
TOTAL                        80     15    81%`,
			expected: 81.0,
		},
		{
			name:     "empty output",
			output:   "",
			expected: 0.0,
		},
		{
			name:     "unrelated output",
			output:   "Build successful\nNo errors found",
			expected: 0.0,
		},
		{
			name:     "coverage at 0 percent",
			output:   "coverage: 0.0% of statements",
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCoverageOutput(tt.output)
			if got != tt.expected {
				t.Errorf("parseCoverageOutput() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestShouldRetry_ActionWarn(t *testing.T) {
	config := &Config{
		OnFailure: FailureConfig{
			Action:     ActionWarn,
			MaxRetries: 5,
		},
	}
	results := &CheckResults{AllPassed: false}

	got := ShouldRetry(config, results, 0)
	if got {
		t.Error("expected ShouldRetry to be false for ActionWarn")
	}
}
