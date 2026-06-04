package azuredevops

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/httpretry"
)

const (
	defaultBaseURL    = "https://dev.azure.com"
	apiVersion        = "7.1"
	apiVersionPreview = "7.1-preview"
)

// Client is an Azure DevOps API client
type Client struct {
	pat          string
	organization string
	project      string
	repository   string
	httpClient   *http.Client
	baseURL      string
	retryOpts    httpretry.RetryOptions // Retry config for doRequest; disabled in tests
}

// NewClient creates a new Azure DevOps client
func NewClient(pat, organization, project string) *Client {
	return &Client{
		pat:          pat,
		organization: organization,
		project:      project,
		repository:   project, // Default repository name to project name
		baseURL:      defaultBaseURL,
		retryOpts:    httpretry.DefaultRetryOptions(),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewClientWithConfig creates a new Azure DevOps client from config
func NewClientWithConfig(config *Config) *Client {
	c := NewClient(config.PAT, config.Organization, config.Project)
	if config.Repository != "" {
		c.repository = config.Repository
	}
	if config.BaseURL != "" {
		c.baseURL = config.BaseURL
	}
	return c
}

// NewClientWithBaseURL creates a new Azure DevOps client with a custom base URL (for testing).
// Retry is disabled by default so unit tests fail fast; set client.retryOpts to enable.
func NewClientWithBaseURL(pat, organization, project, baseURL string) *Client {
	return &Client{
		pat:          pat,
		organization: organization,
		project:      project,
		repository:   project,
		baseURL:      baseURL,
		retryOpts:    httpretry.RetryOptions{MaxRetries: 0},
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetRepository sets the repository name (if different from project)
func (c *Client) SetRepository(repo string) {
	c.repository = repo
}

// doRequest performs an HTTP request to the Azure DevOps API with automatic
// retry on transient errors (429 + Retry-After, 5xx, network failures). The
// request body is buffered once before the retry loop so it can be replayed.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
	}

	fullURL := c.baseURL + path

	return httpretry.WithRetryVoid(ctx, func() error {
		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		// Azure DevOps uses Basic auth with empty username and PAT as password
		auth := base64.StdEncoding.EncodeToString([]byte(":" + c.pat))
		req.Header.Set("Authorization", "Basic "+auth)
		req.Header.Set("Accept", "application/json")
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

// doRequestWithPatch performs a PATCH request with JSON Patch content type,
// with the same transient-error retry semantics as doRequest.
func (c *Client) doRequestWithPatch(ctx context.Context, path string, body interface{}, result interface{}) error {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
	}

	fullURL := c.baseURL + path

	return httpretry.WithRetryVoid(ctx, func() error {
		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPatch, fullURL, bodyReader)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		auth := base64.StdEncoding.EncodeToString([]byte(":" + c.pat))
		req.Header.Set("Authorization", "Basic "+auth)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json-patch+json")

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
