package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// PagerDutyChannel sends alerts to PagerDuty
type PagerDutyChannel struct {
	name       string
	routingKey string
	serviceID  string
	client     *http.Client
	baseURL    string // Override for testing; defaults to PagerDuty Events API
}

const pagerDutyEventsAPI = "https://events.pagerduty.com/v2/enqueue"

// NewPagerDutyChannel creates a new PagerDuty alert channel
func NewPagerDutyChannel(name string, config *PagerDutyChannelConfig) *PagerDutyChannel {
	return &PagerDutyChannel{
		name:       name,
		routingKey: config.RoutingKey,
		serviceID:  config.ServiceID,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *PagerDutyChannel) Name() string { return c.name }
func (c *PagerDutyChannel) Type() string { return "pagerduty" }

func (c *PagerDutyChannel) Send(ctx context.Context, alert *Alert) error {
	severity := "warning"
	switch alert.Severity {
	case SeverityCritical:
		severity = "critical"
	case SeverityWarning:
		severity = "warning"
	case SeverityInfo:
		severity = "info"
	}

	event := map[string]interface{}{
		"routing_key":  c.routingKey,
		"event_action": "trigger",
		"dedup_key":    fmt.Sprintf("pilot-%s-%s", alert.Type, alert.Source),
		"payload": map[string]interface{}{
			"summary":        fmt.Sprintf("%s: %s", alert.Title, alert.Message),
			"source":         alert.Source,
			"severity":       severity,
			"timestamp":      alert.CreatedAt.Format(time.RFC3339),
			"component":      "pilot",
			"group":          alert.ProjectPath,
			"class":          string(alert.Type),
			"custom_details": alert.Metadata,
		},
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal PagerDuty event: %w", err)
	}

	apiURL := pagerDutyEventsAPI
	if c.baseURL != "" {
		apiURL = c.baseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create PagerDuty request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("PagerDuty request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("PagerDuty returned status %d", resp.StatusCode)
	}

	return nil
}
