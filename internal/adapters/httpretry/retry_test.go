package httpretry

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"nil", nil, false},
		{"403 secondary rate limit via RateLimitError", &RateLimitError{StatusCode: 403, Message: "secondary rate limit"}, true},
		{"429 via RateLimitError", &RateLimitError{StatusCode: 429, Message: "rate limited"}, true},
		{"500 via APIError", &APIError{StatusCode: 500, Message: "boom"}, true},
		{"502 string", errors.New("API error (status 502): bad gateway"), true},
		{"503 string", errors.New("API error (status 503): unavailable"), true},
		{"504 string", errors.New("API error (status 504): timeout"), true},
		{"400 not retryable", &APIError{StatusCode: 400, Message: "bad"}, false},
		{"401 not retryable", &APIError{StatusCode: 401, Message: "unauth"}, false},
		{"404 not retryable", &APIError{StatusCode: 404, Message: "missing"}, false},
		{"422 not retryable", &APIError{StatusCode: 422, Message: "unprocessable"}, false},
		{"graphql rate limited", errors.New("graphql error: RATE_LIMITED"), true},
		{"submitted too quickly", errors.New("was submitted too quickly"), true},
		{"connection refused", errors.New("dial tcp: connection refused"), true},
		{"i/o timeout", errors.New("read: i/o timeout"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryableError(tt.err); got != tt.retryable {
				t.Errorf("IsRetryableError(%v) = %v, want %v", tt.err, got, tt.retryable)
			}
		})
	}
}

func TestExtractRetryAfter(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected time.Duration
	}{
		{"nil", nil, 0},
		{"RateLimitError with RetryAfter", &RateLimitError{StatusCode: 403, RetryAfter: 30 * time.Second}, 30 * time.Second},
		{"RateLimitError no RetryAfter defaults to 60s", &RateLimitError{StatusCode: 403}, 60 * time.Second},
		{"plain 429 string defaults to 60s", errors.New("API error (status 429): too many"), 60 * time.Second},
		{"non-rate-limit error", &APIError{StatusCode: 500}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractRetryAfter(tt.err); got != tt.expected {
				t.Errorf("ExtractRetryAfter(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestClassifyResponse(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		headers     map[string]string
		body        string
		wantRate    bool
		wantRetryAt time.Duration
	}{
		{
			name:        "429 with Retry-After",
			status:      http.StatusTooManyRequests,
			headers:     map[string]string{"Retry-After": "12"},
			body:        "slow down",
			wantRate:    true,
			wantRetryAt: 12 * time.Second,
		},
		{
			name:     "403 secondary rate limit",
			status:   http.StatusForbidden,
			headers:  map[string]string{},
			body:     "You have exceeded a secondary rate limit",
			wantRate: true,
		},
		{
			name:     "403 with X-RateLimit-Remaining 0",
			status:   http.StatusForbidden,
			headers:  map[string]string{"X-RateLimit-Remaining": "0"},
			body:     "forbidden",
			wantRate: true,
		},
		{
			name:     "plain 403 not a rate limit",
			status:   http.StatusForbidden,
			headers:  map[string]string{},
			body:     "forbidden",
			wantRate: false,
		},
		{
			name:     "404 is plain APIError",
			status:   http.StatusNotFound,
			headers:  map[string]string{},
			body:     "missing",
			wantRate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tt.status, Header: http.Header{}}
			for k, v := range tt.headers {
				resp.Header.Set(k, v)
			}
			err := ClassifyResponse(resp, []byte(tt.body))

			var rlErr *RateLimitError
			isRate := errors.As(err, &rlErr)
			if isRate != tt.wantRate {
				t.Fatalf("ClassifyResponse rate-limit = %v, want %v (err=%v)", isRate, tt.wantRate, err)
			}
			if tt.wantRate && tt.wantRetryAt != 0 && rlErr.RetryAfter != tt.wantRetryAt {
				t.Errorf("RetryAfter = %v, want %v", rlErr.RetryAfter, tt.wantRetryAt)
			}
			if !tt.wantRate {
				var apiErr *APIError
				if !errors.As(err, &apiErr) {
					t.Fatalf("expected *APIError, got %T (%v)", err, err)
				}
				if apiErr.StatusCode != tt.status {
					t.Errorf("APIError.StatusCode = %d, want %d", apiErr.StatusCode, tt.status)
				}
			}
		})
	}
}

func TestWithRetry_SuccessAfterRetries(t *testing.T) {
	attempts := 0
	result, err := WithRetry(context.Background(), func() (string, error) {
		attempts++
		if attempts < 3 {
			return "", &APIError{StatusCode: 503, Message: "unavailable"}
		}
		return "ok", nil
	}, RetryOptions{MaxRetries: 5, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond})

	if err != nil {
		t.Fatalf("WithRetry err = %v, want nil", err)
	}
	if result != "ok" {
		t.Errorf("result = %q, want ok", result)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestWithRetry_NonRetryableStopsImmediately(t *testing.T) {
	attempts := 0
	_, err := WithRetry(context.Background(), func() (string, error) {
		attempts++
		return "", &APIError{StatusCode: 400, Message: "bad"}
	}, RetryOptions{MaxRetries: 5, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond})

	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (non-retryable)", attempts)
	}
}

func TestWithRetry_HonorsRetryAfterCappedAtMaxDelay(t *testing.T) {
	attempts := 0
	start := time.Now()
	_, err := WithRetry(context.Background(), func() (string, error) {
		attempts++
		if attempts == 1 {
			return "", &RateLimitError{StatusCode: 429, RetryAfter: time.Hour}
		}
		return "ok", nil
	}, RetryOptions{MaxRetries: 2, BaseDelay: time.Millisecond, MaxDelay: 20 * time.Millisecond})

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("elapsed = %v, runaway Retry-After was not capped at MaxDelay", elapsed)
	}
}

func TestHasAPIStatus(t *testing.T) {
	if !HasAPIStatus(&APIError{StatusCode: 404}, 404) {
		t.Error("expected match for typed APIError 404")
	}
	if !HasAPIStatus(&RateLimitError{StatusCode: 429}, 429) {
		t.Error("expected match for typed RateLimitError 429")
	}
	if !HasAPIStatus(errors.New("API error (status 422): bad"), 422) {
		t.Error("expected match for legacy string 422")
	}
	if HasAPIStatus(errors.New("API error (status 500): boom"), 404) {
		t.Error("unexpected match for mismatched status")
	}
	if HasAPIStatus(nil, 404) {
		t.Error("nil error should not match")
	}
}
