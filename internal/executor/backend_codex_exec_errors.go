package executor

import (
	"fmt"
	"strings"
)

type CodexExecErrorType string

const (
	CodexExecErrorTypeRateLimit       CodexExecErrorType = "rate_limit"
	CodexExecErrorTypeAPIError        CodexExecErrorType = "api_error"
	CodexExecErrorTypeTimeout         CodexExecErrorType = "timeout"
	CodexExecErrorTypeInvalidConfig   CodexExecErrorType = "invalid_config"
	CodexExecErrorTypeSessionNotFound CodexExecErrorType = "session_not_found"
	CodexExecErrorTypeSandbox         CodexExecErrorType = "sandbox_error"
	CodexExecErrorTypeUnknown         CodexExecErrorType = "unknown"
)

type CodexExecError struct {
	Type    CodexExecErrorType
	Message string
	Stderr  string
}

func (e *CodexExecError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("%s: %s (stderr: %s)", e.Type, e.Message, e.Stderr)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

func (e *CodexExecError) ErrorType() string { return string(e.Type) }

func (e *CodexExecError) ErrorMessage() string { return e.Message }

func (e *CodexExecError) ErrorStderr() string { return e.Stderr }

func classifyCodexExecError(stderr string, originalErr error) *CodexExecError {
	stderrLower := strings.ToLower(stderr)
	trimmed := strings.TrimSpace(stderr)

	if strings.Contains(stderrLower, "rate limit") ||
		strings.Contains(stderrLower, "usage limit") ||
		strings.Contains(stderrLower, "429") {
		return &CodexExecError{Type: CodexExecErrorTypeRateLimit, Message: "Codex rate limit reached", Stderr: trimmed}
	}
	if strings.Contains(stderrLower, "not logged in") ||
		strings.Contains(stderrLower, "unauthorized") ||
		strings.Contains(stderrLower, "authentication") ||
		strings.Contains(stderrLower, "401") ||
		strings.Contains(stderrLower, "403") {
		return &CodexExecError{Type: CodexExecErrorTypeAPIError, Message: "Codex authentication or API error", Stderr: trimmed}
	}
	if strings.Contains(stderrLower, "session not found") ||
		strings.Contains(stderrLower, "unknown thread") ||
		strings.Contains(stderrLower, "thread") && strings.Contains(stderrLower, "not found") {
		return &CodexExecError{Type: CodexExecErrorTypeSessionNotFound, Message: "Codex session not found", Stderr: trimmed}
	}
	if strings.Contains(stderrLower, "invalid model") ||
		strings.Contains(stderrLower, "unknown option") ||
		strings.Contains(stderrLower, "unrecognized") ||
		strings.Contains(stderrLower, "invalid config") {
		return &CodexExecError{Type: CodexExecErrorTypeInvalidConfig, Message: "Invalid Codex exec configuration", Stderr: trimmed}
	}
	if strings.Contains(stderrLower, "sandbox") ||
		strings.Contains(stderrLower, "approval") && strings.Contains(stderrLower, "required") {
		return &CodexExecError{Type: CodexExecErrorTypeSandbox, Message: "Codex sandbox or approval error", Stderr: trimmed}
	}
	if strings.Contains(stderrLower, "killed") ||
		strings.Contains(stderrLower, "signal") ||
		strings.Contains(stderrLower, "timeout") {
		return &CodexExecError{Type: CodexExecErrorTypeTimeout, Message: "Codex process killed or timed out", Stderr: trimmed}
	}

	msg := "Unknown error"
	if originalErr != nil {
		msg = originalErr.Error()
	}
	return &CodexExecError{Type: CodexExecErrorTypeUnknown, Message: msg, Stderr: trimmed}
}
