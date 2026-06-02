package executor

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// ClaudeCodeErrorType categorizes different types of Claude Code failures.
// GH-917: Better error classification enables smarter retry decisions.
type ClaudeCodeErrorType string

const (
	// ErrorTypeRateLimit indicates Claude Code hit a rate limit
	ErrorTypeRateLimit ClaudeCodeErrorType = "rate_limit"
	// ErrorTypeInvalidConfig indicates invalid configuration (e.g., --effort max)
	ErrorTypeInvalidConfig ClaudeCodeErrorType = "invalid_config"
	// ErrorTypeAPIError indicates Claude API errors (auth, server errors)
	ErrorTypeAPIError ClaudeCodeErrorType = "api_error"
	// ErrorTypeTimeout indicates the process was killed due to timeout
	ErrorTypeTimeout ClaudeCodeErrorType = "timeout"
	// ErrorTypeOOM indicates the process was OOM-killed (exit 137/139) (GH-2112)
	ErrorTypeOOM ClaudeCodeErrorType = "oom_killed"
	// ErrorTypeSessionNotFound indicates the session for --from-pr or --resume was not found (GH-1267)
	ErrorTypeSessionNotFound ClaudeCodeErrorType = "session_not_found"
	// ErrorTypeNoChanges indicates Claude exited 0 but produced no diff — typically
	// a refusal or "task already done" response. The final assistant message holds
	// the refusal reason. GH-2328.
	ErrorTypeNoChanges ClaudeCodeErrorType = "no_changes"
	// ErrorTypeDeclined indicates Claude explicitly declined the task as unactionable,
	// emitting a structured DECLINED:<reason> marker in its response. GH-2777.
	ErrorTypeDeclined ClaudeCodeErrorType = "declined"
	// ErrorTypeUnknown indicates an unclassified error
	ErrorTypeUnknown ClaudeCodeErrorType = "unknown"
)

// ClaudeCodeError represents a classified error from Claude Code.
type ClaudeCodeError struct {
	Type    ClaudeCodeErrorType
	Message string
	Stderr  string
}

func (e *ClaudeCodeError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("%s: %s (stderr: %s)", e.Type, e.Message, e.Stderr)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

// ErrorType implements BackendError.
func (e *ClaudeCodeError) ErrorType() string { return string(e.Type) }

// ErrorMessage implements BackendError.
func (e *ClaudeCodeError) ErrorMessage() string { return e.Message }

// ErrorStderr implements BackendError.
func (e *ClaudeCodeError) ErrorStderr() string { return e.Stderr }

// classifyClaudeCodeError examines stderr output and exit code to classify the error.
func classifyClaudeCodeError(stderr string, originalErr error) *ClaudeCodeError {
	// GH-2112: Check exit code first — OOM kills (137=SIGKILL, 139=SIGSEGV) often
	// produce no stderr, so exit code is the only reliable signal.
	if exitCode := extractExitCode(originalErr); exitCode == 137 || exitCode == 139 {
		sigName := "SIGKILL"
		if exitCode == 139 {
			sigName = "SIGSEGV"
		}
		return &ClaudeCodeError{
			Type:    ErrorTypeOOM,
			Message: fmt.Sprintf("Process killed by %s (exit code %d)", sigName, exitCode),
			Stderr:  strings.TrimSpace(stderr),
		}
	}

	stderrLower := strings.ToLower(stderr)

	// Rate limit detection
	if strings.Contains(stderrLower, "hit your limit") ||
		strings.Contains(stderrLower, "rate limit") ||
		strings.Contains(stderrLower, "resets") && strings.Contains(stderrLower, "limit") {
		return &ClaudeCodeError{
			Type:    ErrorTypeRateLimit,
			Message: "Claude Code rate limit reached",
			Stderr:  strings.TrimSpace(stderr),
		}
	}

	// Invalid config detection (effort level, model, etc.)
	if strings.Contains(stderrLower, "effort level") ||
		strings.Contains(stderrLower, "is not available") ||
		strings.Contains(stderrLower, "invalid model") ||
		strings.Contains(stderrLower, "requires --verbose") {
		return &ClaudeCodeError{
			Type:    ErrorTypeInvalidConfig,
			Message: "Invalid Claude Code configuration",
			Stderr:  strings.TrimSpace(stderr),
		}
	}

	// API errors
	if strings.Contains(stderrLower, "api error") ||
		strings.Contains(stderrLower, "authentication") ||
		strings.Contains(stderrLower, "unauthorized") ||
		strings.Contains(stderrLower, "403") ||
		strings.Contains(stderrLower, "401") {
		return &ClaudeCodeError{
			Type:    ErrorTypeAPIError,
			Message: "Claude API error",
			Stderr:  strings.TrimSpace(stderr),
		}
	}

	// Session not found (GH-1267: --from-pr or --resume failed)
	// GH-2377: include "no conversation found" — CC emits this exact phrase
	// when --resume targets an evicted/expired session ID.
	if strings.Contains(stderrLower, "session not found") ||
		strings.Contains(stderrLower, "no session") ||
		strings.Contains(stderrLower, "no conversation found") ||
		strings.Contains(stderrLower, "session expired") ||
		strings.Contains(stderrLower, "could not find session") ||
		strings.Contains(stderrLower, "invalid session") {
		return &ClaudeCodeError{
			Type:    ErrorTypeSessionNotFound,
			Message: "Session not found for --from-pr or --resume",
			Stderr:  strings.TrimSpace(stderr),
		}
	}

	// Timeout/killed
	if strings.Contains(stderrLower, "killed") ||
		strings.Contains(stderrLower, "signal") ||
		strings.Contains(stderrLower, "timeout") {
		return &ClaudeCodeError{
			Type:    ErrorTypeTimeout,
			Message: "Process killed or timed out",
			Stderr:  strings.TrimSpace(stderr),
		}
	}

	// Unknown error
	msg := "Unknown error"
	if originalErr != nil {
		msg = originalErr.Error()
	}
	return &ClaudeCodeError{
		Type:    ErrorTypeUnknown,
		Message: msg,
		Stderr:  strings.TrimSpace(stderr),
	}
}

// extractExitCode returns the process exit code from an exec.ExitError, or -1 if unavailable.
func extractExitCode(err error) int {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return -1
	}
	// On Unix, check for signal-based termination (128+signal)
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
		if ws.Signaled() {
			return 128 + int(ws.Signal())
		}
	}
	return exitErr.ExitCode()
}

// parseClaudeCodeError examines stderr output and exit code to classify the error.
// This function matches the specification in GH-917 and returns error interface.
func parseClaudeCodeError(stderr string, originalErr error) error {
	return classifyClaudeCodeError(stderr, originalErr)
}
