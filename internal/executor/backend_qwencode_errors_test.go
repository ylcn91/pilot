package executor

import (
	"fmt"
	"os/exec"
	"testing"
)

func TestClassifyQwenCodeError(t *testing.T) {
	tests := []struct {
		name       string
		stderr     string
		expectType QwenCodeErrorType
	}{
		{
			name:       "rate limit - 429",
			stderr:     "Error: 429 Too Many Requests",
			expectType: QwenErrorTypeRateLimit,
		},
		{
			name:       "rate limit - rate limit text",
			stderr:     "Error: Rate limit exceeded, try again later",
			expectType: QwenErrorTypeRateLimit,
		},
		{
			name:       "rate limit - hit your limit",
			stderr:     "Error: You've hit your limit",
			expectType: QwenErrorTypeRateLimit,
		},
		{
			name:       "invalid config - invalid model",
			stderr:     "Error: Invalid model specified",
			expectType: QwenErrorTypeInvalidConfig,
		},
		{
			name:       "invalid config - unknown option",
			stderr:     "Error: Unknown option --foobar",
			expectType: QwenErrorTypeInvalidConfig,
		},
		{
			name:       "api error - authentication",
			stderr:     "Error: Authentication failed. Please check your API key.",
			expectType: QwenErrorTypeAPIError,
		},
		{
			name:       "api error - 401",
			stderr:     "HTTP 401: Unauthorized",
			expectType: QwenErrorTypeAPIError,
		},
		{
			name:       "timeout - killed",
			stderr:     "signal: killed",
			expectType: QwenErrorTypeTimeout,
		},
		{
			name:       "timeout - timeout",
			stderr:     "Error: Request timeout",
			expectType: QwenErrorTypeTimeout,
		},
		{
			name:       "unknown error",
			stderr:     "Some random error message",
			expectType: QwenErrorTypeUnknown,
		},
		{
			name:       "empty stderr",
			stderr:     "",
			expectType: QwenErrorTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := classifyQwenCodeError(tt.stderr, nil)
			if err.Type != tt.expectType {
				t.Errorf("classifyQwenCodeError() type = %q, want %q", err.Type, tt.expectType)
			}
			if tt.stderr != "" && err.Stderr != tt.stderr {
				t.Errorf("classifyQwenCodeError() stderr = %q, want %q", err.Stderr, tt.stderr)
			}
		})
	}
}

func TestClassifyQwenCodeError_OOM(t *testing.T) {
	// #26: OOM/SIGKILL detection via exit code, mirroring claude-code.
	tests := []struct {
		name       string
		exitCode   int
		stderr     string
		expectType QwenCodeErrorType
		expectMsg  string
	}{
		{
			name:       "exit 137 (SIGKILL) classified as OOM",
			exitCode:   137,
			expectType: QwenErrorTypeOOM,
			expectMsg:  "Process killed by SIGKILL (exit code 137)",
		},
		{
			name:       "exit 139 (SIGSEGV) classified as OOM",
			exitCode:   139,
			expectType: QwenErrorTypeOOM,
			expectMsg:  "Process killed by SIGSEGV (exit code 139)",
		},
		{
			name:       "exit 137 with stderr still classified as OOM",
			exitCode:   137,
			stderr:     "some noise",
			expectType: QwenErrorTypeOOM,
			expectMsg:  "Process killed by SIGKILL (exit code 137)",
		},
		{
			name:       "exit 1 is not OOM",
			exitCode:   1,
			stderr:     "",
			expectType: QwenErrorTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var exitErr error
			if tt.exitCode > 0 {
				cmd := exec.Command("sh", "-c", fmt.Sprintf("exit %d", tt.exitCode))
				exitErr = cmd.Run()
			}
			err := classifyQwenCodeError(tt.stderr, exitErr)
			if err.Type != tt.expectType {
				t.Errorf("type = %q, want %q", err.Type, tt.expectType)
			}
			if tt.expectMsg != "" && err.Message != tt.expectMsg {
				t.Errorf("message = %q, want %q", err.Message, tt.expectMsg)
			}
		})
	}
}

func TestQwenCodeError_Error(t *testing.T) {
	t.Run("with stderr", func(t *testing.T) {
		err := &QwenCodeError{
			Type:    QwenErrorTypeRateLimit,
			Message: "Rate limit hit",
			Stderr:  "detailed stderr",
		}
		errStr := err.Error()
		if errStr != "rate_limit: Rate limit hit (stderr: detailed stderr)" {
			t.Errorf("Error() = %q, unexpected format", errStr)
		}
	})

	t.Run("without stderr", func(t *testing.T) {
		err := &QwenCodeError{
			Type:    QwenErrorTypeUnknown,
			Message: "Unknown error",
			Stderr:  "",
		}
		errStr := err.Error()
		if errStr != "unknown: Unknown error" {
			t.Errorf("Error() = %q, unexpected format", errStr)
		}
	})
}
