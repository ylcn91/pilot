package telegram

import (
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/executor"
)

func TestFormatTaskResult(t *testing.T) {
	tests := []struct {
		name     string
		result   *executor.ExecutionResult
		contains []string
	}{
		{
			name: "success result",
			result: &executor.ExecutionResult{
				TaskID:   "TG-123",
				Success:  true,
				Duration: 45 * time.Second,
				Output:   "Created auth.go",
			},
			contains: []string{"✅", "TG-123", "45s"},
		},
		{
			name: "success with commit",
			result: &executor.ExecutionResult{
				TaskID:    "TG-456",
				Success:   true,
				Duration:  30 * time.Second,
				CommitSHA: "abc12345def",
			},
			contains: []string{"✅", "Commit:", "abc12345"},
		},
		{
			name: "success with PR",
			result: &executor.ExecutionResult{
				TaskID:   "TG-789",
				Success:  true,
				Duration: 60 * time.Second,
				PRUrl:    "https://github.com/org/repo/pull/123",
			},
			contains: []string{"✅", "PR:", "github.com"},
		},
		{
			name: "failure result",
			result: &executor.ExecutionResult{
				TaskID:   "TG-ERR",
				Success:  false,
				Duration: 10 * time.Second,
				Error:    "Build failed: missing dependency",
			},
			contains: []string{"❌", "failed", "missing dependency"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatTaskResult(tt.result)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("FormatTaskResult() = %q, want to contain %q", got, want)
				}
			}
		})
	}
}

// TestFormatSuccessResultWithFiles tests output with file operations
func TestFormatSuccessResultWithFiles(t *testing.T) {
	result := &executor.ExecutionResult{
		TaskID:   "TASK-01",
		Success:  true,
		Duration: 30 * time.Second,
		Output:   "Created `auth.go`\nModified `main.go`\nAdded `test.go`",
	}

	got := FormatTaskResult(result)

	wantContains := []string{"✅", "TASK-01", "30s", "auth.go", "main.go", "test.go"}
	for _, want := range wantContains {
		if !strings.Contains(got, want) {
			t.Errorf("FormatTaskResult() should contain %q, got:\n%s", want, got)
		}
	}
}

// TestFormatFailureResultTruncation tests error truncation
func TestFormatFailureResultTruncation(t *testing.T) {
	longError := strings.Repeat("error ", 100)
	result := &executor.ExecutionResult{
		TaskID:   "TASK-ERR",
		Success:  false,
		Duration: 5 * time.Second,
		Error:    longError,
	}

	got := FormatTaskResult(result)

	if len(got) > 600 { // Error truncated to 400 + metadata
		t.Errorf("FormatTaskResult() too long: %d chars", len(got))
	}
	if !strings.Contains(got, "...") {
		t.Error("FormatTaskResult() should contain truncation indicator")
	}
}

// TestFormatFailureResultEmpty tests failure with empty error
func TestFormatFailureResultEmpty(t *testing.T) {
	result := &executor.ExecutionResult{
		TaskID:   "TASK-ERR",
		Success:  false,
		Duration: 5 * time.Second,
		Error:    "",
	}

	got := FormatTaskResult(result)

	if !strings.Contains(got, "Unknown error") {
		t.Errorf("FormatTaskResult() should contain 'Unknown error' for empty error, got:\n%s", got)
	}
}

// TestFormatTaskResultSuccessNoOutput tests success with no output
func TestFormatTaskResultSuccessNoOutput(t *testing.T) {
	result := &executor.ExecutionResult{
		TaskID:   "TASK-01",
		Success:  true,
		Duration: 10 * time.Second,
		Output:   "",
	}

	got := FormatTaskResult(result)

	if !strings.Contains(got, "completed") {
		t.Errorf("FormatTaskResult() should contain 'completed', got:\n%s", got)
	}
	if strings.Contains(got, "Summary") {
		t.Error("FormatTaskResult() should not have Summary section for empty output")
	}
}

// TestFormatTaskResultSuccessWithInternalSignals tests cleanup in success output
func TestFormatTaskResultSuccessWithInternalSignals(t *testing.T) {
	result := &executor.ExecutionResult{
		TaskID:   "TASK-01",
		Success:  true,
		Duration: 20 * time.Second,
		Output:   "Created file.go\nEXIT_SIGNAL: true\nLOOP COMPLETE",
	}

	got := FormatTaskResult(result)

	if strings.Contains(got, "EXIT_SIGNAL") {
		t.Error("FormatTaskResult() should clean EXIT_SIGNAL from output")
	}
	if strings.Contains(got, "LOOP COMPLETE") {
		t.Error("FormatTaskResult() should clean LOOP COMPLETE from output")
	}
}
