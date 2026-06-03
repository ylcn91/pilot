package bitbucket

import (
	"context"
	"fmt"
	"strings"
)

// Notifier handles status updates to Bitbucket issues.
//
// Bitbucket Cloud's issue tracker has no free-form label collection, so status
// is communicated via comments and issue state transitions rather than labels.
type Notifier struct {
	client     *Client
	pilotLabel string
}

// NewNotifier creates a new Bitbucket notifier
func NewNotifier(client *Client, pilotLabel string) *Notifier {
	return &Notifier{
		client:     client,
		pilotLabel: pilotLabel,
	}
}

// NotifyTaskStarted posts a "started" comment on the issue
func (n *Notifier) NotifyTaskStarted(ctx context.Context, issueID int, taskID string) error {
	comment := fmt.Sprintf("🤖 **Pilot started working on this issue**\n\nTask ID: `%s`\n\nI'll post updates as I make progress.", taskID)
	if _, err := n.client.AddIssueComment(ctx, issueID, comment); err != nil {
		return fmt.Errorf("failed to add start comment: %w", err)
	}
	return nil
}

// NotifyProgress posts a progress update comment
func (n *Notifier) NotifyProgress(ctx context.Context, issueID int, phase string, details string) error {
	var emoji string
	switch strings.ToLower(phase) {
	case "exploring", "research":
		emoji = "🔍"
	case "implementing", "impl":
		emoji = "🔨"
	case "testing", "verify":
		emoji = "🧪"
	case "committing":
		emoji = "📝"
	default:
		emoji = "⏳"
	}

	comment := fmt.Sprintf("%s **Phase: %s**\n\n%s", emoji, phase, details)
	if _, err := n.client.AddIssueComment(ctx, issueID, comment); err != nil {
		return fmt.Errorf("failed to add progress comment: %w", err)
	}
	return nil
}

// NotifyTaskCompleted posts a completion comment and resolves the issue
func (n *Notifier) NotifyTaskCompleted(ctx context.Context, issueID int, prURL string, summary string) error {
	var comment strings.Builder
	comment.WriteString("✅ **Pilot completed this task!**\n\n")

	if prURL != "" {
		comment.WriteString(fmt.Sprintf("**Pull Request**: %s\n\n", prURL))
	}

	if summary != "" {
		comment.WriteString("**Summary**:\n")
		comment.WriteString(summary)
		comment.WriteString("\n\n")
	}

	comment.WriteString("_This issue will be resolved when the PR is merged._")

	if _, err := n.client.AddIssueComment(ctx, issueID, comment.String()); err != nil {
		return fmt.Errorf("failed to add completion comment: %w", err)
	}
	return nil
}

// NotifyTaskFailed posts a failure comment on the issue
func (n *Notifier) NotifyTaskFailed(ctx context.Context, issueID int, reason string) error {
	comment := fmt.Sprintf("❌ **Pilot could not complete this task**\n\n**Reason**: %s\n\n_Please review the issue and consider manual intervention or reopening with more details._", reason)
	if _, err := n.client.AddIssueComment(ctx, issueID, comment); err != nil {
		return fmt.Errorf("failed to add failure comment: %w", err)
	}
	return nil
}

// LinkPR adds a comment linking the created PR
func (n *Notifier) LinkPR(ctx context.Context, issueID int, prID int, prURL string) error {
	comment := fmt.Sprintf("🔗 **Pull Request Created**: #%d\n\n%s\n\n_This PR implements the changes for this issue._", prID, prURL)
	if _, err := n.client.AddIssueComment(ctx, issueID, comment); err != nil {
		return fmt.Errorf("failed to add PR link comment: %w", err)
	}
	return nil
}
