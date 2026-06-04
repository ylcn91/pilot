// Package httpretry provides shared HTTP retry/backoff primitives for the
// adapter clients (github, gitlab, jira, linear, azuredevops). It centralizes
// the exponential-backoff loop, the typed *RateLimitError, and the
// Retry-After / status-code classification that previously lived only in the
// github adapter, so every adapter handles 429 + transient 5xx the same way.
package httpretry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RetryOptions configures retry behavior.
type RetryOptions struct {
	MaxRetries int           // Maximum number of retries (default: 3)
	BaseDelay  time.Duration // Initial delay between retries (default: 1s)
	MaxDelay   time.Duration // Maximum delay between retries (default: 30s)
}

// DefaultRetryOptions returns sensible defaults for retry behavior.
func DefaultRetryOptions() RetryOptions {
	return RetryOptions{
		MaxRetries: 3,
		BaseDelay:  1 * time.Second,
		MaxDelay:   30 * time.Second,
	}
}

// RateLimitError is returned by an HTTP client when the API signals a rate
// limit via a 403 or 429 response. It carries the parsed Retry-After duration
// so the retry loop can honor it without regexing the error string.
type RateLimitError struct {
	StatusCode int
	RetryAfter time.Duration
	Message    string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Message)
}

// APIError is returned for non-2xx responses that are not rate limits.
// Carrying the status code lets callers branch with errors.As instead of
// matching the formatted message string.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Message)
}

// ParseRetryAfterHeader reads Retry-After and X-RateLimit-Reset headers and
// returns the delay duration. Returns 0 when neither header is present.
func ParseRetryAfterHeader(h http.Header) time.Duration {
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	if v := h.Get("X-RateLimit-Reset"); v != "" {
		if unix, err := strconv.ParseInt(v, 10, 64); err == nil {
			if d := time.Until(time.Unix(unix, 0)); d > 0 {
				return d
			}
		}
	}
	return 0
}

// ClassifyResponse turns a non-2xx HTTP response into a typed error. It returns
// a *RateLimitError for 429 (and 403 responses that look like rate limits) and
// an *APIError for every other non-2xx status. The caller passes the already-read
// response body to avoid re-reading the stream.
func ClassifyResponse(resp *http.Response, body []byte) error {
	msg := string(body)
	if resp.StatusCode == http.StatusTooManyRequests {
		return &RateLimitError{
			StatusCode: http.StatusTooManyRequests,
			RetryAfter: ParseRetryAfterHeader(resp.Header),
			Message:    msg,
		}
	}
	if resp.StatusCode == http.StatusForbidden {
		msgLower := strings.ToLower(msg)
		isRateLimit := resp.Header.Get("X-RateLimit-Remaining") == "0" ||
			strings.Contains(msgLower, "secondary rate limit") ||
			strings.Contains(msgLower, "rate limit exceeded")
		if isRateLimit {
			return &RateLimitError{
				StatusCode: http.StatusForbidden,
				RetryAfter: ParseRetryAfterHeader(resp.Header),
				Message:    msg,
			}
		}
	}
	return &APIError{StatusCode: resp.StatusCode, Message: msg}
}

// WithRetry executes an operation with exponential backoff retry.
// It respects context cancellation and any Retry-After delay carried
// by a *RateLimitError returned from the operation.
func WithRetry[T any](ctx context.Context, op func() (T, error), opts RetryOptions) (T, error) {
	var result T
	var lastErr error

	for attempt := 0; attempt <= opts.MaxRetries; attempt++ {
		result, lastErr = op()
		if lastErr == nil {
			return result, nil
		}

		// Don't retry non-retryable errors
		if !IsRetryableError(lastErr) {
			return result, lastErr
		}

		// Don't retry if we've exhausted retries
		if attempt >= opts.MaxRetries {
			return result, lastErr
		}

		// Calculate delay with exponential backoff: 1s, 2s, 4s, 8s...
		delay := opts.BaseDelay * time.Duration(1<<uint(attempt))
		if delay > opts.MaxDelay {
			delay = opts.MaxDelay
		}

		// Check for Retry-After header in rate limit errors; cap at MaxDelay so
		// a runaway "Retry-After: 3600" can't stall the worker for an hour.
		if retryAfter := ExtractRetryAfter(lastErr); retryAfter > 0 {
			if retryAfter > opts.MaxDelay {
				retryAfter = opts.MaxDelay
			}
			delay = retryAfter
		}

		// Wait with context cancellation support
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(delay):
			// Continue to next retry attempt
		}
	}

	return result, lastErr
}

// WithRetryVoid is like WithRetry but for operations that don't return a value.
func WithRetryVoid(ctx context.Context, op func() error, opts RetryOptions) error {
	_, err := WithRetry(ctx, func() (struct{}, error) {
		return struct{}{}, op()
	}, opts)
	return err
}

// IsRetryableError determines if an error is transient and should be retried.
// Returns true for:
// - 429 Too Many Requests (rate limiting)
// - 500, 502, 503, 504 (server errors)
// - Network/connection errors
// Returns false for:
// - 400 Bad Request
// - 401 Unauthorized
// - 403 Forbidden (non-rate-limit)
// - 404 Not Found
// - 422 Unprocessable Entity
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// RateLimitError (403 secondary rate-limit or 429) is always retryable.
	var rlErr *RateLimitError
	if errors.As(err, &rlErr) {
		return true
	}

	errStr := err.Error()

	// Check for retryable HTTP status codes
	retryableStatuses := []string{
		"status 429", // Rate limited
		"status 500", // Internal Server Error
		"status 502", // Bad Gateway
		"status 503", // Service Unavailable
		"status 504", // Gateway Timeout
	}

	for _, status := range retryableStatuses {
		if strings.Contains(errStr, status) {
			return true
		}
	}

	// GraphQL rate limit errors: HTTP 200 but error body signals rate-limiting.
	// GitHub Projects V2 returns these as GraphQL errors rather than HTTP 429.
	if strings.Contains(errStr, "RATE_LIMITED") || strings.Contains(errStr, "was submitted too quickly") {
		return true
	}

	// Check for network errors (these don't have HTTP status)
	networkErrors := []string{
		"connection refused",
		"connection reset",
		"no such host",
		"network is unreachable",
		"i/o timeout",
		"context deadline exceeded",
		"dial tcp",
	}

	errLower := strings.ToLower(errStr)
	for _, netErr := range networkErrors {
		if strings.Contains(errLower, netErr) {
			return true
		}
	}

	return false
}

// ExtractRetryAfter extracts the Retry-After duration from a rate limit error.
// APIs include this header in 429 responses indicating when the client can retry.
// Returns 0 if no Retry-After information is found.
func ExtractRetryAfter(err error) time.Duration {
	if err == nil {
		return 0
	}

	// Prefer the header-parsed duration encoded in the typed error.
	var rlErr *RateLimitError
	if errors.As(err, &rlErr) {
		if rlErr.RetryAfter > 0 {
			return rlErr.RetryAfter
		}
		// No header present: use a sensible default for any rate limit.
		return 60 * time.Second
	}

	errStr := err.Error()

	// Legacy string-scanning path for errors not produced by a typed client.
	// Look for patterns like "retry after X seconds" or "Retry-After: X"
	patterns := []string{
		`retry.after[:\s]+(\d+)`,
		`Retry-After[:\s]+(\d+)`,
		`rate.limit.*?(\d+)\s*seconds?`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile("(?i)" + pattern)
		matches := re.FindStringSubmatch(errStr)
		if len(matches) > 1 {
			if seconds, parseErr := strconv.Atoi(matches[1]); parseErr == nil && seconds > 0 {
				return time.Duration(seconds) * time.Second
			}
		}
	}

	// Default for 429 without explicit retry-after: wait 60 seconds.
	if strings.Contains(errStr, "status 429") {
		return 60 * time.Second
	}

	return 0
}

// HasAPIStatus reports whether err is (or wraps) an *APIError with the given
// status code. It also recognizes the legacy formatted message string so callers
// that only have the rendered error (rather than the typed value) still match.
func HasAPIStatus(err error, status int) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == status
	}
	var rlErr *RateLimitError
	if errors.As(err, &rlErr) {
		return rlErr.StatusCode == status
	}
	prefix := fmt.Sprintf("API error (status %d", status)
	errStr := err.Error()
	return len(errStr) >= len(prefix) && errStr[:len(prefix)] == prefix
}
