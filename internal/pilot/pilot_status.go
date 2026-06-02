package pilot

import (
	"context"
	"fmt"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/gateway"
	"github.com/ylcn91/pilot/internal/teams"
	"github.com/ylcn91/pilot/internal/webhooks"
)

// GetStatus returns current Pilot status
func (p *Pilot) GetStatus() map[string]interface{} {
	webhookDeliveries, webhookFailures, webhookRetries, lastDelivery := p.webhookManager.Stats()

	// Build Linear status (GH-391: multi-workspace support)
	linearStatus := p.config.Adapters.Linear != nil && p.config.Adapters.Linear.Enabled
	var linearWorkspaces []string
	if p.linearMultiWH != nil {
		linearWorkspaces = p.linearMultiWH.ListWorkspaces()
	}

	return map[string]interface{}{
		"running": true,
		"tasks":   p.orchestrator.GetTaskStates(),
		"config": map[string]interface{}{
			"gateway":           fmt.Sprintf("%s:%d", p.config.Gateway.Host, p.config.Gateway.Port),
			"linear":            linearStatus,
			"linear_workspaces": linearWorkspaces,
			"github":            p.config.Adapters.GitHub != nil && p.config.Adapters.GitHub.Enabled,
			"gitlab":            p.config.Adapters.GitLab != nil && p.config.Adapters.GitLab.Enabled,
			"slack":             p.config.Adapters.Slack != nil && p.config.Adapters.Slack.Enabled,
			"webhooks":          p.webhookManager.IsEnabled(),
		},
		"webhooks": map[string]interface{}{
			"enabled":       p.webhookManager.IsEnabled(),
			"endpoints":     len(p.webhookManager.ListEndpoints()),
			"deliveries":    webhookDeliveries,
			"failures":      webhookFailures,
			"retries":       webhookRetries,
			"last_delivery": lastDelivery,
		},
	}
}

// WebhookManager returns the webhook manager for external access
func (p *Pilot) WebhookManager() *webhooks.Manager {
	return p.webhookManager
}

// DispatchWebhookEvent dispatches an event to all subscribed webhook endpoints
func (p *Pilot) DispatchWebhookEvent(ctx context.Context, event *webhooks.Event) []webhooks.DeliveryResult {
	return p.webhookManager.Dispatch(ctx, event)
}

// Router returns the gateway router for registering handlers
func (p *Pilot) Router() *gateway.Router {
	return p.gateway.Router()
}

// Gateway returns the gateway server for registering HTTP handlers
func (p *Pilot) Gateway() *gateway.Server {
	return p.gateway
}

// TeamsService returns the teams service for RBAC (GH-633)
// Returns nil if --team was not provided.
func (p *Pilot) TeamsService() *teams.Service {
	return p.teamsService
}

// OnProgress registers a callback for task progress updates
func (p *Pilot) OnProgress(callback func(taskID, phase string, progress int, message string)) {
	p.orchestrator.OnProgress(callback)
}

// OnToken registers a callback for token usage updates
func (p *Pilot) OnToken(name string, callback func(taskID string, inputTokens, outputTokens int64, modelName string)) {
	p.orchestrator.OnToken(name, callback)
}

// GetTaskStates returns current task states from the orchestrator
func (p *Pilot) GetTaskStates() []*executor.TaskState {
	return p.orchestrator.GetTaskStates()
}

// SuppressProgressLogs disables slog output for progress updates.
// Use this when a visual progress display is active to prevent log spam.
func (p *Pilot) SuppressProgressLogs(suppress bool) {
	p.orchestrator.SuppressProgressLogs(suppress)
}

// SetQualityCheckerFactory sets the factory for creating quality checkers.
// Quality gates run after task execution to validate code quality before PR creation.
// This allows main.go to wire the factory without creating import cycles.
func (p *Pilot) SetQualityCheckerFactory(factory executor.QualityCheckerFactory) {
	p.orchestrator.SetQualityCheckerFactory(factory)
}

// SetOnPRReview wires a PR review callback on the GitHub webhook handler.
// This allows cmd/pilot/main.go to route review events to the autopilot controller
// without creating import cycles.
func (p *Pilot) SetOnPRReview(callback github.PRReviewCallback) {
	if p.githubWH != nil {
		p.githubWH.OnPRReview(callback)
	}
}
