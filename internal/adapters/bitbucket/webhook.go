package bitbucket

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/ylcn91/pilot/internal/logging"
)

// WebhookHandler handles Bitbucket Cloud webhooks
type WebhookHandler struct {
	client        *Client
	webhookSecret string
	pilotLabel    string
	onIssue       func(context.Context, *Issue, *Repository) error
}

// NewWebhookHandler creates a new webhook handler
func NewWebhookHandler(client *Client, webhookSecret, pilotLabel string) *WebhookHandler {
	return &WebhookHandler{
		client:        client,
		webhookSecret: webhookSecret,
		pilotLabel:    pilotLabel,
	}
}

// OnIssue sets the callback for when a pilot-eligible issue is received
func (h *WebhookHandler) OnIssue(callback func(context.Context, *Issue, *Repository) error) {
	h.onIssue = callback
}

// VerifySignature verifies the Bitbucket webhook signature.
// Bitbucket Cloud signs webhook bodies with HMAC-SHA256 in the
// X-Hub-Signature header, formatted as "sha256=<hex>".
func (h *WebhookHandler) VerifySignature(payload []byte, signature string) bool {
	return VerifyWebhookSignature(payload, signature, h.webhookSecret)
}

// VerifyWebhookSignature verifies a Bitbucket webhook signature against a secret.
// Standalone variant for callers without a WebhookHandler instance.
func VerifyWebhookSignature(payload []byte, signature, secret string) bool {
	if secret == "" {
		// Fail-closed: no secret means reject unless the dev escape hatch is set.
		return os.Getenv("PILOT_ALLOW_UNSIGNED_WEBHOOKS") == "1"
	}

	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	expectedSig := signature[7:] // Remove "sha256=" prefix

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	actualSig := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expectedSig), []byte(actualSig))
}

// Handle processes a webhook payload identified by the X-Event-Key header.
func (h *WebhookHandler) Handle(ctx context.Context, eventKey string, payload *WebhookPayload) error {
	logging.WithComponent("bitbucket").Debug("Bitbucket webhook",
		slog.String("event", eventKey))

	switch eventKey {
	case WebhookEventIssueCreated, WebhookEventIssueUpdated:
		return h.handleIssue(ctx, payload)
	case WebhookEventPRCreated, WebhookEventPRUpdated,
		WebhookEventPRMerged, WebhookEventPRDeclined:
		// PR events are observed for autopilot wiring (STEP 2); no issue action here.
		return nil
	default:
		return nil
	}
}

// handleIssue processes an issue webhook event
func (h *WebhookHandler) handleIssue(ctx context.Context, payload *WebhookPayload) error {
	if payload.Issue == nil {
		return nil
	}

	issue := payload.Issue
	issue.Labels = synthesizeLabels(issue)

	if !h.matchesPilotLabel(issue) {
		logging.WithComponent("bitbucket").Debug("Issue does not match pilot label, skipping",
			slog.Int("id", issue.ID))
		return nil
	}

	logging.WithComponent("bitbucket").Info("Processing pilot issue",
		slog.Int("id", issue.ID),
		slog.String("title", issue.Title))

	// Fetch full issue details via API (webhook payload may be incomplete).
	fullIssue, err := h.client.GetIssue(ctx, issue.ID)
	if err != nil {
		return fmt.Errorf("failed to fetch issue details: %w", err)
	}
	fullIssue.Labels = synthesizeLabels(fullIssue)

	if h.onIssue != nil {
		return h.onIssue(ctx, fullIssue, payload.Repository)
	}

	return nil
}

// matchesPilotLabel reports whether the issue's synthesized labels or kind
// match the configured pilot trigger label.
func (h *WebhookHandler) matchesPilotLabel(issue *Issue) bool {
	if issue.Kind == h.pilotLabel {
		return true
	}
	return HasLabel(issue, h.pilotLabel)
}
