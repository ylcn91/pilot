package pilot

import (
	"context"
	"fmt"

	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/config"
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

	memoryPath := config.MemoryPathOrDefault(cfg)

	// Initialize memory store
	store, err := memory.NewStore(memoryPath)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create memory store: %w", err)
	}
	p.store = store

	// Initialize knowledge graph
	graph, err := memory.NewKnowledgeGraph(memoryPath)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create knowledge graph: %w", err)
	}
	p.graph = graph

	// Initialize approval manager
	p.approvalMgr = approval.NewManager(cfg.Approval)

	// Initialize Slack notifier if enabled
	p.initSlackNotifier(cfg)

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
	if err := p.initLinearAdapter(cfg); err != nil {
		cancel()
		return nil, err
	}

	// Initialize GitHub/GitLab/Jira/Azure DevOps/Asana/Plane adapters if enabled
	p.initTrackerAdapters(cfg)

	// Initialize alerts engine if enabled
	if cfg.Alerts != nil && cfg.Alerts.Enabled {
		p.initAlerts(cfg)
	}

	// Initialize gateway with webhook secrets
	p.initGateway(cfg)

	// Register webhook handlers
	p.registerWebhookHandlers()

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
	p.initTelegramHandler(cfg)

	// Initialize Slack handler if runner was provided via options (GH-652)
	// This enables Slack Socket Mode in gateway mode alongside other adapters
	p.initSlackHandler(cfg)

	return p, nil
}
