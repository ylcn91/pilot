package main

import (
	"log/slog"

	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/logging"
)

func (p *pollingRuntime) setupApproval() {
	cfg := p.cfg

	// Create approval manager
	approvalMgr := approval.NewManager(cfg.Approval)

	// Register Telegram approval handler if enabled
	var tgApprovalHandler telegram.ApprovalCallbackHandler
	var tgApprovalHandlerImpl *approval.TelegramHandler
	if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled && cfg.Adapters.Telegram.BotToken != "" &&
		(cfg.Adapters.Telegram.Approval == nil || cfg.Adapters.Telegram.Approval.Enabled) {
		tgApprovalClient := telegram.NewClient(cfg.Adapters.Telegram.BotToken)
		tgApprovalHandlerImpl = approval.NewTelegramHandler(&telegramApprovalAdapter{client: tgApprovalClient}, cfg.Adapters.Telegram.ChatID)
		approvalMgr.RegisterHandler(tgApprovalHandlerImpl)
		tgApprovalHandler = tgApprovalHandlerImpl
		logging.WithComponent("start").Info("registered Telegram approval handler")
	}

	// Register Slack approval handler if enabled
	if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled && cfg.Adapters.Slack.BotToken != "" {
		if cfg.Adapters.Slack.Approval != nil && cfg.Adapters.Slack.Approval.Enabled {
			slackClient := slack.NewClient(cfg.Adapters.Slack.BotToken)
			slackAdapter := slack.NewSlackClientAdapter(slackClient)
			slackChannel := cfg.Adapters.Slack.Approval.Channel
			if slackChannel == "" {
				slackChannel = cfg.Adapters.Slack.Channel
			}
			slackApprovalHandler := approval.NewSlackHandler(&slackApprovalClientAdapter{adapter: slackAdapter}, slackChannel)
			approvalMgr.RegisterHandler(slackApprovalHandler)
			logging.WithComponent("start").Info("registered Slack approval handler",
				slog.String("channel", slackChannel))
		}
	}

	p.approvalMgr = approvalMgr
	p.tgApprovalHandler = tgApprovalHandler
	p.tgApprovalHandlerImpl = tgApprovalHandlerImpl
}
