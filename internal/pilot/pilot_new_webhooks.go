package pilot

import (
	"encoding/json"
	"log/slog"
	"os"

	"github.com/ylcn91/pilot/internal/adapters/asana"
	"github.com/ylcn91/pilot/internal/adapters/azuredevops"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/gateway"
	"github.com/ylcn91/pilot/internal/logging"
)

// initGateway builds the gateway server with adapter webhook secrets folded in.
func (p *Pilot) initGateway(cfg *config.Config) {
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
}

// registerWebhookHandlers registers all adapter webhook handlers on the gateway router,
// in the same order as the original New flow. The handler closures capture p.ctx, which is
// identical to the ctx local used inline in New.
func (p *Pilot) registerWebhookHandlers() {
	ctx := p.ctx

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
}
