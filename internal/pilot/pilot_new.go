package pilot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/ylcn91/pilot/internal/adapters/asana"
	"github.com/ylcn91/pilot/internal/adapters/azuredevops"
	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/gitlab"
	"github.com/ylcn91/pilot/internal/adapters/jira"
	"github.com/ylcn91/pilot/internal/adapters/linear"
	"github.com/ylcn91/pilot/internal/adapters/plane"
	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/comms"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/gateway"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/orchestrator"
	"github.com/ylcn91/pilot/internal/webhooks"
)

// New creates a new Pilot instance
func New(cfg *config.Config, opts ...Option) (*Pilot, error) {
	ctx, cancel := context.WithCancel(context.Background())

	p := &Pilot{
		config:      cfg,
		ctx:         ctx,
		cancel:      cancel,
		linearTasks: make(map[string]linearTaskInfo),
	}

	// Initialize memory store
	store, err := memory.NewStore(cfg.Memory.Path)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create memory store: %w", err)
	}
	p.store = store

	// Initialize knowledge graph
	graph, err := memory.NewKnowledgeGraph(cfg.Memory.Path)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create knowledge graph: %w", err)
	}
	p.graph = graph

	// Initialize approval manager
	p.approvalMgr = approval.NewManager(cfg.Approval)

	// Initialize Slack notifier if enabled
	if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled {
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

	// Initialize webhook manager
	p.webhookManager = webhooks.NewManager(cfg.Webhooks, logging.WithComponent("webhooks"))

	// Initialize orchestrator
	orchConfig := &orchestrator.Config{
		Model:         cfg.Orchestrator.Model,
		MaxConcurrent: cfg.Orchestrator.MaxConcurrent,
		BackendConfig: cfg.Executor, // GH-2286: pass executor config to runner
	}
	orch, err := orchestrator.NewOrchestrator(orchConfig, p.slackNotify)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create orchestrator: %w", err)
	}
	p.orchestrator = orch

	// Register completion callback for platform notifications + outbound webhooks
	p.orchestrator.OnCompletion(p.handleTaskCompletion)

	// Register progress callback for outbound webhooks
	p.orchestrator.OnProgress(func(taskID, phase string, progress int, message string) {
		if p.webhookManager.IsEnabled() {
			p.webhookManager.Dispatch(ctx, webhooks.NewEvent(webhooks.EventTaskProgress, &webhooks.TaskProgressData{
				TaskID:   taskID,
				Phase:    phase,
				Progress: float64(progress),
				Message:  message,
			}))
		}
	})

	// Initialize Linear adapter if enabled (GH-391: multi-workspace support)
	if cfg.Adapters.Linear != nil && cfg.Adapters.Linear.Enabled {
		workspaces := cfg.Adapters.Linear.GetWorkspaces()
		if len(workspaces) > 1 || (len(workspaces) == 1 && len(cfg.Adapters.Linear.Workspaces) > 0) {
			// Multi-workspace mode
			multiWH, err := linear.NewMultiWorkspaceHandler(cfg.Adapters.Linear)
			if err != nil {
				cancel()
				return nil, fmt.Errorf("failed to create Linear multi-workspace handler: %w", err)
			}
			p.linearMultiWH = multiWH
			p.linearMultiWH.OnIssue(p.handleLinearIssueMultiWorkspace)
			logging.WithComponent("pilot").Info("Linear multi-workspace mode enabled",
				slog.Int("workspaces", p.linearMultiWH.WorkspaceCount()))
		} else {
			// Legacy single-workspace mode
			p.linearClient = linear.NewClient(cfg.Adapters.Linear.APIKey)
			pilotLabel := cfg.Adapters.Linear.PilotLabel
			if pilotLabel == "" {
				pilotLabel = "pilot"
			}
			p.linearWH = linear.NewWebhookHandler(p.linearClient, pilotLabel, cfg.Adapters.Linear.ProjectIDs)
			p.linearWH.OnIssue(p.handleLinearIssue)
			p.linearNotify = linear.NewNotifier(p.linearClient)
		}
	}

	// Initialize GitHub adapter if enabled
	if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled {
		p.githubClient = github.NewClient(cfg.Adapters.GitHub.Token)
		p.githubWH = github.NewWebhookHandler(
			p.githubClient,
			cfg.Adapters.GitHub.WebhookSecret,
			cfg.Adapters.GitHub.PilotLabel,
		)
		p.githubWH.OnIssue(p.handleGithubIssue)
		p.githubNotify = github.NewNotifier(p.githubClient, cfg.Adapters.GitHub.PilotLabel)
	}

	// Initialize GitLab adapter if enabled
	if cfg.Adapters.GitLab != nil && cfg.Adapters.GitLab.Enabled {
		p.gitlabClient = gitlab.NewClient(cfg.Adapters.GitLab.Token, cfg.Adapters.GitLab.Project)
		p.gitlabWH = gitlab.NewWebhookHandler(
			p.gitlabClient,
			cfg.Adapters.GitLab.WebhookSecret,
			cfg.Adapters.GitLab.PilotLabel,
		)
		p.gitlabWH.OnIssue(p.handleGitlabIssue)
		p.gitlabNotify = gitlab.NewNotifier(p.gitlabClient, cfg.Adapters.GitLab.PilotLabel)
	}

	// Initialize Jira adapter if enabled
	if cfg.Adapters.Jira != nil && cfg.Adapters.Jira.Enabled {
		p.jiraClient = jira.NewClient(
			cfg.Adapters.Jira.BaseURL,
			cfg.Adapters.Jira.Username,
			cfg.Adapters.Jira.APIToken,
			cfg.Adapters.Jira.Platform,
		)
		pilotLabel := cfg.Adapters.Jira.PilotLabel
		if pilotLabel == "" {
			pilotLabel = "pilot"
		}
		p.jiraWH = jira.NewWebhookHandler(p.jiraClient, cfg.Adapters.Jira.WebhookSecret, pilotLabel)
		p.jiraWH.OnIssue(p.handleJiraIssue)
	}

	// GH-1699: Initialize Azure DevOps webhook handler if configured
	if cfg.Adapters.AzureDevOps != nil && cfg.Adapters.AzureDevOps.Enabled {
		p.azureDevOpsClient = azuredevops.NewClientWithConfig(cfg.Adapters.AzureDevOps)
		pilotTag := cfg.Adapters.AzureDevOps.PilotTag
		if pilotTag == "" {
			pilotTag = "pilot"
		}
		p.azureDevOpsWH = azuredevops.NewWebhookHandler(p.azureDevOpsClient, cfg.Adapters.AzureDevOps.WebhookSecret, pilotTag)
		if len(cfg.Adapters.AzureDevOps.WorkItemTypes) > 0 {
			p.azureDevOpsWH.SetWorkItemTypes(cfg.Adapters.AzureDevOps.WorkItemTypes)
		}
	}

	// GH-2044: Initialize Asana adapter if enabled
	if cfg.Adapters.Asana != nil && cfg.Adapters.Asana.Enabled {
		p.asanaClient = asana.NewClient(cfg.Adapters.Asana.AccessToken, cfg.Adapters.Asana.WorkspaceID)
		pilotTag := cfg.Adapters.Asana.PilotTag
		if pilotTag == "" {
			pilotTag = "pilot"
		}
		p.asanaWH = asana.NewWebhookHandler(p.asanaClient, cfg.Adapters.Asana.WebhookSecret, pilotTag)
		p.asanaWH.OnTask(p.handleAsanaTask)
	}

	// GH-2044: Initialize Plane adapter if enabled
	if cfg.Adapters.Plane != nil && cfg.Adapters.Plane.Enabled {
		pilotLabel := cfg.Adapters.Plane.PilotLabel
		if pilotLabel == "" {
			pilotLabel = "pilot"
		}
		p.planeWH = plane.NewWebhookHandler(cfg.Adapters.Plane.WebhookSecret, pilotLabel, cfg.Adapters.Plane.ProjectIDs)
		p.planeWH.OnWorkItem(p.handlePlaneWorkItem)
	}

	// Initialize alerts engine if enabled
	if cfg.Alerts != nil && cfg.Alerts.Enabled {
		p.initAlerts(cfg)
	}

	// Initialize gateway with webhook secrets
	gatewayCfg := cfg.Gateway
	if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.WebhookSecret != "" {
		gatewayCfg.GithubWebhookSecret = cfg.Adapters.GitHub.WebhookSecret
	}
	if cfg.Adapters.Linear != nil && cfg.Adapters.Linear.WebhookPublicKey != "" {
		key, err := parseLinearWebhookKey(cfg.Adapters.Linear.WebhookPublicKey)
		if err != nil {
			logging.WithComponent("pilot").Error("Linear webhook public key invalid — signature verification disabled", slog.Any("error", err))
		} else {
			gatewayCfg.LinearWebhookPublicKey = key
		}
	} else {
		logging.WithComponent("pilot").Info("Linear webhook signature verification disabled — set adapters.linear.webhook_public_key to enable")
	}
	// Loud startup warning when the fail-closed default is bypassed.
	// PILOT_ALLOW_UNSIGNED_WEBHOOKS=1 disables signature verification on every adapter.
	if os.Getenv("PILOT_ALLOW_UNSIGNED_WEBHOOKS") == "1" {
		logging.WithComponent("pilot").Warn("PILOT_ALLOW_UNSIGNED_WEBHOOKS=1 — webhook signature verification DISABLED across all adapters; never use this in production")
	}
	p.gateway = gateway.NewServer(gatewayCfg)

	// Register webhook handlers
	if p.linearMultiWH != nil {
		// Multi-workspace mode (GH-391)
		p.gateway.Router().RegisterWebhookHandler("linear", func(payload map[string]interface{}) {
			if err := p.linearMultiWH.Handle(ctx, payload); err != nil {
				logging.WithComponent("pilot").Error("Linear webhook error", slog.Any("error", err))
			}
		})
	} else if p.linearWH != nil {
		// Legacy single-workspace mode
		p.gateway.Router().RegisterWebhookHandler("linear", func(payload map[string]interface{}) {
			if err := p.linearWH.Handle(ctx, payload); err != nil {
				logging.WithComponent("pilot").Error("Linear webhook error", slog.Any("error", err))
			}
		})
	}

	if p.githubWH != nil {
		p.gateway.Router().RegisterWebhookHandler("github", func(payload map[string]interface{}) {
			eventType, _ := payload["_event_type"].(string)
			if err := p.githubWH.Handle(ctx, eventType, payload); err != nil {
				logging.WithComponent("pilot").Error("GitHub webhook error", slog.Any("error", err))
			}
		})
	}

	if p.gitlabWH != nil {
		p.gateway.Router().RegisterWebhookHandler("gitlab", func(payload map[string]interface{}) {
			eventType, _ := payload["_event_type"].(string)
			token, _ := payload["_token"].(string)

			// Verify webhook token
			if !p.gitlabWH.VerifyToken(token) {
				logging.WithComponent("pilot").Warn("GitLab webhook token verification failed")
				return
			}

			// Parse the webhook payload
			webhookPayload := p.parseGitlabWebhookPayload(payload)
			if webhookPayload == nil {
				logging.WithComponent("pilot").Warn("Failed to parse GitLab webhook payload")
				return
			}

			if err := p.gitlabWH.Handle(ctx, eventType, webhookPayload); err != nil {
				logging.WithComponent("pilot").Error("GitLab webhook error", slog.Any("error", err))
			}
		})
	}

	if p.jiraWH != nil {
		p.gateway.Router().RegisterWebhookHandler("jira", func(payload map[string]interface{}) {
			signature, _ := payload["_signature"].(string)
			// TASK-333: verify the HMAC over the exact raw request body the gateway
			// buffered, not a re-marshaled map (which would not reproduce the bytes
			// Jira signed).
			rawBody, _ := payload["_raw_body"].(string)
			if !p.jiraWH.VerifySignature([]byte(rawBody), signature) {
				logging.WithComponent("pilot").Warn("Jira webhook signature verification failed")
				return
			}
			if err := p.jiraWH.Handle(ctx, payload); err != nil {
				logging.WithComponent("pilot").Error("Jira webhook error", slog.Any("error", err))
			}
		})
	}

	// GH-1699: Register Azure DevOps webhook handler
	if p.azureDevOpsWH != nil {
		p.gateway.Router().RegisterWebhookHandler("azuredevops", func(payload map[string]interface{}) {
			// Verify webhook secret
			secret, _ := payload["_secret"].(string)
			if !p.azureDevOpsWH.VerifySecret(secret) {
				logging.WithComponent("pilot").Warn("Azure DevOps webhook secret verification failed")
				return
			}

			// Parse the raw payload into WebhookPayload
			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				logging.WithComponent("pilot").Error("Failed to marshal Azure DevOps payload", slog.Any("error", err))
				return
			}

			var webhookPayload azuredevops.WebhookPayload
			if err := json.Unmarshal(payloadBytes, &webhookPayload); err != nil {
				logging.WithComponent("pilot").Error("Failed to parse Azure DevOps webhook payload", slog.Any("error", err))
				return
			}

			if err := p.azureDevOpsWH.Handle(ctx, &webhookPayload); err != nil {
				logging.WithComponent("pilot").Error("Azure DevOps webhook error", slog.Any("error", err))
			}
		})
	}

	// GH-2044: Register Asana webhook handler
	if p.asanaWH != nil {
		p.gateway.Router().RegisterWebhookHandler("asana", func(payload map[string]interface{}) {
			signature, _ := payload["_signature"].(string)
			// TASK-333: verify the HMAC over the exact raw request body the gateway
			// buffered, not a re-marshaled map (which would not reproduce the bytes
			// Asana signed).
			rawBody, _ := payload["_raw_body"].(string)
			if !p.asanaWH.VerifySignature([]byte(rawBody), signature) {
				logging.WithComponent("pilot").Warn("Asana webhook signature verification failed")
				return
			}

			// Parse the map payload into WebhookPayload
			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				logging.WithComponent("pilot").Error("Failed to marshal Asana payload", slog.Any("error", err))
				return
			}

			var webhookPayload asana.WebhookPayload
			if err := json.Unmarshal(payloadBytes, &webhookPayload); err != nil {
				logging.WithComponent("pilot").Error("Failed to parse Asana webhook payload", slog.Any("error", err))
				return
			}

			if err := p.asanaWH.Handle(ctx, &webhookPayload); err != nil {
				logging.WithComponent("pilot").Error("Asana webhook error", slog.Any("error", err))
			}
		})
	}

	// GH-2044: Register Plane webhook handler
	if p.planeWH != nil {
		p.gateway.Router().RegisterWebhookHandler("plane", func(payload map[string]interface{}) {
			// Plane handler needs raw bytes + signature for HMAC verification
			rawBody, _ := payload["_raw_body"].(string)
			signature, _ := payload["_signature"].(string)

			if err := p.planeWH.Handle(ctx, []byte(rawBody), signature); err != nil {
				logging.WithComponent("pilot").Error("Plane webhook error", slog.Any("error", err))
			}
		})
	}

	// Register Slack interaction webhook handler for approval buttons
	if p.slackInteractionWH != nil {
		p.gateway.RegisterHandler("/webhooks/slack/interactions", p.slackInteractionWH)
		logging.WithComponent("pilot").Info("registered Slack interaction webhook handler",
			slog.String("path", "/webhooks/slack/interactions"))
	}

	// Apply functional options (GH-349)
	for _, opt := range opts {
		opt(p)
	}

	// Set embedded dashboard frontend on gateway if available (GH-1612)
	if p.dashboardFS != nil {
		p.gateway.SetDashboardFS(p.dashboardFS)
	}

	// Initialize Telegram handler if runner was provided via options (GH-349)
	// This enables Telegram polling in gateway mode alongside Linear/Jira webhooks
	if p.telegramRunner != nil && cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled && cfg.Adapters.Telegram.Polling {
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

	// Initialize Slack handler if runner was provided via options (GH-652)
	// This enables Slack Socket Mode in gateway mode alongside other adapters
	if p.slackRunner != nil && cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled && cfg.Adapters.Slack.SocketMode {
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

	return p, nil
}
