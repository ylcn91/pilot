package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/httpretry"
	"github.com/ylcn91/pilot/internal/executor"
)

const (
	linearAPIURL = "https://api.linear.app/graphql"
)

// Compile-time check: *Client implements executor.SubIssueCreator (GH-1472)
var _ executor.SubIssueCreator = (*Client)(nil)

// Client is a Linear API client
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	retryOpts  httpretry.RetryOptions // Retry config for Execute; disabled in tests

	doneStateMu    sync.RWMutex
	doneStateCache map[string]string
}

// NewClient creates a new Linear client
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:    apiKey,
		baseURL:   linearAPIURL,
		retryOpts: httpretry.DefaultRetryOptions(),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		doneStateCache: make(map[string]string),
	}
}

// NewClientWithBaseURL creates a new Linear client with a custom base URL (for testing).
// Retry is disabled by default so unit tests fail fast; set client.retryOpts to enable.
func NewClientWithBaseURL(apiKey, baseURL string) *Client {
	return &Client{
		apiKey:    apiKey,
		baseURL:   baseURL,
		retryOpts: httpretry.RetryOptions{MaxRetries: 0},
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		doneStateCache: make(map[string]string),
	}
}

// GraphQLRequest represents a GraphQL request
type GraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

// GraphQLResponse represents a GraphQL response
type GraphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []GraphQLError  `json:"errors,omitempty"`
}

// GraphQLError represents a GraphQL error
type GraphQLError struct {
	Message string `json:"message"`
}

// Execute executes a GraphQL query with automatic retry on transient errors
// (429 + Retry-After, 5xx, network failures, and GraphQL-level RATE_LIMITED
// responses). The request body is marshalled once before the retry loop so it
// can be replayed on each attempt.
func (c *Client) Execute(ctx context.Context, query string, variables map[string]interface{}, result interface{}) error {
	reqBody := GraphQLRequest{
		Query:     query,
		Variables: variables,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	return httpretry.WithRetryVoid(ctx, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", c.apiKey)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to execute request: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			// ClassifyResponse yields a typed *RateLimitError / *APIError so the
			// retry loop honors 429 + Retry-After and retries transient 5xx.
			return httpretry.ClassifyResponse(resp, respBody)
		}

		var gqlResp GraphQLResponse
		if err := json.Unmarshal(respBody, &gqlResp); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if len(gqlResp.Errors) > 0 {
			// GraphQL-level RATE_LIMITED errors arrive as HTTP 200; the message
			// triggers IsRetryableError so the loop backs off and retries.
			return fmt.Errorf("GraphQL error: %s", gqlResp.Errors[0].Message)
		}

		if result != nil {
			if err := json.Unmarshal(gqlResp.Data, result); err != nil {
				return fmt.Errorf("failed to parse data: %w", err)
			}
		}

		return nil
	}, c.retryOpts)
}
