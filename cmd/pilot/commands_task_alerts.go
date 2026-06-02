package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/config"
)

// setupTaskAlerts initializes the alerts engine for the task command when the
// --alerts flag is set. It loads config, registers Slack/Telegram channels,
// starts the engine, prints status, and emits the task-started event.
//
// The returned engine must be stopped at the *outer* RunE return, so the caller
// registers `defer engine.Stop()` once a non-nil engine is returned (mirroring
// the original defer placement, which only ran after Start succeeded). A nil
// engine with nil error means no engine was started.
func setupTaskAlerts(ctx context.Context, taskID, taskDesc, projectPath string) (*alerts.Engine, error) {
	// Load config for alerts
	configPath := cfgFile
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config for alerts: %w", err)
	}

	// Get alerts config
	alertsCfg := getAlertsConfig(cfg)
	if alertsCfg == nil {
		// Use default config with alerts enabled
		alertsCfg = alerts.DefaultConfig()
		alertsCfg.Enabled = true
	} else {
		alertsCfg.Enabled = true
	}

	// Create dispatcher and register channels
	dispatcher := alerts.NewDispatcher(alertsCfg)

	// Register Slack channel if configured
	if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled && cfg.Adapters.Slack.BotToken != "" {
		slackClient := slack.NewClient(cfg.Adapters.Slack.BotToken)
		for _, ch := range alertsCfg.Channels {
			if ch.Type == "slack" && ch.Slack != nil {
				slackChannel := alerts.NewSlackChannel(ch.Name, slackClient, ch.Slack.Channel)
				dispatcher.RegisterChannel(slackChannel)
			}
		}
	}

	// Register Telegram channel if configured
	if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled && cfg.Adapters.Telegram.BotToken != "" {
		telegramClient := telegram.NewClient(cfg.Adapters.Telegram.BotToken)
		for _, ch := range alertsCfg.Channels {
			if ch.Type == "telegram" && ch.Telegram != nil {
				telegramChannel := alerts.NewTelegramChannel(ch.Name, telegramClient, ch.Telegram.ChatID)
				dispatcher.RegisterChannel(telegramChannel)
			}
		}
	}

	alertsEngine := alerts.NewEngine(alertsCfg, alerts.WithDispatcher(dispatcher))
	if err := alertsEngine.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start alerts engine: %w", err)
	}

	fmt.Printf("   Alerts:    ✓ enabled (%d channels)\n", len(dispatcher.ListChannels()))

	// Send task started event
	alertsEngine.ProcessEvent(alerts.Event{
		Type:      alerts.EventTypeTaskStarted,
		TaskID:    taskID,
		TaskTitle: taskDesc,
		Project:   projectPath,
		Timestamp: time.Now(),
	})

	return alertsEngine, nil
}
