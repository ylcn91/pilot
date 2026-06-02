package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	githubAPIURL     = "https://api.github.com"
	githubGraphQLURL = "https://api.github.com/graphql"
)

// RateLimitError is returned by doRequest when GitHub signals a rate limit via
// a 403 or 429 response. It carries the parsed Retry-After duration so the
// retry loop can honor it without regexing the error string.
type RateLimitError struct {
	StatusCode int
	RetryAfter time.Duration
	Message    string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Message)
}

// parseRetryAfterHeader reads Retry-After and X-RateLimit-Reset headers and
// returns the delay duration. Returns 0 when neither header is present.
func parseRetryAfterHeader(h http.Header) time.Duration {
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

// Client is a GitHub API client
type Client struct {
	token                string
	httpClient           *http.Client
	baseURL              string       // For testing - defaults to githubAPIURL
	retryOpts            RetryOptions // Retry config for doRequest; overridable in tests
	issueCreationEnabled bool
}

// NewClient creates a new GitHub client
func NewClient(token string) *Client {
	return &Client{
		token:     token,
		baseURL:   githubAPIURL,
		retryOpts: DefaultRetryOptions(),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetIssueCreationEnabled controls whether this client may create GitHub issues.
func (c *Client) SetIssueCreationEnabled(enabled bool) {
	if c != nil {
		c.issueCreationEnabled = enabled
	}
}

// IssueCreationEnabled reports whether this client may create GitHub issues.
func (c *Client) IssueCreationEnabled() bool {
	return c != nil && c.issueCreationEnabled
}

// NewClientWithBaseURL creates a new GitHub client with a custom base URL (for testing).
// Retry is disabled by default so unit tests fail fast; set client.retryOpts to enable.
func NewClientWithBaseURL(token, baseURL string) *Client {
	return &Client{
		token:     token,
		baseURL:   baseURL,
		retryOpts: RetryOptions{MaxRetries: 0},
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// doRequest performs an HTTP request to the GitHub API with automatic retry on
// transient errors (429, 5xx, network failures). The request body is buffered
// once before the retry loop so it can be replayed on each attempt.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
	}

	return WithRetryVoid(ctx, func() error {
		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to execute request: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			msg := string(respBody)
			if resp.StatusCode == http.StatusTooManyRequests {
				return &RateLimitError{
					StatusCode: http.StatusTooManyRequests,
					RetryAfter: parseRetryAfterHeader(resp.Header),
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
						RetryAfter: parseRetryAfterHeader(resp.Header),
						Message:    msg,
					}
				}
			}
			return fmt.Errorf("API error (status %d): %s", resp.StatusCode, msg)
		}

		if result != nil && len(respBody) > 0 {
			if err := json.Unmarshal(respBody, result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
		}

		return nil
	}, c.retryOpts)
}

// isNotFoundError checks if error is a 404 not found error
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return len(errStr) >= 21 && errStr[:21] == "API error (status 404"
}

// isUnprocessableError checks if error is a 422 unprocessable entity error
func isUnprocessableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return len(errStr) >= 21 && errStr[:21] == "API error (status 422"
}
