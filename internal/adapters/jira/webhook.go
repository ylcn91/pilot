package jira

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/text"
)

// WebhookEventType represents the type of webhook event
type WebhookEventType string

const (
	EventIssueCreated WebhookEventType = "jira:issue_created"
	EventIssueUpdated WebhookEventType = "jira:issue_updated"
	EventIssueDeleted WebhookEventType = "jira:issue_deleted"
	EventCommentAdded WebhookEventType = "comment_created"
)

// WebhookPayload represents a Jira webhook payload
type WebhookPayload struct {
	WebhookEvent string     `json:"webhookEvent"`
	Timestamp    int64      `json:"timestamp"`
	User         *User      `json:"user,omitempty"`
	Issue        *Issue     `json:"issue,omitempty"`
	Changelog    *Changelog `json:"changelog,omitempty"`
	Comment      *Comment   `json:"comment,omitempty"`
}

// Changelog represents changes in a webhook event
type Changelog struct {
	ID    string          `json:"id"`
	Items []ChangelogItem `json:"items"`
}

// ChangelogItem represents a single change in the changelog
type ChangelogItem struct {
	Field      string `json:"field"`
	FieldType  string `json:"fieldtype"`
	From       string `json:"from"`
	FromString string `json:"fromString"`
	To         string `json:"to"`
	ToString   string `json:"toString"`
}

// WebhookHandler handles Jira webhooks
type WebhookHandler struct {
	client        *Client
	webhookSecret string
	pilotLabel    string
	onIssue       func(context.Context, *Issue) error
}

// NewWebhookHandler creates a new webhook handler
func NewWebhookHandler(client *Client, webhookSecret, pilotLabel string) *WebhookHandler {
	return &WebhookHandler{
		client:        client,
		webhookSecret: webhookSecret,
		pilotLabel:    pilotLabel,
	}
}

// OnIssue sets the callback for when a pilot-labeled issue is received
func (h *WebhookHandler) OnIssue(callback func(context.Context, *Issue) error) {
	h.onIssue = callback
}

// VerifySignature verifies the Jira webhook signature
func (h *WebhookHandler) VerifySignature(payload []byte, signature string) bool {
	if h.webhookSecret == "" {
		// Fail-closed: no secret means reject unless the dev escape hatch is set.
		return os.Getenv("PILOT_ALLOW_UNSIGNED_WEBHOOKS") == "1"
	}

	mac := hmac.New(sha256.New, []byte(h.webhookSecret))
	mac.Write(payload)
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedSig))
}

// Handle processes a webhook payload
func (h *WebhookHandler) Handle(ctx context.Context, payload map[string]interface{}) error {
	webhookEvent, _ := payload["webhookEvent"].(string)

	logging.WithComponent("jira").Debug("Jira webhook", slog.String("event", webhookEvent))

	switch webhookEvent {
	case string(EventIssueCreated):
		return h.handleIssueCreated(ctx, payload)
	case string(EventIssueUpdated):
		return h.handleIssueUpdated(ctx, payload)
	default:
		logging.WithComponent("jira").Debug("Ignoring Jira event", slog.String("event", webhookEvent))
		return nil
	}
}

// handleIssueCreated processes newly created issues
func (h *WebhookHandler) handleIssueCreated(ctx context.Context, payload map[string]interface{}) error {
	issue, err := h.extractIssue(payload)
	if err != nil {
		return err
	}

	// Check if issue has pilot label
	if !h.hasPilotLabel(issue) {
		logging.WithComponent("jira").Debug("Issue does not have pilot label, skipping", slog.String("key", issue.Key))
		return nil
	}

	return h.processIssue(ctx, issue)
}

// handleIssueUpdated processes issue updates (check for label additions)
func (h *WebhookHandler) handleIssueUpdated(ctx context.Context, payload map[string]interface{}) error {
	// Check if the update added the pilot label
	if !h.wasLabelAdded(payload) {
		return nil
	}

	issue, err := h.extractIssue(payload)
	if err != nil {
		return err
	}

	// Verify the issue currently has the pilot label
	if !h.hasPilotLabel(issue) {
		logging.WithComponent("jira").Debug("Issue does not have pilot label, skipping", slog.String("key", issue.Key))
		return nil
	}

	return h.processIssue(ctx, issue)
}

// processIssue processes an issue that should be handled by Pilot
func (h *WebhookHandler) processIssue(ctx context.Context, issue *Issue) error {
	logging.WithComponent("jira").Info("Processing pilot issue", slog.String("key", issue.Key), slog.String("summary", issue.Fields.Summary))

	// Fetch full issue details via API (webhook payload may be incomplete)
	fullIssue, err := h.client.GetIssue(ctx, issue.Key)
	if err != nil {
		return fmt.Errorf("failed to fetch issue details: %w", err)
	}

	// Call the callback
	if h.onIssue != nil {
		return h.onIssue(ctx, fullIssue)
	}

	return nil
}

// webhookIssue mirrors Issue for webhook decoding. The only deviation is the
// description: Jira Server sends it as a plain string while Jira Cloud sends
// an Atlassian Document Format (ADF) object, so it is decoded as RawMessage
// and normalized to text afterwards.
type webhookIssue struct {
	ID     string        `json:"id"`
	Key    string        `json:"key"`
	Self   string        `json:"self"`
	Fields webhookFields `json:"fields"`
}

type webhookFields struct {
	Summary     string          `json:"summary"`
	Description json.RawMessage `json:"description"`
	IssueType   IssueType       `json:"issuetype"`
	Status      Status          `json:"status"`
	Priority    *JiraPriority   `json:"priority,omitempty"`
	Labels      []string        `json:"labels"`
	Assignee    *User           `json:"assignee,omitempty"`
	Reporter    *User           `json:"reporter,omitempty"`
	Project     Project         `json:"project"`
	Created     string          `json:"created"`
	Updated     string          `json:"updated"`
}

// extractIssue extracts issue data from the webhook payload
func (h *WebhookHandler) extractIssue(payload map[string]interface{}) (*Issue, error) {
	issueData, ok := payload["issue"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("missing issue in payload")
	}

	rawIssue, err := json.Marshal(issueData)
	if err != nil {
		return nil, err
	}

	var decoded webhookIssue
	if err := json.Unmarshal(rawIssue, &decoded); err != nil {
		return nil, err
	}

	issue := &Issue{
		ID:   decoded.ID,
		Key:  decoded.Key,
		Self: decoded.Self,
		Fields: Fields{
			IssueType: decoded.Fields.IssueType,
			Status:    decoded.Fields.Status,
			Priority:  decoded.Fields.Priority,
			Labels:    decoded.Fields.Labels,
			Assignee:  decoded.Fields.Assignee,
			Reporter:  decoded.Fields.Reporter,
			Project:   decoded.Fields.Project,
			Created:   decoded.Fields.Created,
			Updated:   decoded.Fields.Updated,
		},
	}

	// Untrusted text coming off the wire is sanitized before being stored on
	// the canonical Issue struct so that every downstream consumer (not just
	// ConvertIssueToTask) sees the cleaned form. This defends against
	// ASCII-smuggling prompt-injection via invisible Unicode format chars.
	var summaryStripped, descStripped int
	issue.Fields.Summary, summaryStripped = text.SanitizeUntrusted(decoded.Fields.Summary)
	if desc, ok := decodeDescription(decoded.Fields.Description, h); ok {
		issue.Fields.Description, descStripped = text.SanitizeUntrusted(desc)
	}
	if summaryStripped+descStripped > 0 {
		logging.WithComponent("jira").Warn(
			"invisible_unicode_stripped",
			slog.String("source", "jira-webhook"),
			slog.String("issue", issue.Key),
			slog.Int("summary_stripped", summaryStripped),
			slog.Int("description_stripped", descStripped),
		)
	}

	return issue, nil
}

// decodeDescription resolves a Jira webhook description that may be a plain
// string (Server) or an ADF object (Cloud). The second return value is false
// when the description is absent or null, matching the original behavior where
// neither type assertion fired and Description was left empty.
func decodeDescription(raw json.RawMessage, h *WebhookHandler) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString, true
	}

	var asADF map[string]interface{}
	if err := json.Unmarshal(raw, &asADF); err == nil {
		return h.extractADFText(asADF), true
	}

	return "", false
}

// extractADFText extracts plain text from Atlassian Document Format
func (h *WebhookHandler) extractADFText(adf map[string]interface{}) string {
	var sb strings.Builder
	h.extractADFTextRecursive(adf, &sb)
	return strings.TrimSpace(sb.String())
}

// extractADFTextRecursive recursively extracts text from ADF nodes
func (h *WebhookHandler) extractADFTextRecursive(node map[string]interface{}, sb *strings.Builder) {
	if text, ok := node["text"].(string); ok {
		sb.WriteString(text)
	}

	if content, ok := node["content"].([]interface{}); ok {
		for _, item := range content {
			if itemMap, ok := item.(map[string]interface{}); ok {
				h.extractADFTextRecursive(itemMap, sb)
			}
		}
		// Add newline for block elements
		if nodeType, ok := node["type"].(string); ok {
			if nodeType == "paragraph" || nodeType == "heading" || nodeType == "listItem" {
				sb.WriteString("\n")
			}
		}
	}
}

// hasPilotLabel checks if the issue has the pilot label
func (h *WebhookHandler) hasPilotLabel(issue *Issue) bool {
	for _, label := range issue.Fields.Labels {
		if strings.EqualFold(label, h.pilotLabel) {
			return true
		}
	}
	return false
}

// wasLabelAdded checks if the pilot label was added in this update
func (h *WebhookHandler) wasLabelAdded(payload map[string]interface{}) bool {
	changelog, ok := payload["changelog"].(map[string]interface{})
	if !ok {
		return false
	}

	items, ok := changelog["items"].([]interface{})
	if !ok {
		return false
	}

	for _, item := range items {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		field, _ := itemMap["field"].(string)
		if field != "labels" {
			continue
		}

		// Check if pilot label was added
		toString, _ := itemMap["toString"].(string)
		if strings.Contains(strings.ToLower(toString), strings.ToLower(h.pilotLabel)) {
			// Make sure it wasn't already there
			fromString, _ := itemMap["fromString"].(string)
			if !strings.Contains(strings.ToLower(fromString), strings.ToLower(h.pilotLabel)) {
				return true
			}
		}
	}

	return false
}
