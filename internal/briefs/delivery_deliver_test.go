package briefs

import (
	"context"
	"log/slog"
	"testing"
)

func TestDeliverAllNoChannels(t *testing.T) {
	config := &BriefConfig{
		Channels: []ChannelConfig{},
	}

	service := NewDeliveryService(config)
	brief := createTestBrief()
	ctx := context.Background()

	results := service.DeliverAll(ctx, brief)

	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestDeliverAllSlackSuccess(t *testing.T) {
	// Skip: Requires interface-based mocking for slack.Client
	// This test documents expected behavior but cannot run without a mock
	t.Skip("Requires interface-based mocking for slack.Client")
}

func TestDeliverAllSlackFailure(t *testing.T) {
	config := &BriefConfig{
		Channels: []ChannelConfig{
			{Type: "slack", Channel: "#test-channel"},
		},
	}

	mockSlack := &mockSlackClient{shouldFail: true}

	service := &DeliveryService{
		config:      config,
		slackClient: createSlackClientWrapper(mockSlack),
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

	result := results[0]
	if result.Success {
		t.Error("expected failure")
	}
	if result.Error == nil {
		t.Error("expected error")
	}
}

func TestDeliverAllSlackNoClient(t *testing.T) {
	config := &BriefConfig{
		Channels: []ChannelConfig{
			{Type: "slack", Channel: "#test-channel"},
		},
	}

	service := NewDeliveryService(config) // No slack client

	brief := createTestBrief()
	ctx := context.Background()

	results := service.DeliverAll(ctx, brief)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Success {
		t.Error("expected failure without slack client")
	}
	if result.Error == nil {
		t.Error("expected error")
	}
}

func TestDeliverAllEmailSuccess(t *testing.T) {
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

	result := results[0]
	if !result.Success {
		t.Errorf("expected success, got error: %v", result.Error)
	}

	if len(mockEmail.lastTo) != 1 || mockEmail.lastTo[0] != "test@example.com" {
		t.Errorf("unexpected recipients: %v", mockEmail.lastTo)
	}

	if mockEmail.lastSubject == "" {
		t.Error("expected non-empty subject")
	}

	if mockEmail.lastHtmlBody == "" {
		t.Error("expected non-empty HTML body")
	}
}

func TestDeliverAllEmailFailure(t *testing.T) {
	config := &BriefConfig{
		Channels: []ChannelConfig{
			{Type: "email", Recipients: []string{"test@example.com"}},
		},
	}

	mockEmail := &mockEmailSender{shouldFail: true}

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

	result := results[0]
	if result.Success {
		t.Error("expected failure")
	}
}

func TestDeliverAllEmailNoSender(t *testing.T) {
	config := &BriefConfig{
		Channels: []ChannelConfig{
			{Type: "email", Recipients: []string{"test@example.com"}},
		},
	}

	service := NewDeliveryService(config) // No email sender

	brief := createTestBrief()
	ctx := context.Background()

	results := service.DeliverAll(ctx, brief)

	result := results[0]
	if result.Success {
		t.Error("expected failure without email sender")
	}
}

func TestDeliverAllEmailNoRecipients(t *testing.T) {
	config := &BriefConfig{
		Channels: []ChannelConfig{
			{Type: "email", Recipients: []string{}},
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

	result := results[0]
	if result.Success {
		t.Error("expected failure with no recipients")
	}
}

func TestDeliverAllTelegramSuccess(t *testing.T) {
	// Skip: Requires interface-based mocking for telegram.Client
	t.Skip("Requires interface-based mocking for telegram.Client")
}

func TestDeliverAllTelegramFailure(t *testing.T) {
	// Skip: Requires interface-based mocking for telegram.Client
	t.Skip("Requires interface-based mocking for telegram.Client")
}

func TestDeliverAllTelegramNoClient(t *testing.T) {
	config := &BriefConfig{
		Channels: []ChannelConfig{
			{Type: "telegram", Channel: "123456"},
		},
	}

	service := NewDeliveryService(config) // No telegram client

	brief := createTestBrief()
	ctx := context.Background()

	results := service.DeliverAll(ctx, brief)

	result := results[0]
	if result.Success {
		t.Error("expected failure without telegram client")
	}
}

func TestDeliverAllUnsupportedChannel(t *testing.T) {
	config := &BriefConfig{
		Channels: []ChannelConfig{
			{Type: "unsupported", Channel: "test"},
		},
	}

	service := NewDeliveryService(config)
	brief := createTestBrief()
	ctx := context.Background()

	results := service.DeliverAll(ctx, brief)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Success {
		t.Error("expected failure for unsupported channel")
	}
	if result.Error == nil {
		t.Error("expected error for unsupported channel")
	}
}

func TestDeliverAllMultipleChannels(t *testing.T) {
	config := &BriefConfig{
		Channels: []ChannelConfig{
			{Type: "email", Recipients: []string{"a@test.com"}},
			{Type: "email", Recipients: []string{"b@test.com"}},
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

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	// All should succeed
	for i, result := range results {
		if !result.Success {
			t.Errorf("result %d failed: %v", i, result.Error)
		}
	}
}

func TestDeliverSpecificChannel(t *testing.T) {
	config := &BriefConfig{
		Channels: []ChannelConfig{
			{Type: "email", Channel: "team1", Recipients: []string{"team1@test.com"}},
			{Type: "email", Channel: "team2", Recipients: []string{"team2@test.com"}},
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

	// Deliver to specific channel by full identifier
	result, err := service.Deliver(ctx, brief, "email:team1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %v", result.Error)
	}

	// Deliver to specific channel by name only
	result, err = service.Deliver(ctx, brief, "team2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %v", result.Error)
	}
}

func TestDeliverChannelNotFound(t *testing.T) {
	config := &BriefConfig{
		Channels: []ChannelConfig{
			{Type: "slack", Channel: "#channel1"},
		},
	}

	service := NewDeliveryService(config)
	brief := createTestBrief()
	ctx := context.Background()

	_, err := service.Deliver(ctx, brief, "#nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent channel")
	}
}

func TestDeliverUnsupportedChannelType(t *testing.T) {
	config := &BriefConfig{
		Channels: []ChannelConfig{
			{Type: "unknown", Channel: "test"},
		},
	}

	service := NewDeliveryService(config)
	brief := createTestBrief()
	ctx := context.Background()

	_, err := service.Deliver(ctx, brief, "unknown:test")
	if err == nil {
		t.Error("expected error for unsupported channel type")
	}
}
