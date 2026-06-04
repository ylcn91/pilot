package main

import (
	"fmt"
	"log/slog"

	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/briefs"
	"github.com/ylcn91/pilot/internal/comms"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/logging"
)

func (p *pollingRuntime) startAdapterPollers() {
	cfg := p.cfg

	// GH-1847: Start adapter pollers via registry pattern (polling mode)
	pollingDeps := &PollerDeps{
		Cfg:                  cfg,
		ProjectPath:          p.projectPath,
		Dispatcher:           p.dispatcher,
		Runner:               p.runner,
		Monitor:              p.monitor,
		Program:              p.program,
		AlertsEngine:         p.alertsEngine,
		Enforcer:             p.enforcer,
		AutopilotController:  p.autopilotController,
		AutopilotStateStore:  p.autopilotStateStore,
		AutopilotControllers: p.autopilotControllers,
	}
	StartAdapterPollers(p.ctx, pollingDeps, adapterPollerRegistrations())
}

func (p *pollingRuntime) startTelegramPolling() {
	// Start Telegram polling if enabled
	if p.tgHandler != nil {
		if !p.dashboardMode {
			fmt.Println("📱 Telegram polling started")
		}
		p.tgHandler.StartPolling(p.ctx)
	}
}

func (p *pollingRuntime) startSlackSocketMode() {
	cfg := p.cfg
	ctx := p.ctx
	runner := p.runner
	store := p.store
	projectPath := p.projectPath

	// Start Slack Socket Mode if enabled (GH-652: wire into polling mode)
	var slackHandler *slack.Handler
	if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled && cfg.Adapters.Slack.SocketMode &&
		cfg.Adapters.Slack.AppToken != "" && cfg.Adapters.Slack.BotToken != "" {
		slackClient := slack.NewClient(cfg.Adapters.Slack.BotToken)
		slackMessenger := slack.NewMessenger(slackClient)

		var slackMemberResolver comms.MemberResolver
		if p.teamAdapter != nil {
			slackMemberResolver = &slack.MemberResolverAdapter{Inner: p.teamAdapter}
		}

		slackCommsHandler := comms.NewHandler(&comms.HandlerConfig{
			Messenger:      slackMessenger,
			Runner:         runner,
			Projects:       config.NewSlackProjectSource(cfg),
			ProjectPath:    projectPath,
			MemberResolver: slackMemberResolver,
			Store:          store,
			TaskIDPrefix:   "SLACK",
		})

		slackHandler = slack.NewHandler(&slack.HandlerConfig{
			AppToken:        cfg.Adapters.Slack.AppToken,
			Client:          slackClient,
			CommsHandler:    slackCommsHandler,
			AllowedChannels: cfg.Adapters.Slack.AllowedChannels,
			AllowedUsers:    cfg.Adapters.Slack.AllowedUsers,
		})

		logging.SafeGo("slack.socketmode", func() {
			if err := slackHandler.StartListening(ctx); err != nil {
				logging.WithComponent("slack").Error("Slack Socket Mode error", slog.Any("error", err))
			}
		})

		if !p.dashboardMode {
			fmt.Println("💬 Slack Socket Mode started")
		}
		logging.WithComponent("start").Info("Slack Socket Mode started in polling mode")
	}

	// Discord bot started via poller registry (poller_discord.go)
}

func (p *pollingRuntime) startBriefScheduler() {
	cfg := p.cfg
	ctx := p.ctx
	store := p.store

	// Start brief scheduler if enabled
	var briefScheduler *briefs.Scheduler
	if cfg.Orchestrator.DailyBrief != nil && cfg.Orchestrator.DailyBrief.Enabled {
		briefCfg := cfg.Orchestrator.DailyBrief

		// Convert config to briefs.BriefConfig
		briefsConfig := &briefs.BriefConfig{
			Enabled:  briefCfg.Enabled,
			Schedule: briefCfg.Schedule,
			Timezone: briefCfg.Timezone,
			Content: briefs.ContentConfig{
				IncludeMetrics:     briefCfg.Content.IncludeMetrics,
				IncludeErrors:      briefCfg.Content.IncludeErrors,
				MaxItemsPerSection: briefCfg.Content.MaxItemsPerSection,
			},
			Filters: briefs.FilterConfig{
				Projects: briefCfg.Filters.Projects,
			},
		}

		// Convert channels
		for _, ch := range briefCfg.Channels {
			briefsConfig.Channels = append(briefsConfig.Channels, briefs.ChannelConfig{
				Type:       ch.Type,
				Channel:    ch.Channel,
				Recipients: ch.Recipients,
			})
		}

		// Create generator (requires store)
		if store != nil {
			generator := briefs.NewGenerator(store, briefsConfig)

			// Create delivery service with available clients
			var deliveryOpts []briefs.DeliveryOption
			if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled {
				slackClient := slack.NewClient(cfg.Adapters.Slack.BotToken)
				deliveryOpts = append(deliveryOpts, briefs.WithSlackClient(slackClient))
			}
			if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled {
				tgClient := telegram.NewClient(cfg.Adapters.Telegram.BotToken)
				deliveryOpts = append(deliveryOpts, briefs.WithTelegramSender(&telegramBriefAdapter{client: tgClient}))
			}
			deliveryOpts = append(deliveryOpts, briefs.WithLogger(slog.Default()))

			delivery := briefs.NewDeliveryService(briefsConfig, deliveryOpts...)

			// Create and start scheduler
			briefScheduler = briefs.NewScheduler(generator, delivery, briefsConfig, slog.Default(), store)
			if err := briefScheduler.Start(ctx); err != nil {
				logging.WithComponent("start").Warn("Failed to start brief scheduler", slog.Any("error", err))
				briefScheduler = nil
			} else {
				logging.WithComponent("start").Info("brief scheduler started",
					slog.String("schedule", briefCfg.Schedule),
					slog.String("timezone", briefCfg.Timezone),
				)
			}
		} else {
			logging.WithComponent("start").Warn("Brief scheduler requires memory store, skipping")
		}
	}

	p.briefScheduler = briefScheduler
}
