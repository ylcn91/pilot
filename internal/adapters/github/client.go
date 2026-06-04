package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/httpretry"
)

const (
	githubAPIURL     = "https://api.github.com"
	githubGraphQLURL = "https://api.github.com/graphql"
)

// RateLimitError is returned by doRequest when GitHub signals a rate limit via
// a 403 or 429 response. It carries the parsed Retry-After duration so the
// retry loop can honor it without regexing the error string. It aliases the
// shared httpretry type so errors.As matches across adapter boundaries.
type RateLimitError = httpretry.RateLimitError

// APIError is returned by doRequest for non-2xx GitHub responses that are not
// rate limits. Carrying the status code lets callers branch with errors.As
// instead of matching the formatted message string.
type APIError = httpretry.APIError

// parseRetryAfterHeader reads Retry-After and X-RateLimit-Reset headers and
// returns the delay duration. Returns 0 when neither header is present.
func parseRetryAfterHeader(h http.Header) time.Duration {
	return httpretry.ParseRetryAfterHeader(h)
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
			return httpretry.ClassifyResponse(resp, respBody)
		}

		if result != nil && len(respBody) > 0 {
			if err := json.Unmarshal(respBody, result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
		}

		return nil
	}, c.retryOpts)
}

// hasAPIStatus reports whether err is (or wraps) an *APIError with the given
// status code. It also recognizes the legacy formatted message string so callers
// that only have the rendered error (rather than the typed value) still match.
func hasAPIStatus(err error, status int) bool {
	return httpretry.HasAPIStatus(err, status)
}

// isNotFoundError checks if error is a 404 not found error
func isNotFoundError(err error) bool {
	return hasAPIStatus(err, http.StatusNotFound)
}

// isUnprocessableError checks if error is a 422 unprocessable entity error
func isUnprocessableError(err error) bool {
	return hasAPIStatus(err, http.StatusUnprocessableEntity)
}
