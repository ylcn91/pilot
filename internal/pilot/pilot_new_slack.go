package pilot

import (
	"context"
	"log/slog"

	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/logging"
)

// initSlackNotifier initializes the Slack notifier and, if enabled, the Slack approval
// handler and interaction webhook handler, registering the approval handler on the
// approval manager. Mirrors the original inline block in New exactly.
func (p *Pilot) initSlackNotifier(cfg *config.Config) {
	if cfg.Adapters.Slack == nil || !cfg.Adapters.Slack.Enabled {
		return
	}
	p.slackNotify = slack.NewNotifier(cfg.Adapters.Slack)

	// Initialize Slack approval handler if enabled
	if cfg.Adapters.Slack.Approval != nil && cfg.Adapters.Slack.Approval.Enabled {
		p.slackClient = slack.NewClient(cfg.Adapters.Slack.BotToken)
		slackAdapter := slack.NewSlackClientAdapter(p.slackClient)
		approvalChannel := cfg.Adapters.Slack.Approval.Channel
		if approvalChannel == "" {
			approvalChannel = cfg.Adapters.Slack.Channel
		}
		p.slackApprovalHdlr = approval.NewSlackHandler(
			&slackApprovalClientAdapter{adapter: slackAdapter},
			approvalChannel,
		)
		p.approvalMgr.RegisterHandler(p.slackApprovalHdlr)
		logging.WithComponent("pilot").Info("registered Slack approval handler",
			slog.String("channel", approvalChannel))

		// Set up Slack interaction webhook handler
		signingSecret := cfg.Adapters.Slack.Approval.SigningSecret
		if signingSecret == "" {
			signingSecret = cfg.Adapters.Slack.SigningSecret
		}
		p.slackInteractionWH = slack.NewInteractionHandler(signingSecret)
		p.slackInteractionWH.OnAction(func(action *slack.InteractionAction) bool {
			return p.slackApprovalHdlr.HandleInteraction(
				context.Background(),
				action.ActionID,
				action.Value,
				action.UserID,
				action.Username,
				action.ResponseURL,
			)
		})
	}
}
