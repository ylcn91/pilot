package pilot

import (
	"log/slog"

	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/logging"
)

// initAlerts initializes the alerts engine with configured channels
func (p *Pilot) initAlerts(cfg *config.Config) {
	log := logging.WithComponent("alerts")

	// Convert config.AlertsConfig to alerts.AlertConfig
	alertCfg := p.convertAlertsConfig(cfg.Alerts)

	// Create dispatcher with configured channels
	dispatcher := alerts.NewDispatcher(alertCfg, alerts.WithDispatcherLogger(log))

	// Register Slack channel if configured
	if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled && cfg.Adapters.Slack.BotToken != "" {
		p.slackClient = slack.NewClient(cfg.Adapters.Slack.BotToken)
		for _, ch := range cfg.Alerts.Channels {
			if ch.Type == "slack" && ch.Enabled && ch.Slack != nil {
				slackChannel := alerts.NewSlackChannel(ch.Name, p.slackClient, ch.Slack.Channel)
				dispatcher.RegisterChannel(slackChannel)
				log.Info("Registered Slack alert channel",
					slog.String("name", ch.Name),
					slog.String("channel", ch.Slack.Channel))
			}
		}
	}

	// Register Telegram channel if configured
	if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled && cfg.Adapters.Telegram.BotToken != "" {
		p.telegramClient = telegram.NewClient(cfg.Adapters.Telegram.BotToken)
		for _, ch := range cfg.Alerts.Channels {
			if ch.Type == "telegram" && ch.Enabled && ch.Telegram != nil {
				telegramChannel := alerts.NewTelegramChannel(ch.Name, p.telegramClient, ch.Telegram.ChatID)
				dispatcher.RegisterChannel(telegramChannel)
				log.Info("Registered Telegram alert channel",
					slog.String("name", ch.Name),
					slog.Int64("chat_id", ch.Telegram.ChatID))
			}
		}
	}

	// Register webhook channels
	for _, ch := range cfg.Alerts.Channels {
		if ch.Type == "webhook" && ch.Enabled && ch.Webhook != nil {
			webhookChannel := alerts.NewWebhookChannel(ch.Name, &alerts.WebhookChannelConfig{
				URL:     ch.Webhook.URL,
				Method:  ch.Webhook.Method,
				Headers: ch.Webhook.Headers,
				Secret:  ch.Webhook.Secret,
			})
			dispatcher.RegisterChannel(webhookChannel)
			log.Info("Registered webhook alert channel",
				slog.String("name", ch.Name),
				slog.String("url", ch.Webhook.URL))
		}
	}

	// Register email channels
	for _, ch := range cfg.Alerts.Channels {
		if ch.Type == "email" && ch.Enabled && ch.Email != nil && ch.Email.SMTPHost != "" {
			sender := alerts.NewSMTPSender(
				ch.Email.SMTPHost,
				ch.Email.SMTPPort,
				ch.Email.From,
				ch.Email.Username,
				ch.Email.Password,
			)
			emailChannel := alerts.NewEmailChannel(ch.Name, sender, ch.Email)
			dispatcher.RegisterChannel(emailChannel)
			log.Info("Registered email alert channel",
				slog.String("name", ch.Name),
				slog.Int("recipients", len(ch.Email.To)))
		}
	}

	// Register PagerDuty channels
	for _, ch := range cfg.Alerts.Channels {
		if ch.Type == "pagerduty" && ch.Enabled && ch.PagerDuty != nil {
			pdChannel := alerts.NewPagerDutyChannel(ch.Name, ch.PagerDuty)
			dispatcher.RegisterChannel(pdChannel)
			log.Info("Registered PagerDuty alert channel",
				slog.String("name", ch.Name))
		}
	}

	// Create engine with dispatcher
	p.alertEngine = alerts.NewEngine(alertCfg,
		alerts.WithLogger(log),
		alerts.WithDispatcher(dispatcher),
	)

	// Wire alerts engine to executor via adapter
	adapter := alerts.NewEngineAdapter(p.alertEngine)
	p.orchestrator.SetAlertProcessor(adapter)

	log.Info("Alerts engine initialized",
		slog.Int("rules", len(alertCfg.Rules)),
		slog.Int("channels", len(dispatcher.ListChannels())))
}

// convertAlertsConfig converts config.AlertsConfig to alerts.AlertConfig
func (p *Pilot) convertAlertsConfig(cfg *config.AlertsConfig) *alerts.AlertConfig {
	// Build channel configs (channel-specific configs are shared types, passed directly)
	channels := make([]alerts.ChannelConfigInput, len(cfg.Channels))
	for i, ch := range cfg.Channels {
		channels[i] = alerts.ChannelConfigInput{
			Name:       ch.Name,
			Type:       ch.Type,
			Enabled:    ch.Enabled,
			Severities: ch.Severities,
			Slack:      ch.Slack,     // Same type, direct pass-through
			Telegram:   ch.Telegram,  // Same type, direct pass-through
			Email:      ch.Email,     // Same type, direct pass-through
			Webhook:    ch.Webhook,   // Same type, direct pass-through
			PagerDuty:  ch.PagerDuty, // Same type, direct pass-through
		}
	}

	// Build rule configs
	rules := make([]alerts.RuleConfigInput, len(cfg.Rules))
	for i, r := range cfg.Rules {
		rules[i] = alerts.RuleConfigInput{
			Name:        r.Name,
			Type:        r.Type,
			Enabled:     r.Enabled,
			Severity:    r.Severity,
			Channels:    r.Channels,
			Cooldown:    r.Cooldown,
			Description: r.Description,
			Condition: alerts.ConditionConfigInput{
				ProgressUnchangedFor: r.Condition.ProgressUnchangedFor,
				ConsecutiveFailures:  r.Condition.ConsecutiveFailures,
				DailySpendThreshold:  r.Condition.DailySpendThreshold,
				BudgetLimit:          r.Condition.BudgetLimit,
				UsageSpikePercent:    r.Condition.UsageSpikePercent,
				Pattern:              r.Condition.Pattern,
				FilePattern:          r.Condition.FilePattern,
				Paths:                r.Condition.Paths,
			},
		}
	}

	// Build defaults
	defaults := alerts.DefaultsConfigInput{
		Cooldown:           cfg.Defaults.Cooldown,
		DefaultSeverity:    cfg.Defaults.DefaultSeverity,
		SuppressDuplicates: cfg.Defaults.SuppressDuplicates,
	}

	return alerts.FromConfigAlerts(cfg.Enabled, channels, rules, defaults)
}
