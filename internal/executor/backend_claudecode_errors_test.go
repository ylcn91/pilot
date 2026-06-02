package executor

import (
	"fmt"
	"os/exec"
	"runtime"
	"testing"
)

func TestClassifyClaudeCodeError(t *testing.T) {
	tests := []struct {
		name       string
		stderr     string
		expectType ClaudeCodeErrorType
	}{
		{
			name:       "rate limit - hit your limit",
			stderr:     "Error: You've hit your limit · resets 6am (Europe/Podgorica)",
			expectType: ErrorTypeRateLimit,
		},
		{
			name:       "rate limit - rate limit",
			stderr:     "Error: Rate limit exceeded, try again later",
			expectType: ErrorTypeRateLimit,
		},
		{
			name:       "invalid config - effort level",
			stderr:     `Error: Effort level "max" is not available for Claude.ai subscribers`,
			expectType: ErrorTypeInvalidConfig,
		},
		{
			name:       "invalid config - requires verbose",
			stderr:     "Error: When using --print, --output-format=stream-json requires --verbose",
			expectType: ErrorTypeInvalidConfig,
		},
		{
			name:       "api error - authentication",
			stderr:     "Error: Authentication failed. Please check your API key.",
			expectType: ErrorTypeAPIError,
		},
		{
			name:       "api error - 401",
			stderr:     "HTTP 401: Unauthorized",
			expectType: ErrorTypeAPIError,
		},
		{
			name:       "timeout - killed",
			stderr:     "signal: killed",
			expectType: ErrorTypeTimeout,
		},
		{
			// GH-2377: CC emits "No conversation found with session ID: <uuid>"
			// when --resume targets an evicted session. Must classify as
			// session_not_found so the --resume fallback triggers.
			name:       "session not found - no conversation found",
			stderr:     "No conversation found with session ID: 723e1e6e-0253-45ae-a7e4-e79112d73deb",
			expectType: ErrorTypeSessionNotFound,
		},
		{
			name:       "session not found - classic phrasing",
			stderr:     "Error: session not found",
			expectType: ErrorTypeSessionNotFound,
		},
		{
			name:       "unknown error",
			stderr:     "Some random error message",
			expectType: ErrorTypeUnknown,
		},
		{
			name:       "empty stderr",
			stderr:     "",
			expectType: ErrorTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := classifyClaudeCodeError(tt.stderr, nil)
			if err.Type != tt.expectType {
				t.Errorf("classifyClaudeCodeError() type = %q, want %q", err.Type, tt.expectType)
			}
			// Verify stderr is captured
			if tt.stderr != "" && err.Stderr != tt.stderr {
				t.Errorf("classifyClaudeCodeError() stderr = %q, want %q", err.Stderr, tt.stderr)
			}
		})
	}
}

func TestParseClaudeCodeError(t *testing.T) {
	tests := []struct {
		name       string
		stderr     string
		expectType ClaudeCodeErrorType
	}{
		{
			name:       "rate limit error",
			stderr:     "Error: You've hit your limit · resets 6am (Europe/Podgorica)",
			expectType: ErrorTypeRateLimit,
		},
		{
			name:       "invalid config error",
			stderr:     `Error: Effort level "max" is not available for Claude.ai subscribers`,
			expectType: ErrorTypeInvalidConfig,
		},
		{
			name:       "api error",
			stderr:     "Error: Authentication failed. Please check your API key.",
			expectType: ErrorTypeAPIError,
		},
		{
			name:       "timeout error",
			stderr:     "signal: killed",
			expectType: ErrorTypeTimeout,
		},
		{
			name:       "unknown error",
			stderr:     "Something completely unexpected happened",
			expectType: ErrorTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseClaudeCodeError(tt.stderr, nil)
			ccErr, ok := err.(*ClaudeCodeError)
			if !ok {
				t.Errorf("parseClaudeCodeError() did not return *ClaudeCodeError, got %T", err)
				return
			}
			if ccErr.Type != tt.expectType {
				t.Errorf("parseClaudeCodeError() type = %q, want %q", ccErr.Type, tt.expectType)
			}
			if tt.stderr != "" && ccErr.Stderr != tt.stderr {
				t.Errorf("parseClaudeCodeError() stderr = %q, want %q", ccErr.Stderr, tt.stderr)
			}
		})
	}
}

func TestSawSuccessResultRecovery(t *testing.T) {
	// GH-2107: When a successful result event was seen but the process exits with
	// an error (e.g., timeout on final summary), SawSuccessResult should be set.
	t.Run("successful result sets SawSuccessResult", func(t *testing.T) {
		backend := NewClaudeCodeBackend(nil)
		event := backend.parseStreamEvent(`{"type":"result","result":"All tasks completed.","is_error":false}`)
		if event.Type != EventTypeResult {
			t.Fatalf("expected result event, got %s", event.Type)
		}
		if event.IsError {
			t.Fatal("expected non-error result")
		}

		// Simulate what executeWithFromPR does: set SawSuccessResult when result is not error
		result := &BackendResult{}
		if event.Type == EventTypeResult && !event.IsError {
			result.Output = event.Message
			result.SawSuccessResult = true
		}

		if !result.SawSuccessResult {
			t.Error("SawSuccessResult should be true for non-error result")
		}
		if result.Output != "All tasks completed." {
			t.Errorf("Output = %q, want %q", result.Output, "All tasks completed.")
		}
	})

	t.Run("error result does not set SawSuccessResult", func(t *testing.T) {
		backend := NewClaudeCodeBackend(nil)
		event := backend.parseStreamEvent(`{"type":"result","result":"Failed to complete","is_error":true}`)

		result := &BackendResult{}
		if event.Type == EventTypeResult {
			if event.IsError {
				result.Error = event.Message
			} else {
				result.SawSuccessResult = true
			}
		}

		if result.SawSuccessResult {
			t.Error("SawSuccessResult should be false for error result")
		}
	})

	t.Run("no result event does not set SawSuccessResult", func(t *testing.T) {
		result := &BackendResult{}
		if result.SawSuccessResult {
			t.Error("SawSuccessResult should default to false")
		}
	})
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		n        int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.n)
		if got != tt.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.expected)
		}
	}
}

func TestClassifyClaudeCodeError_OOM(t *testing.T) {
	// GH-2112: OOM/SIGKILL detection via exit code
	tests := []struct {
		name       string
		exitCode   int
		stderr     string
		expectType ClaudeCodeErrorType
		expectMsg  string
	}{
		{
			name:       "exit 137 (SIGKILL) classified as OOM",
			exitCode:   137,
			stderr:     "",
			expectType: ErrorTypeOOM,
			expectMsg:  "Process killed by SIGKILL (exit code 137)",
		},
		{
			name:       "exit 139 (SIGSEGV) classified as OOM",
			exitCode:   139,
			stderr:     "",
			expectType: ErrorTypeOOM,
			expectMsg:  "Process killed by SIGSEGV (exit code 139)",
		},
		{
			name:       "exit 137 with stderr still classified as OOM",
			exitCode:   137,
			stderr:     "some output before death",
			expectType: ErrorTypeOOM,
			expectMsg:  "Process killed by SIGKILL (exit code 137)",
		},
		{
			name:       "exit 1 with empty stderr is not OOM",
			exitCode:   1,
			stderr:     "",
			expectType: ErrorTypeUnknown,
		},
		{
			name:       "exit 1 with rate limit stderr",
			exitCode:   1,
			stderr:     "Error: You've hit your limit",
			expectType: ErrorTypeRateLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a real exec.ExitError by running a process that exits with the desired code
			var exitErr error
			if tt.exitCode > 0 {
				cmd := exec.Command("sh", "-c", fmt.Sprintf("exit %d", tt.exitCode))
				exitErr = cmd.Run()
			}
			err := classifyClaudeCodeError(tt.stderr, exitErr)
			if err.Type != tt.expectType {
				t.Errorf("type = %q, want %q", err.Type, tt.expectType)
			}
			if tt.expectMsg != "" && err.Message != tt.expectMsg {
				t.Errorf("message = %q, want %q", err.Message, tt.expectMsg)
			}
		})
	}
}

func TestExtractExitCode(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
		expected int
	}{
		{"exit 0 (no error)", 0, -1}, // cmd.Run() returns nil for exit 0
		{"exit 1", 1, 1},
		{"exit 137", 137, 137},
		{"exit 139", 139, 139},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("signal-based exit codes are Unix-only")
			}
			cmd := exec.Command("sh", "-c", fmt.Sprintf("exit %d", tt.exitCode))
			err := cmd.Run()
			got := extractExitCode(err)
			if got != tt.expected {
				t.Errorf("extractExitCode() = %d, want %d", got, tt.expected)
			}
		})
	}

	t.Run("nil error returns -1", func(t *testing.T) {
		if got := extractExitCode(nil); got != -1 {
			t.Errorf("extractExitCode(nil) = %d, want -1", got)
		}
	})

	t.Run("non-ExitError returns -1", func(t *testing.T) {
		if got := extractExitCode(fmt.Errorf("some error")); got != -1 {
			t.Errorf("extractExitCode(non-ExitError) = %d, want -1", got)
		}
	})
}

func TestClaudeCodeError_Error(t *testing.T) {
	t.Run("with stderr", func(t *testing.T) {
		err := &ClaudeCodeError{
			Type:    ErrorTypeRateLimit,
			Message: "Rate limit hit",
			Stderr:  "detailed stderr",
		}
		errStr := err.Error()
		if errStr != "rate_limit: Rate limit hit (stderr: detailed stderr)" {
			t.Errorf("Error() = %q, unexpected format", errStr)
		}
	})

	t.Run("without stderr", func(t *testing.T) {
		err := &ClaudeCodeError{
			Type:    ErrorTypeUnknown,
			Message: "Unknown error",
			Stderr:  "",
		}
		errStr := err.Error()
		if errStr != "unknown: Unknown error" {
			t.Errorf("Error() = %q, unexpected format", errStr)
		}
	})
}

// TestErrorTypeNoChanges verifies that the no_changes classification constant
// is defined and distinguishable from other error types. GH-2328.
func TestErrorTypeNoChanges(t *testing.T) {
	if ErrorTypeNoChanges != "no_changes" {
		t.Errorf("ErrorTypeNoChanges = %q, want %q", ErrorTypeNoChanges, "no_changes")
	}

	// Must not collide with any other classification the runner already depends on.
	others := []ClaudeCodeErrorType{
		ErrorTypeRateLimit,
		ErrorTypeInvalidConfig,
		ErrorTypeAPIError,
		ErrorTypeTimeout,
		ErrorTypeOOM,
		ErrorTypeSessionNotFound,
		ErrorTypeUnknown,
	}
	for _, o := range others {
		if ErrorTypeNoChanges == o {
			t.Errorf("ErrorTypeNoChanges collides with %q", o)
		}
	}

	// A ClaudeCodeError carrying no_changes must render the final assistant
	// text via Error() so the autopilot failure comment surfaces the refusal.
	err := &ClaudeCodeError{
		Type:    ErrorTypeNoChanges,
		Message: "refused: this task is out of scope",
	}
	if got := err.Error(); got != "no_changes: refused: this task is out of scope" {
		t.Errorf("Error() = %q, want %q", got, "no_changes: refused: this task is out of scope")
	}
}
