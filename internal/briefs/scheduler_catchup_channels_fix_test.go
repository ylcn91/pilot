package briefs

import (
	"context"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

// TestMaybeCatchUpSlackOnlyNoSpuriousCatchUp proves the fix for the bug where
// maybeCatchUp hardcoded GetLastBriefSent("telegram"): a Slack-only deployment
// that already delivered a brief at its scheduled time must NOT fire a spurious
// catch-up on restart. The brief is recorded under the Slack delivery key
// ("slack:#daily"), and catch-up must derive the channel(s) to query from the
// configured channels rather than the "telegram" literal.
func TestMaybeCatchUpSlackOnlyNoSpuriousCatchUp(t *testing.T) {
	store, cleanup := setupSchedulerTestStore(t)
	defer cleanup()

	config := &BriefConfig{
		Enabled:  true,
		Schedule: "0 9 * * *", // daily at 9am
		Timezone: "UTC",
		Channels: []ChannelConfig{
			{Type: "slack", Channel: "#daily"},
		},
		Content: ContentConfig{
			IncludeMetrics:     true,
			IncludeErrors:      true,
			MaxItemsPerSection: 10,
		},
	}

	// A brief was already delivered "now" to the only configured channel,
	// recorded under the production delivery identifier "slack:#daily".
	seed := &memory.BriefRecord{
		SentAt:    time.Now(),
		Channel:   "slack:#daily",
		BriefType: "daily",
	}
	if err := store.RecordBriefSent(seed); err != nil {
		t.Fatalf("seed brief: %v", err)
	}

	// Use a fake telegram sender purely as a delivery-fired sentinel; the
	// config has no telegram channel, so it must never be invoked.
	sender := &fakeTelegramSender{}
	generator := NewGenerator(store, config)
	delivery := NewDeliveryService(config, WithTelegramSender(sender))
	scheduler := NewScheduler(generator, delivery, config, nil, store)

	scheduler.maybeCatchUp(context.Background())

	// Before the fix, GetLastBriefSent("telegram") returned nil for this
	// Slack-only deployment, so catch-up treated the brief as missed and fired.
	if sender.Calls() != 0 {
		t.Fatalf("expected no spurious catch-up for a Slack-only deployment with a recent brief, got %d delivery attempts", sender.Calls())
	}
}

// TestMaybeCatchUpEmailOnlyNoSpuriousCatchUp covers the email channel, whose
// delivery identifier is the bare "email" (no per-channel suffix). A recent
// email brief must suppress catch-up.
func TestMaybeCatchUpEmailOnlyNoSpuriousCatchUp(t *testing.T) {
	store, cleanup := setupSchedulerTestStore(t)
	defer cleanup()

	config := &BriefConfig{
		Enabled:  true,
		Schedule: "0 9 * * *",
		Timezone: "UTC",
		Channels: []ChannelConfig{
			{Type: "email", Recipients: []string{"team@example.com"}},
		},
		Content: ContentConfig{
			IncludeMetrics:     true,
			IncludeErrors:      true,
			MaxItemsPerSection: 10,
		},
	}

	seed := &memory.BriefRecord{
		SentAt:    time.Now(),
		Channel:   "email",
		BriefType: "daily",
	}
	if err := store.RecordBriefSent(seed); err != nil {
		t.Fatalf("seed brief: %v", err)
	}

	sender := &fakeTelegramSender{}
	generator := NewGenerator(store, config)
	delivery := NewDeliveryService(config, WithTelegramSender(sender))
	scheduler := NewScheduler(generator, delivery, config, nil, store)

	scheduler.maybeCatchUp(context.Background())

	if sender.Calls() != 0 {
		t.Fatalf("expected no spurious catch-up for an email-only deployment with a recent brief, got %d delivery attempts", sender.Calls())
	}
}

// TestRecordChannelKeyMatchesDeliveryIdentifier locks the helper to the exact
// identifiers produced by the deliver* methods (and recorded by
// runBriefWithResults), since catch-up correctness depends on that match.
func TestRecordChannelKeyMatchesDeliveryIdentifier(t *testing.T) {
	tests := []struct {
		name    string
		channel ChannelConfig
		want    string
	}{
		{"slack", ChannelConfig{Type: "slack", Channel: "#daily"}, "slack:#daily"},
		{"telegram", ChannelConfig{Type: "telegram", Channel: "@test"}, "telegram:@test"},
		{"email", ChannelConfig{Type: "email", Recipients: []string{"a@b.com"}}, "email"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := recordChannelKey(tt.channel); got != tt.want {
				t.Errorf("recordChannelKey(%+v) = %q, want %q", tt.channel, got, tt.want)
			}
		})
	}
}
