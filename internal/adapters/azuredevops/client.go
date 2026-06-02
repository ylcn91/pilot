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
}

// NewClient creates a new Azure DevOps client
func NewClient(pat, organization, project string) *Client {
	return &Client{
		pat:          pat,
		organization: organization,
		project:      project,
		repository:   project, // Default repository name to project name
		baseURL:      defaultBaseURL,
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

// NewClientWithBaseURL creates a new Azure DevOps client with a custom base URL (for testing)
func NewClientWithBaseURL(pat, organization, project, baseURL string) *Client {
	return &Client{
		pat:          pat,
		organization: organization,
		project:      project,
		repository:   project,
		baseURL:      baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetRepository sets the repository name (if different from project)
func (c *Client) SetRepository(repo string) {
	c.repository = repo
}

// doRequest performs an HTTP request to the Azure DevOps API
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	fullURL := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Azure DevOps uses Basic auth with empty username and PAT as password
	auth := base64.StdEncoding.EncodeToString([]byte(":" + c.pat))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Accept", "application/json")
	if body != nil {
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
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
	}

	return nil
}

// doRequestWithPatch performs a PATCH request with JSON Patch content type
func (c *Client) doRequestWithPatch(ctx context.Context, path string, body interface{}, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	fullURL := c.baseURL + path
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
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
	}

	return nil
}
