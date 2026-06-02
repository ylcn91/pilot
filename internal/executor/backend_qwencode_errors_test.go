package executor

import (
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
