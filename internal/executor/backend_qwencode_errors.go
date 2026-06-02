package executor

import (
	"fmt"
	"strings"
)

// QwenCodeErrorType categorizes different types of Qwen Code failures.
type QwenCodeErrorType string

const (
	QwenErrorTypeRateLimit       QwenCodeErrorType = "rate_limit"
	QwenErrorTypeAPIError        QwenCodeErrorType = "api_error"
	QwenErrorTypeTimeout         QwenCodeErrorType = "timeout"
	QwenErrorTypeInvalidConfig   QwenCodeErrorType = "invalid_config"
	QwenErrorTypeSessionNotFound QwenCodeErrorType = "session_not_found"
	QwenErrorTypeUnknown         QwenCodeErrorType = "unknown"
)

// QwenCodeError represents a classified error from Qwen Code.
type QwenCodeError struct {
	Type    QwenCodeErrorType
	Message string
	Stderr  string
}

func (e *QwenCodeError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("%s: %s (stderr: %s)", e.Type, e.Message, e.Stderr)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

// ErrorType implements BackendError.
func (e *QwenCodeError) ErrorType() string { return string(e.Type) }

// ErrorMessage implements BackendError.
func (e *QwenCodeError) ErrorMessage() string { return e.Message }

// ErrorStderr implements BackendError.
func (e *QwenCodeError) ErrorStderr() string { return e.Stderr }

// classifyQwenCodeError examines stderr output to classify the error.
func classifyQwenCodeError(stderr string, originalErr error) *QwenCodeError {
	stderrLower := strings.ToLower(stderr)

	// Rate limit detection
	if strings.Contains(stderrLower, "rate limit") ||
		strings.Contains(stderrLower, "hit your limit") ||
		strings.Contains(stderrLower, "too many requests") ||
		strings.Contains(stderrLower, "429") {
		return &QwenCodeError{
			Type:    QwenErrorTypeRateLimit,
			Message: "Qwen Code rate limit reached",
			Stderr:  strings.TrimSpace(stderr),
		}
	}

	// Invalid config detection
	if strings.Contains(stderrLower, "invalid model") ||
		strings.Contains(stderrLower, "not available") ||
		strings.Contains(stderrLower, "unknown option") ||
		strings.Contains(stderrLower, "unrecognized") {
		return &QwenCodeError{
			Type:    QwenErrorTypeInvalidConfig,
			Message: "Invalid Qwen Code configuration",
			Stderr:  strings.TrimSpace(stderr),
		}
	}

	// API errors
	if strings.Contains(stderrLower, "api error") ||
		strings.Contains(stderrLower, "authentication") ||
		strings.Contains(stderrLower, "unauthorized") ||
		strings.Contains(stderrLower, "403") ||
		strings.Contains(stderrLower, "401") {
		return &QwenCodeError{
			Type:    QwenErrorTypeAPIError,
			Message: "Qwen API error",
			Stderr:  strings.TrimSpace(stderr),
		}
	}

	// Session not found/expired
	if strings.Contains(stderrLower, "session not found") ||
		strings.Contains(stderrLower, "session expired") ||
		strings.Contains(stderrLower, "invalid session") {
		return &QwenCodeError{
			Type:    QwenErrorTypeSessionNotFound,
			Message: "Qwen session not found",
			Stderr:  strings.TrimSpace(stderr),
		}
	}

	// Timeout/killed
	if strings.Contains(stderrLower, "killed") ||
		strings.Contains(stderrLower, "signal") ||
		strings.Contains(stderrLower, "timeout") {
		return &QwenCodeError{
			Type:    QwenErrorTypeTimeout,
			Message: "Process killed or timed out",
			Stderr:  strings.TrimSpace(stderr),
		}
	}

	// Unknown error
	msg := "Unknown error"
	if originalErr != nil {
		msg = originalErr.Error()
	}
	return &QwenCodeError{
		Type:    QwenErrorTypeUnknown,
		Message: msg,
		Stderr:  strings.TrimSpace(stderr),
	}
}
