package pilot

import (
	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/comms"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/logging"
)

// initTelegramHandler initializes the Telegram handler if a runner was provided via options (GH-349).
// This enables Telegram polling in gateway mode alongside Linear/Jira webhooks.
func (p *Pilot) initTelegramHandler(cfg *config.Config) {
	if p.telegramRunner == nil || cfg.Adapters.Telegram == nil || !cfg.Adapters.Telegram.Enabled || !cfg.Adapters.Telegram.Polling {
		return
	}

	var allowedIDs []int64
	allowedIDs = append(allowedIDs, cfg.Adapters.Telegram.AllowedIDs...)
	if cfg.Adapters.Telegram.ChatID != "" {
		if id, err := parseInt64(cfg.Adapters.Telegram.ChatID); err == nil {
			allowedIDs = append(allowedIDs, id)
		}
	}

	// Get project path - use first project if available
	projectPath := ""
	if len(cfg.Projects) > 0 {
		projectPath = cfg.Projects[0].Path
	}

	tgClient := telegram.NewClient(cfg.Adapters.Telegram.BotToken)
	tgMessenger := telegram.NewMessenger(tgClient, true) // Default to plain text mode

	// Build comms.MemberResolver wrapper (GH-634)
	var tgMemberResolver comms.MemberResolver
	if p.telegramMemberResolver != nil {
		tgMemberResolver = &telegram.MemberResolverAdapter{Inner: p.telegramMemberResolver}
	}

	tgCommsHandler := comms.NewHandler(&comms.HandlerConfig{
		Messenger:      tgMessenger,
		Runner:         p.telegramRunner,
		Projects:       config.NewProjectSource(cfg),
		ProjectPath:    projectPath,
		RateLimit:      cfg.Adapters.Telegram.RateLimit,
		MemberResolver: tgMemberResolver,
		Store:          p.store,
		TaskIDPrefix:   "TG",
	})

	p.telegramHandler = telegram.NewHandler(&telegram.HandlerConfig{
		Client:          tgClient,
		CommsHandler:    tgCommsHandler,
		ProjectPath:     projectPath,
		Projects:        config.NewProjectSource(cfg),
		AllowedIDs:      allowedIDs,
		Transcription:   cfg.Adapters.Telegram.Transcription,
		Store:           p.store,
		ApprovalHandler: p.telegramApprovalHdlr,
	}, p.telegramRunner)

	if len(allowedIDs) == 0 {
		logging.WithComponent("pilot").Warn("SECURITY: telegram allowed_ids is empty - ALL users can interact with the bot!")
	}

	logging.WithComponent("pilot").Info("Telegram handler initialized for gateway mode")
}

// initSlackHandler initializes the Slack handler if a runner was provided via options (GH-652).
// This enables Slack Socket Mode in gateway mode alongside other adapters.
func (p *Pilot) initSlackHandler(cfg *config.Config) {
	if p.slackRunner == nil || cfg.Adapters.Slack == nil || !cfg.Adapters.Slack.Enabled || !cfg.Adapters.Slack.SocketMode {
		return
	}

	// Get project path - use first project if available
	projectPath := ""
	if len(cfg.Projects) > 0 {
		projectPath = cfg.Projects[0].Path
	}

	slackClient := slack.NewClient(cfg.Adapters.Slack.BotToken)
	slackMessenger := slack.NewMessenger(slackClient)

	var slackMemberResolver comms.MemberResolver
	if p.slackMemberResolver != nil {
		slackMemberResolver = &slack.MemberResolverAdapter{Inner: p.slackMemberResolver}
	}

	slackCommsHandler := comms.NewHandler(&comms.HandlerConfig{
		Messenger:      slackMessenger,
		Runner:         p.slackRunner,
		Projects:       config.NewSlackProjectSource(cfg),
		ProjectPath:    projectPath,
		MemberResolver: slackMemberResolver,
		Store:          p.store,
		TaskIDPrefix:   "SLACK",
	})

	p.slackHandler = slack.NewHandler(&slack.HandlerConfig{
		AppToken:        cfg.Adapters.Slack.AppToken,
		Client:          slackClient,
		CommsHandler:    slackCommsHandler,
		AllowedChannels: cfg.Adapters.Slack.AllowedChannels,
		AllowedUsers:    cfg.Adapters.Slack.AllowedUsers,
	})

	if len(cfg.Adapters.Slack.AllowedChannels) == 0 && len(cfg.Adapters.Slack.AllowedUsers) == 0 {
		logging.WithComponent("pilot").Warn("SECURITY: slack allowed_channels and allowed_users are empty - ALL users can interact with the bot!")
	}

	logging.WithComponent("pilot").Info("Slack handler initialized for gateway mode")
}
