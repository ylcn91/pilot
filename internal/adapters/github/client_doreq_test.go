package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestDoRequest_ErrorHandling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			response:   `{"id": 1}`,
			wantErr:    false,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			response:   `{"message": "Not Found"}`,
			wantErr:    true,
			errMsg:     "API error (status 404)",
		},
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			response:   `{"message": "Bad credentials"}`,
			wantErr:    true,
			errMsg:     "API error (status 401)",
		},
		{
			name:       "rate limited",
			statusCode: http.StatusForbidden,
			response:   `{"message": "API rate limit exceeded"}`,
			wantErr:    true,
			errMsg:     "API error (status 403)",
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			response:   `{"message": "Internal server error"}`,
			wantErr:    true,
			errMsg:     "API error (status 500)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			_, err := client.GetIssue(context.Background(), "owner", "repo", 1)

			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("error = %v, want to contain %s", err, tt.errMsg)
			}
		})
	}
}

func TestDoRequest_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	_, err := client.GetIssue(context.Background(), "owner", "repo", 1)

	if err == nil {
		t.Error("expected error for invalid JSON response")
	}
	if !strings.Contains(err.Error(), "failed to parse response") {
		t.Errorf("error = %v, want to contain 'failed to parse response'", err)
	}
}

func TestDoRequest_RetryOn429(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(Issue{Number: 1})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	// Use fast retry so the test doesn't sleep for 1s
	client.retryOpts = RetryOptions{MaxRetries: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}

	var issue Issue
	err := client.doRequest(context.Background(), "GET", "/repos/owner/repo/issues/1", nil, &issue)
	if err != nil {
		t.Fatalf("doRequest() error = %v, want nil", err)
	}
	if calls != 2 {
		t.Errorf("server called %d times, want 2 (1 initial + 1 retry)", calls)
	}
	if issue.Number != 1 {
		t.Errorf("issue.Number = %d, want 1", issue.Number)
	}
}

// TestDoRequest_RateLimitError verifies that doRequest returns *RateLimitError for rate-limit
// 403/429 responses and a plain error for non-rate-limit 403 responses.
func TestDoRequest_RateLimitError(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		body           string
		headers        map[string]string
		wantRateLimit  bool
		wantRetryAfter time.Duration
	}{
		{
			name:          "429 always rate limit",
			statusCode:    http.StatusTooManyRequests,
			body:          `{"message": "too many requests"}`,
			wantRateLimit: true,
		},
		{
			name:           "429 with Retry-After header",
			statusCode:     http.StatusTooManyRequests,
			body:           `{"message": "too many requests"}`,
			headers:        map[string]string{"Retry-After": "30"},
			wantRateLimit:  true,
			wantRetryAfter: 30 * time.Second,
		},
		{
			name:          "403 secondary rate limit body",
			statusCode:    http.StatusForbidden,
			body:          `{"message": "You have exceeded a secondary rate limit"}`,
			wantRateLimit: true,
		},
		{
			name:           "403 secondary rate limit with Retry-After",
			statusCode:     http.StatusForbidden,
			body:           `{"message": "You have exceeded a secondary rate limit"}`,
			headers:        map[string]string{"Retry-After": "60"},
			wantRateLimit:  true,
			wantRetryAfter: 60 * time.Second,
		},
		{
			name:          "403 rate limit exceeded body",
			statusCode:    http.StatusForbidden,
			body:          `{"message": "API rate limit exceeded"}`,
			wantRateLimit: true,
		},
		{
			name:          "403 X-RateLimit-Remaining zero",
			statusCode:    http.StatusForbidden,
			body:          `{"message": "Forbidden"}`,
			headers:       map[string]string{"X-RateLimit-Remaining": "0"},
			wantRateLimit: true,
		},
		{
			name:          "403 plain forbidden - not a rate limit",
			statusCode:    http.StatusForbidden,
			body:          `{"message": "Resource not accessible by integration"}`,
			wantRateLimit: false,
		},
		{
			name:          "403 permission denied - not a rate limit",
			statusCode:    http.StatusForbidden,
			body:          `{"message": "Must have push access to the repository"}`,
			wantRateLimit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tt.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			err := client.doRequest(context.Background(), "GET", "/repos/owner/repo/issues/1", nil, nil)

			if err == nil {
				t.Fatal("expected error, got nil")
			}

			var rlErr *RateLimitError
			gotRateLimit := errors.As(err, &rlErr)

			if gotRateLimit != tt.wantRateLimit {
				t.Errorf("errors.As(*RateLimitError) = %v, want %v (err: %v)", gotRateLimit, tt.wantRateLimit, err)
				return
			}

			if tt.wantRateLimit && tt.wantRetryAfter > 0 {
				if rlErr.RetryAfter != tt.wantRetryAfter {
					t.Errorf("RateLimitError.RetryAfter = %v, want %v", rlErr.RetryAfter, tt.wantRetryAfter)
				}
			}

			// Error string must always contain the status code for callers checking string.
			if !strings.Contains(err.Error(), fmt.Sprintf("status %d", tt.statusCode)) {
				t.Errorf("error %q does not contain expected status", err.Error())
			}
		})
	}
}

// TestDoRequest_Secondary403Retried verifies that a 403 secondary rate-limit response
// is retried, while a plain 403 (permission error) is not.
func TestDoRequest_Secondary403Retried(t *testing.T) {
	t.Run("secondary rate limit 403 is retried", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls == 1 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message": "You have exceeded a secondary rate limit"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(Issue{Number: 1})
		}))
		defer server.Close()

		client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
		client.retryOpts = RetryOptions{MaxRetries: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}

		var issue Issue
		err := client.doRequest(context.Background(), "GET", "/repos/owner/repo/issues/1", nil, &issue)
		if err != nil {
			t.Fatalf("expected success after retry, got: %v", err)
		}
		if calls != 2 {
			t.Errorf("server called %d times, want 2 (1 rate-limited + 1 retry)", calls)
		}
	})

	t.Run("plain 403 forbidden is not retried", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message": "Resource not accessible by integration"}`))
		}))
		defer server.Close()

		client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
		client.retryOpts = RetryOptions{MaxRetries: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}

		err := client.doRequest(context.Background(), "GET", "/repos/owner/repo/issues/1", nil, nil)
		if err == nil {
			t.Fatal("expected error for plain 403, got nil")
		}
		if calls != 1 {
			t.Errorf("server called %d times, want 1 (no retry on plain 403)", calls)
		}
	})
}

// TestDoRequest_NoRetryOn404 verifies that doRequest does not retry non-retriable errors.
func TestDoRequest_NoRetryOn404(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	client.retryOpts = RetryOptions{MaxRetries: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}

	err := client.doRequest(context.Background(), "GET", "/repos/owner/repo/issues/999", nil, nil)
	if err == nil {
		t.Fatal("doRequest() expected error for 404, got nil")
	}
	if calls != 1 {
		t.Errorf("server called %d times, want 1 (no retry on 404)", calls)
	}
}

// TestDoRequest_ReplayBodyOnRetry verifies that POST body is replayed correctly on retry.
func TestDoRequest_ReplayBodyOnRetry(t *testing.T) {
	calls := 0
	var receivedBodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		buf := new(strings.Builder)
		_, _ = io.Copy(buf, r.Body)
		receivedBodies = append(receivedBodies, buf.String())
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Comment{ID: 42, Body: "hello"})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	client.retryOpts = RetryOptions{MaxRetries: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}

	var comment Comment
	err := client.doRequest(context.Background(), "POST", "/repos/owner/repo/issues/1/comments",
		map[string]string{"body": "hello"}, &comment)
	if err != nil {
		t.Fatalf("doRequest() error = %v", err)
	}
	if calls != 2 {
		t.Errorf("server called %d times, want 2", calls)
	}
	// Both attempts should have sent the same body
	for i, body := range receivedBodies {
		if !strings.Contains(body, "hello") {
			t.Errorf("attempt %d body %q does not contain 'hello'", i+1, body)
		}
	}
}
