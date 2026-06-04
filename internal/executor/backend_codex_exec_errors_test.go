package executor

import (
	"fmt"
	"os/exec"
	"testing"
)

func TestClassifyCodexExecError(t *testing.T) {
	tests := []struct {
		name       string
		stderr     string
		expectType CodexExecErrorType
	}{
		{
			name:       "rate limit - 429",
			stderr:     "Error: 429 Too Many Requests",
			expectType: CodexExecErrorTypeRateLimit,
		},
		{
			name:       "api error - not logged in",
			stderr:     "Error: not logged in",
			expectType: CodexExecErrorTypeAPIError,
		},
		{
			name:       "session not found",
			stderr:     "Error: session not found",
			expectType: CodexExecErrorTypeSessionNotFound,
		},
		{
			name:       "invalid config - unknown option",
			stderr:     "Error: unknown option --foobar",
			expectType: CodexExecErrorTypeInvalidConfig,
		},
		{
			name:       "sandbox error",
			stderr:     "Error: sandbox denied write",
			expectType: CodexExecErrorTypeSandbox,
		},
		{
			name:       "timeout - killed",
			stderr:     "signal: killed",
			expectType: CodexExecErrorTypeTimeout,
		},
		{
			name:       "unknown",
			stderr:     "some random error",
			expectType: CodexExecErrorTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := classifyCodexExecError(tt.stderr, nil)
			if err.Type != tt.expectType {
				t.Errorf("type = %q, want %q", err.Type, tt.expectType)
			}
		})
	}
}

func TestClassifyCodexExecError_OOM(t *testing.T) {
	// #26: OOM/SIGKILL detection via exit code, mirroring claude-code.
	tests := []struct {
		name       string
		exitCode   int
		stderr     string
		expectType CodexExecErrorType
		expectMsg  string
	}{
		{
			name:       "exit 137 (SIGKILL) classified as OOM",
			exitCode:   137,
			expectType: CodexExecErrorTypeOOM,
			expectMsg:  "Process killed by SIGKILL (exit code 137)",
		},
		{
			name:       "exit 139 (SIGSEGV) classified as OOM",
			exitCode:   139,
			expectType: CodexExecErrorTypeOOM,
			expectMsg:  "Process killed by SIGSEGV (exit code 139)",
		},
		{
			name:       "exit 137 with stderr still classified as OOM",
			exitCode:   137,
			stderr:     "some noise",
			expectType: CodexExecErrorTypeOOM,
			expectMsg:  "Process killed by SIGKILL (exit code 137)",
		},
		{
			name:       "exit 1 is not OOM",
			exitCode:   1,
			stderr:     "",
			expectType: CodexExecErrorTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var exitErr error
			if tt.exitCode > 0 {
				cmd := exec.Command("sh", "-c", fmt.Sprintf("exit %d", tt.exitCode))
				exitErr = cmd.Run()
			}
			err := classifyCodexExecError(tt.stderr, exitErr)
			if err.Type != tt.expectType {
				t.Errorf("type = %q, want %q", err.Type, tt.expectType)
			}
			if tt.expectMsg != "" && err.Message != tt.expectMsg {
				t.Errorf("message = %q, want %q", err.Message, tt.expectMsg)
			}
		})
	}
}
