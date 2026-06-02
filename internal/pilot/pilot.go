package pilot

import (
	"context"
	"fmt"
	"io/fs"
	"sync"

	"github.com/ylcn91/pilot/internal/adapters/asana"
	"github.com/ylcn91/pilot/internal/adapters/azuredevops"
	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/gitlab"
	"github.com/ylcn91/pilot/internal/adapters/jira"
	"github.com/ylcn91/pilot/internal/adapters/linear"
	"github.com/ylcn91/pilot/internal/adapters/plane"
	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/gateway"
	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/orchestrator"
	"github.com/ylcn91/pilot/internal/teams"
	"github.com/ylcn91/pilot/internal/webhooks"
)

// Pilot is the main application
type Pilot struct {
	config                 *config.Config
	gateway                *gateway.Server
	orchestrator           *orchestrator.Orchestrator
	linearMultiWH          *linear.MultiWorkspaceHandler // Multi-workspace handler (GH-391)
	linearClient           *linear.Client                // Legacy single-workspace client
	linearWH               *linear.WebhookHandler        // Legacy single-workspace handler
	linearNotify           *linear.Notifier              // Legacy single-workspace notifier
	githubClient           *github.Client
	githubWH               *github.WebhookHandler
	githubNotify           *github.Notifier
	gitlabClient           *gitlab.Client
	gitlabWH               *gitlab.WebhookHandler
	gitlabNotify           *gitlab.Notifier
	jiraClient             *jira.Client
	jiraWH                 *jira.WebhookHandler
	azureDevOpsClient      *azuredevops.Client
	azureDevOpsWH          *azuredevops.WebhookHandler
	asanaClient            *asana.Client
	asanaWH                *asana.WebhookHandler
	planeWH                *plane.WebhookHandler
	slackNotify            *slack.Notifier
	slackClient            *slack.Client
	slackInteractionWH     *slack.InteractionHandler
	slackApprovalHdlr      *approval.SlackHandler
	telegramClient         *telegram.Client
	telegramHandler        *telegram.Handler                // Telegram polling handler (GH-349)
	telegramRunner         *executor.Runner                 // Runner for Telegram tasks (GH-349)
	telegramMemberResolver telegram.MemberResolver          // Team member resolver for Telegram RBAC (GH-634)
	telegramApprovalHdlr   telegram.ApprovalCallbackHandler // Routes approve:/reject: callbacks (GH-2651)
	slackHandler           *slack.Handler                   // Slack Socket Mode handler (GH-652)
	slackRunner            *executor.Runner                 // Runner for Slack tasks (GH-652)
	slackMemberResolver    slack.MemberResolver             // Team member resolver for Slack RBAC (GH-786)
	githubPoller           *github.Poller                   // GitHub polling handler (GH-350)
	alertEngine            *alerts.Engine
	teamsService           *teams.Service // Teams RBAC service (GH-633)
	store                  *memory.Store
	graph                  *memory.KnowledgeGraph
	webhookManager         *webhooks.Manager
	approvalMgr            *approval.Manager
	dashboardFS            fs.FS // Embedded React frontend (GH-1612)

	// linearTasks maps task IDs to Linear issue IDs for completion callbacks
	linearTasks   map[string]linearTaskInfo
	linearTasksMu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// slackApprovalClientAdapter wraps slack.SlackClientAdapter to satisfy approval.SlackClient interface
type slackApprovalClientAdapter struct {
	adapter *slack.SlackClientAdapter
}

func (a *slackApprovalClientAdapter) PostInteractiveMessage(ctx context.Context, msg *approval.SlackInteractiveMessage) (*approval.SlackPostMessageResponse, error) {
	resp, err := a.adapter.PostInteractiveMessage(ctx, &slack.SlackApprovalMessage{
		Channel: msg.Channel,
		Text:    msg.Text,
		Blocks:  msg.Blocks,
	})
	if err != nil {
		return nil, err
	}
	return &approval.SlackPostMessageResponse{
		OK:      resp.OK,
		TS:      resp.TS,
		Channel: resp.Channel,
		Error:   resp.Error,
	}, nil
}

func (a *slackApprovalClientAdapter) UpdateInteractiveMessage(ctx context.Context, channel, ts string, blocks []interface{}, text string) error {
	return a.adapter.UpdateInteractiveMessage(ctx, channel, ts, blocks, text)
}

// linearTaskInfo tracks Linear issue info for completion callbacks (GH-391)
type linearTaskInfo struct {
	IssueID       string
	WorkspaceName string // Empty for legacy single-workspace mode
}

// Option is a functional option for configuring Pilot
type Option func(*Pilot)

// WithTelegramHandler enables Telegram polling in gateway mode (GH-349)
// The runner is required to execute tasks from Telegram messages.
func WithTelegramHandler(runner *executor.Runner, projectPath string) Option {
	return func(p *Pilot) {
		p.telegramRunner = runner
		// Store projectPath in config for handler initialization
		// This is used in the Telegram handler setup
		if projectPath != "" && len(p.config.Projects) > 0 {
			// Use provided projectPath as override
			p.config.Projects[0].Path = projectPath
		}
	}
}

// WithTelegramMemberResolver sets the team member resolver for Telegram RBAC (GH-634).
func WithTelegramMemberResolver(resolver telegram.MemberResolver) Option {
	return func(p *Pilot) {
		p.telegramMemberResolver = resolver
	}
}

// WithTelegramApprovalHandler wires an approval callback handler into the Telegram message handler (GH-2651).
// When set, approve:/reject: button taps are dispatched to this handler instead of being silently dropped.
func WithTelegramApprovalHandler(h telegram.ApprovalCallbackHandler) Option {
	return func(p *Pilot) {
		p.telegramApprovalHdlr = h
	}
}

// WithGitHubPoller enables GitHub polling in gateway mode (GH-350)
// The poller is created externally with all necessary options and passed in.
func WithGitHubPoller(poller *github.Poller) Option {
	return func(p *Pilot) {
		p.githubPoller = poller
	}
}

// WithTeamsService enables team-scoped execution (GH-633)
// When set, Pilot uses team RBAC for permission checks and audit logging.
func WithTeamsService(svc *teams.Service) Option {
	return func(p *Pilot) {
		p.teamsService = svc
	}
}

// WithSlackHandler enables Slack Socket Mode in gateway mode (GH-652)
// The runner is required to execute tasks from Slack messages.
func WithSlackHandler(runner *executor.Runner, projectPath string) Option {
	return func(p *Pilot) {
		p.slackRunner = runner
		// Store projectPath in config for handler initialization
		// This is used in the Slack handler setup
		if projectPath != "" && len(p.config.Projects) > 0 {
			// Use provided projectPath as override
			p.config.Projects[0].Path = projectPath
		}
	}
}

// WithSlackMemberResolver sets the team member resolver for Slack RBAC (GH-786).
func WithSlackMemberResolver(resolver slack.MemberResolver) Option {
	return func(p *Pilot) {
		p.slackMemberResolver = resolver
	}
}

// WithDashboardFS sets the embedded React frontend filesystem (GH-1612).
// When set, the gateway serves the dashboard at /dashboard/.
func WithDashboardFS(fsys fs.FS) Option {
	return func(p *Pilot) {
		p.dashboardFS = fsys
	}
}

// parseInt64 parses a string to int64
func parseInt64(s string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(s, "%d", &id)
	return id, err
}
