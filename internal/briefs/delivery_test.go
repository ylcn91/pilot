package briefs

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestNewDeliveryService(t *testing.T) {
	config := &BriefConfig{
		Enabled:  true,
		Schedule: "0 9 * * *",
		Timezone: "UTC",
		Channels: []ChannelConfig{},
	}

	service := NewDeliveryService(config)

	if service == nil {
		t.Fatal("expected service, got nil")
	}

	if service.config != config {
		t.Error("config not set correctly")
	}

	if service.slackFmt == nil {
		t.Error("slack formatter not initialized")
	}

	if service.emailFmt == nil {
		t.Error("email formatter not initialized")
	}

	if service.plainFmt == nil {
		t.Error("plain text formatter not initialized")
	}
}

func TestDeliveryServiceWithOptions(t *testing.T) {
	config := &BriefConfig{}
	logger := slog.Default()

	mockEmail := &mockEmailSender{}

	service := NewDeliveryService(config,
		WithLogger(logger),
		WithEmailSender(mockEmail),
	)

	if service == nil {
		t.Fatal("expected service, got nil")
	}

	// Verify email sender was set
	if service.emailSender != mockEmail {
		t.Error("email sender not set by option")
	}
}

func TestDeliveryResultFields(t *testing.T) {
	result := DeliveryResult{
		Channel:   "slack:#test",
		Success:   true,
		Error:     nil,
		SentAt:    time.Now(),
		MessageID: "12345",
	}

	if result.Channel != "slack:#test" {
		t.Errorf("unexpected Channel: %s", result.Channel)
	}
	if !result.Success {
		t.Error("expected Success to be true")
	}
	if result.Error != nil {
		t.Error("expected nil Error")
	}
	if result.SentAt.IsZero() {
		t.Error("expected non-zero SentAt")
	}
	if result.MessageID != "12345" {
		t.Errorf("unexpected MessageID: %s", result.MessageID)
	}
}

// Override test to work with actual mock injection
func TestDeliveryServiceIntegrationSlack(t *testing.T) {
	// Skip if we can't mock properly
	t.Skip("Requires interface-based mocking for slack.Client")
}

func TestDeliveryServiceIntegrationEmail(t *testing.T) {
	config := &BriefConfig{
		Channels: []ChannelConfig{
			{Type: "email", Recipients: []string{"test@example.com"}},
		},
	}

	mockEmail := &mockEmailSender{}

	service := &DeliveryService{
		config:      config,
		emailSender: mockEmail,
		logger:      slog.Default(),
		slackFmt:    NewSlackFormatter(),
		emailFmt:    NewEmailFormatter(),
		plainFmt:    NewPlainTextFormatter(),
	}

	brief := createTestBrief()
	ctx := context.Background()

	results := service.DeliverAll(ctx, brief)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Verify email was formatted correctly
	if mockEmail.lastSubject == "" {
		t.Error("email subject not set")
	}

	if mockEmail.lastHtmlBody == "" {
		t.Error("email body not set")
	}

	// Check HTML contains expected elements
	expectedElements := []string{
		"<!DOCTYPE html>",
		"Pilot Daily Brief",
		"TASK-001",
		"TASK-002",
		"TASK-003",
		"TASK-004",
		"TASK-005",
	}

	for _, elem := range expectedElements {
		if !containsString(mockEmail.lastHtmlBody, elem) {
			t.Errorf("expected %q in email body", elem)
		}
	}
}

func TestWithSlackClient(t *testing.T) {
	config := &BriefConfig{}

	// WithSlackClient accepts nil without panic
	service := NewDeliveryService(config, WithSlackClient(nil))
	if service == nil {
		t.Fatal("expected service, got nil")
	}
}

func TestWithTelegramSender(t *testing.T) {
	config := &BriefConfig{}

	// WithTelegramSender accepts nil without panic
	service := NewDeliveryService(config, WithTelegramSender(nil))
	if service == nil {
		t.Fatal("expected service, got nil")
	}
}
