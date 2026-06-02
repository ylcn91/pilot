package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	telegramAPIURL = "https://api.telegram.org/bot"
)

// Client is a Telegram Bot API client
type Client struct {
	botToken   string
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Telegram client
func NewClient(botToken string) *Client {
	return &Client{
		botToken: botToken,
		baseURL:  telegramAPIURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // Must be > long polling timeout (30s)
		},
	}
}

// NewClientWithBaseURL creates a new Telegram client with a custom base URL (for testing)
func NewClientWithBaseURL(botToken, baseURL string) *Client {
	return &Client{
		botToken: botToken,
		baseURL:  baseURL + "/bot",
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// CheckSingleton verifies no other bot instance is running by making a quick API call.
// Returns ErrConflict if another instance is detected (409 error).
func (c *Client) CheckSingleton(ctx context.Context) error {
	// Use timeout=0 for immediate response (no long polling)
	url := fmt.Sprintf("%s%s/getUpdates?timeout=0&limit=1", c.baseURL, c.botToken)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to check singleton: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var result GetUpdatesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !result.OK {
		// 409 = Conflict: another getUpdates is running
		if result.ErrorCode == 409 {
			return ErrConflict
		}
		return fmt.Errorf("telegram API error: %s (code: %d)", result.Description, result.ErrorCode)
	}

	return nil
}

// GetMe returns the bot's user info from the Telegram API.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	url := fmt.Sprintf("%s%s/getMe", c.baseURL, c.botToken)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call getMe: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result GetMeResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !result.OK {
		return nil, fmt.Errorf("telegram API error: %s (code: %d)", result.Description, result.ErrorCode)
	}

	return result.Result, nil
}

// GetUpdates retrieves updates using long polling
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout int) ([]*Update, error) {
	url := fmt.Sprintf("%s%s/getUpdates?offset=%d&timeout=%d", c.baseURL, c.botToken, offset, timeout)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get updates: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result GetUpdatesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !result.OK {
		return nil, fmt.Errorf("telegram API error: %s (code: %d)", result.Description, result.ErrorCode)
	}

	return result.Result, nil
}
