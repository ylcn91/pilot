package github

import (
	"context"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/httpretry"
)

// The retry/backoff primitives live in the shared internal/adapters/httpretry
// package so every adapter (gitlab, jira, linear, azuredevops) handles 429 +
// transient 5xx identically. github keeps these aliases for its existing
// callers and tests; behavior is unchanged.

// RetryOptions configures retry behavior.
type RetryOptions = httpretry.RetryOptions

// DefaultRetryOptions returns sensible defaults for retry behavior.
func DefaultRetryOptions() RetryOptions { return httpretry.DefaultRetryOptions() }

// WithRetry executes an operation with exponential backoff retry.
func WithRetry[T any](ctx context.Context, op func() (T, error), opts RetryOptions) (T, error) {
	return httpretry.WithRetry(ctx, op, opts)
}

// WithRetryVoid is like WithRetry but for operations that don't return a value.
func WithRetryVoid(ctx context.Context, op func() error, opts RetryOptions) error {
	return httpretry.WithRetryVoid(ctx, op, opts)
}

// isRetryableError determines if an error is transient and should be retried.
func isRetryableError(err error) bool { return httpretry.IsRetryableError(err) }

// extractRetryAfter extracts the Retry-After duration from a rate limit error.
func extractRetryAfter(err error) time.Duration { return httpretry.ExtractRetryAfter(err) }
