package github

import (
	"context"
	"testing"
	"time"
)

// TestWithRetry_RunawayRetryAfterCappedAtMaxDelay verifies that a runaway
// Retry-After carried by a typed *RateLimitError (e.g. a hostile or
// misconfigured "Retry-After: 3600") is capped at RetryOptions.MaxDelay rather
// than stalling the worker for the full advertised duration.
//
// The existing retry_test.go RetryAfter-cap case exercises the *string-scan*
// default path (a raw "status 429" error → extractRetryAfter returns the 60s
// fallback). This case exercises the *typed-header* path: RateLimitError.RetryAfter
// is honored directly by extractRetryAfter, so a 3600s value must be clamped to
// MaxDelay before the backoff sleep.
func TestWithRetry_RunawayRetryAfterCappedAtMaxDelay(t *testing.T) {
	const maxDelay = 30 * time.Millisecond // stand-in for the real 30s cap, scaled for a hermetic test

	calls := 0
	start := time.Now()
	result, err := WithRetry(context.Background(), func() (string, error) {
		calls++
		if calls == 1 {
			// Runaway advertised delay: an hour.
			return "", &RateLimitError{
				StatusCode: 429,
				RetryAfter: 3600 * time.Second,
				Message:    "secondary rate limit",
			}
		}
		return "recovered", nil
	}, RetryOptions{
		MaxRetries: 3,
		BaseDelay:  1 * time.Millisecond,
		MaxDelay:   maxDelay,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("WithRetry returned error = %v, want nil after retry", err)
	}
	if result != "recovered" {
		t.Errorf("result = %q, want %q", result, "recovered")
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (initial failure + one retry)", calls)
	}

	// With the cap the single backoff sleep is <= maxDelay. Without the cap the
	// loop would wait ~3600s. Allow generous slack for scheduler jitter while
	// still being orders of magnitude below the uncapped delay.
	if elapsed > 2*time.Second {
		t.Errorf("Retry-After was not capped at MaxDelay: elapsed %v (expected well under 2s, uncapped would be ~1h)", elapsed)
	}
}

// TestWithRetry_RunawayRetryAfter_CapAcrossMultipleRetries confirms the cap
// holds for every attempt, not just the first: three consecutive runaway
// RateLimitErrors must each be clamped to MaxDelay, so total elapsed stays
// bounded by roughly MaxRetries * MaxDelay rather than 3 * 3600s.
func TestWithRetry_RunawayRetryAfter_CapAcrossMultipleRetries(t *testing.T) {
	const maxDelay = 20 * time.Millisecond

	calls := 0
	start := time.Now()
	_, err := WithRetry(context.Background(), func() (string, error) {
		calls++
		return "", &RateLimitError{
			StatusCode: 429,
			RetryAfter: 3600 * time.Second,
			Message:    "rate limited",
		}
	}, RetryOptions{
		MaxRetries: 3,
		BaseDelay:  1 * time.Millisecond,
		MaxDelay:   maxDelay,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error after exhausting retries on persistent rate limit")
	}
	// Initial attempt + 3 retries = 4 calls.
	if calls != 4 {
		t.Errorf("calls = %d, want 4 (1 + 3 retries)", calls)
	}
	// 3 capped sleeps of ~20ms each; bounded far below the uncapped 3*3600s.
	if elapsed > 2*time.Second {
		t.Errorf("capped backoff exceeded bound: elapsed %v (expected well under 2s)", elapsed)
	}
}
