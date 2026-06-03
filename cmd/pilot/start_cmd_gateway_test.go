package main

import (
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/testutil"
)

// =============================================================================
// Gap (cli-config-wiring): buildGatewayInfra has no tests. The God function was
// split into buildGatewayApprovalManager / buildGatewayAutopilot /
// buildGatewayAlertsEngine; these exercise the testable extracted builders by
// asserting their observable side effects on the gatewayInfra struct.
// =============================================================================

func TestBuildGatewayApprovalManager_TelegramHandler(t *testing.T) {
	tests := []struct {
		name          string
		telegram      *telegram.Config
		wantTgHandler bool
		wantNonNilMgr bool
	}{
		{
			name:          "telegram disabled",
			telegram:      &telegram.Config{Enabled: false, BotToken: testutil.FakeTelegramBotToken},
			wantTgHandler: false,
			wantNonNilMgr: true,
		},
		{
			name:          "telegram enabled but no bot token",
			telegram:      &telegram.Config{Enabled: true, BotToken: ""},
			wantTgHandler: false,
			wantNonNilMgr: true,
		},
		{
			name:          "telegram enabled with token, default approval (nil => on)",
			telegram:      &telegram.Config{Enabled: true, BotToken: testutil.FakeTelegramBotToken, ChatID: "123"},
			wantTgHandler: true,
			wantNonNilMgr: true,
		},
		{
			name: "telegram enabled but approval explicitly disabled",
			telegram: &telegram.Config{
				Enabled:  true,
				BotToken: testutil.FakeTelegramBotToken,
				ChatID:   "123",
				Approval: &telegram.ApprovalConfig{Enabled: false},
			},
			wantTgHandler: false,
			wantNonNilMgr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Approval: approval.DefaultConfig(),
				Adapters: &config.AdaptersConfig{
					Telegram: tt.telegram,
					Slack:    &slack.Config{Enabled: false},
				},
			}
			gw := &gatewayInfra{} // Store is nil -> no rehydrate path
			mgr := buildGatewayApprovalManager(gw, cfg)

			if tt.wantNonNilMgr && mgr == nil {
				t.Fatal("expected non-nil approval manager")
			}
			if got := gw.TgApprovalHandler != nil; got != tt.wantTgHandler {
				t.Errorf("TgApprovalHandler set = %v, want %v", got, tt.wantTgHandler)
			}
		})
	}
}

func TestBuildGatewayApprovalManager_SlackHandler(t *testing.T) {
	// Slack approval handler registration must not panic and must not touch the
	// Telegram handler field. There is no public accessor for registered
	// handlers on approval.Manager, so we assert the manager is constructed and
	// the Telegram side effect stays nil.
	cfg := &config.Config{
		Approval: approval.DefaultConfig(),
		Adapters: &config.AdaptersConfig{
			Telegram: &telegram.Config{Enabled: false},
			Slack: &slack.Config{
				Enabled:  true,
				BotToken: testutil.FakeSlackBotToken,
				Channel:  "#dev",
				Approval: &slack.ApprovalConfig{Enabled: true, Channel: "#approvals"},
			},
		},
	}
	gw := &gatewayInfra{}
	mgr := buildGatewayApprovalManager(gw, cfg)
	if mgr == nil {
		t.Fatal("expected non-nil approval manager")
	}
	if gw.TgApprovalHandler != nil {
		t.Error("Telegram handler should remain nil when only Slack approval is enabled")
	}
}

func TestBuildGatewayAlertsEngine(t *testing.T) {
	tests := []struct {
		name       string
		alerts     *config.AlertsConfig
		wantEngine bool
	}{
		{
			name:       "alerts nil",
			alerts:     nil,
			wantEngine: false,
		},
		{
			name:       "alerts disabled",
			alerts:     &config.AlertsConfig{Enabled: false},
			wantEngine: false,
		},
		{
			name: "alerts enabled with no channels",
			alerts: &config.AlertsConfig{
				Enabled: true,
				Defaults: config.AlertDefaultsConfig{
					Cooldown:        5 * time.Minute,
					DefaultSeverity: "warning",
				},
			},
			wantEngine: true,
		},
		{
			name: "alerts enabled with webhook channel",
			alerts: &config.AlertsConfig{
				Enabled: true,
				Channels: []config.AlertChannelConfig{
					{
						Name:    "wh",
						Type:    "webhook",
						Enabled: true,
						Webhook: &alerts.WebhookChannelConfig{URL: "https://hooks.test/alert"},
					},
				},
				Defaults: config.AlertDefaultsConfig{
					Cooldown:        5 * time.Minute,
					DefaultSeverity: "warning",
				},
			},
			wantEngine: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Alerts: tt.alerts,
				Adapters: &config.AdaptersConfig{
					Slack:    &slack.Config{Enabled: false},
					Telegram: &telegram.Config{Enabled: false},
				},
			}
			gw := &gatewayInfra{}
			buildGatewayAlertsEngine(gw, cfg)

			if got := gw.AlertsEngine != nil; got != tt.wantEngine {
				t.Errorf("AlertsEngine set = %v, want %v", got, tt.wantEngine)
			}
			if gw.AlertsEngine != nil {
				gw.AlertsEngine.Stop()
			}
		})
	}
}
