package alerts

import (
	"testing"
)

func TestConvertChannel(t *testing.T) {
	tests := []struct {
		name   string
		input  ChannelConfigInput
		verify func(t *testing.T, ch ChannelConfig)
	}{
		{
			name: "basic channel",
			input: ChannelConfigInput{
				Name:       "test-channel",
				Type:       "webhook",
				Enabled:    true,
				Severities: []string{"critical", "warning"},
			},
			verify: func(t *testing.T, ch ChannelConfig) {
				if ch.Name != "test-channel" {
					t.Errorf("expected name 'test-channel', got '%s'", ch.Name)
				}
				if ch.Type != "webhook" {
					t.Errorf("expected type 'webhook', got '%s'", ch.Type)
				}
				if !ch.Enabled {
					t.Error("expected enabled to be true")
				}
				if len(ch.Severities) != 2 {
					t.Errorf("expected 2 severities, got %d", len(ch.Severities))
				}
				if ch.Severities[0] != SeverityCritical {
					t.Errorf("expected first severity 'critical', got '%s'", ch.Severities[0])
				}
			},
		},
		{
			name: "slack channel",
			input: ChannelConfigInput{
				Name:    "slack-alerts",
				Type:    "slack",
				Enabled: true,
				Slack: &SlackChannelConfig{
					Channel: "#ops-alerts",
				},
			},
			verify: func(t *testing.T, ch ChannelConfig) {
				if ch.Slack == nil {
					t.Fatal("expected Slack config to be set")
				}
				if ch.Slack.Channel != "#ops-alerts" {
					t.Errorf("expected channel '#ops-alerts', got '%s'", ch.Slack.Channel)
				}
			},
		},
		{
			name: "telegram channel",
			input: ChannelConfigInput{
				Name:    "telegram-alerts",
				Type:    "telegram",
				Enabled: true,
				Telegram: &TelegramChannelConfig{
					ChatID: 123456789,
				},
			},
			verify: func(t *testing.T, ch ChannelConfig) {
				if ch.Telegram == nil {
					t.Fatal("expected Telegram config to be set")
				}
				if ch.Telegram.ChatID != 123456789 {
					t.Errorf("expected ChatID 123456789, got %d", ch.Telegram.ChatID)
				}
			},
		},
		{
			name: "email channel",
			input: ChannelConfigInput{
				Name:    "email-alerts",
				Type:    "email",
				Enabled: true,
				Email: &EmailChannelConfig{
					To:      []string{"admin@example.com", "ops@example.com"},
					Subject: "[ALERT] {{title}}",
				},
			},
			verify: func(t *testing.T, ch ChannelConfig) {
				if ch.Email == nil {
					t.Fatal("expected Email config to be set")
				}
				if len(ch.Email.To) != 2 {
					t.Errorf("expected 2 recipients, got %d", len(ch.Email.To))
				}
				if ch.Email.Subject != "[ALERT] {{title}}" {
					t.Errorf("expected subject '[ALERT] {{title}}', got '%s'", ch.Email.Subject)
				}
			},
		},
		{
			name: "webhook channel",
			input: ChannelConfigInput{
				Name:    "webhook-alerts",
				Type:    "webhook",
				Enabled: true,
				Webhook: &WebhookChannelConfig{
					URL:    "https://hooks.example.com/alert",
					Method: "POST",
					Headers: map[string]string{
						"Authorization": "Bearer token123",
					},
					Secret: "webhook-secret",
				},
			},
			verify: func(t *testing.T, ch ChannelConfig) {
				if ch.Webhook == nil {
					t.Fatal("expected Webhook config to be set")
				}
				if ch.Webhook.URL != "https://hooks.example.com/alert" {
					t.Errorf("expected URL 'https://hooks.example.com/alert', got '%s'", ch.Webhook.URL)
				}
				if ch.Webhook.Method != "POST" {
					t.Errorf("expected method 'POST', got '%s'", ch.Webhook.Method)
				}
				if ch.Webhook.Headers["Authorization"] != "Bearer token123" {
					t.Error("expected Authorization header")
				}
				if ch.Webhook.Secret != "webhook-secret" {
					t.Errorf("expected secret 'webhook-secret', got '%s'", ch.Webhook.Secret)
				}
			},
		},
		{
			name: "pagerduty channel",
			input: ChannelConfigInput{
				Name:    "pagerduty-alerts",
				Type:    "pagerduty",
				Enabled: true,
				PagerDuty: &PagerDutyChannelConfig{
					RoutingKey: "routing-key-abc",
					ServiceID:  "service-xyz",
				},
			},
			verify: func(t *testing.T, ch ChannelConfig) {
				if ch.PagerDuty == nil {
					t.Fatal("expected PagerDuty config to be set")
				}
				if ch.PagerDuty.RoutingKey != "routing-key-abc" {
					t.Errorf("expected routing key 'routing-key-abc', got '%s'", ch.PagerDuty.RoutingKey)
				}
				if ch.PagerDuty.ServiceID != "service-xyz" {
					t.Errorf("expected service ID 'service-xyz', got '%s'", ch.PagerDuty.ServiceID)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertChannel(tt.input)
			tt.verify(t, result)
		})
	}
}

func TestChannelConfigInput_NilSubConfigs(t *testing.T) {
	// Test that nil sub-configs don't cause issues
	input := ChannelConfigInput{
		Name:      "test",
		Type:      "webhook",
		Enabled:   true,
		Slack:     nil,
		Telegram:  nil,
		Email:     nil,
		Webhook:   nil,
		PagerDuty: nil,
	}

	result := convertChannel(input)

	if result.Slack != nil {
		t.Error("expected Slack to be nil")
	}
	if result.Telegram != nil {
		t.Error("expected Telegram to be nil")
	}
	if result.Email != nil {
		t.Error("expected Email to be nil")
	}
	if result.Webhook != nil {
		t.Error("expected Webhook to be nil")
	}
	if result.PagerDuty != nil {
		t.Error("expected PagerDuty to be nil")
	}
}
