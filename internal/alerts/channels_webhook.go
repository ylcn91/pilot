package alerts

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// WebhookChannel sends alerts to a webhook endpoint
type WebhookChannel struct {
	name    string
	url     string
	method  string
	headers map[string]string
	secret  string
	client  *http.Client
}

// NewWebhookChannel creates a new webhook alert channel
func NewWebhookChannel(name string, config *WebhookChannelConfig) *WebhookChannel {
	method := config.Method
	if method == "" {
		method = http.MethodPost
	}

	return &WebhookChannel{
		name:    name,
		url:     config.URL,
		method:  method,
		headers: config.Headers,
		secret:  config.Secret,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *WebhookChannel) Name() string { return c.name }
func (c *WebhookChannel) Type() string { return "webhook" }

func (c *WebhookChannel) Send(ctx context.Context, alert *Alert) error {
	payload, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("failed to marshal alert: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, c.method, c.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add custom headers
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	// Add HMAC signature if secret is configured
	if c.secret != "" {
		signature := c.sign(payload)
		req.Header.Set("X-Signature-256", "sha256="+signature)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

func (c *WebhookChannel) sign(payload []byte) string {
	h := hmac.New(sha256.New, []byte(c.secret))
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}
