package jira

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/httpretry"
)

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// scriptedStatus describes one scripted HTTP response.
type scriptedStatus struct {
	code    int
	body    string
	headers map[string]string
}

func (s scriptedStatus) response() *http.Response {
	h := http.Header{}
	for k, v := range s.headers {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: s.code,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(s.body)),
	}
}

func TestClient_doRequest_RetriesTransient(t *testing.T) {
	tests := []struct {
		name         string
		maxRetries   int
		script       []scriptedStatus
		wantErr      bool
		wantAttempts int32
	}{
		{
			name:       "429 then success",
			maxRetries: 3,
			script: []scriptedStatus{
				{code: http.StatusTooManyRequests, body: `{"errorMessages":["slow down"]}`, headers: map[string]string{"Retry-After": "0"}},
				{code: http.StatusOK, body: `{"key":"PROJ-1"}`},
			},
			wantErr:      false,
			wantAttempts: 2,
		},
		{
			name:       "503 then success",
			maxRetries: 3,
			script: []scriptedStatus{
				{code: http.StatusServiceUnavailable, body: `{"errorMessages":["unavailable"]}`},
				{code: http.StatusOK, body: `{"key":"PROJ-1"}`},
			},
			wantErr:      false,
			wantAttempts: 2,
		},
		{
			name:       "404 not retried",
			maxRetries: 3,
			script: []scriptedStatus{
				{code: http.StatusNotFound, body: `{"errorMessages":["missing"]}`},
			},
			wantErr:      true,
			wantAttempts: 1,
		},
		{
			name:       "persistent 500 exhausts retries",
			maxRetries: 2,
			script: []scriptedStatus{
				{code: http.StatusInternalServerError, body: `{"errorMessages":["boom"]}`},
			},
			wantErr:      true,
			wantAttempts: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attempts int32
			idx := 0
			client := NewClient("https://jira.example.com", "user", "token", PlatformCloud)
			// Fast retries so the test doesn't sleep on real backoff.
			client.retryOpts = httpretry.RetryOptions{MaxRetries: tt.maxRetries, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}
			client.httpClient = &http.Client{
				Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					atomic.AddInt32(&attempts, 1)
					s := tt.script[idx]
					if idx < len(tt.script)-1 {
						idx++
					}
					return s.response(), nil
				}),
			}

			_, err := client.GetIssue(context.Background(), "PROJ-1")
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := atomic.LoadInt32(&attempts); got != tt.wantAttempts {
				t.Errorf("attempts = %d, want %d", got, tt.wantAttempts)
			}
		})
	}
}
