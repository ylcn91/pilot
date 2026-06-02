package alerts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
)

// SlackChannel sends alerts to Slack
type SlackChannel struct {
	name    string
	client  *slack.Client
	channel string
}

// NewSlackChannel creates a new Slack alert channel
func NewSlackChannel(name string, client *slack.Client, channel string) *SlackChannel {
	return &SlackChannel{
		name:    name,
		client:  client,
		channel: channel,
	}
}

func (c *SlackChannel) Name() string { return c.name }
func (c *SlackChannel) Type() string { return "slack" }

func (c *SlackChannel) Send(ctx context.Context, alert *Alert) error {
	blocks := c.formatSlackBlocks(alert)

	msg := &slack.Message{
		Channel: c.channel,
		Blocks:  blocks,
		Attachments: []slack.Attachment{
			{
				Color: c.severityColor(alert.Severity),
			},
		},
	}

	_, err := c.client.PostMessage(ctx, msg)
	return err
}

func (c *SlackChannel) formatSlackBlocks(alert *Alert) []slack.Block {
	emoji := c.severityEmoji(alert.Severity)
	severityLabel := strings.ToUpper(string(alert.Severity))

	blocks := []slack.Block{
		{
			Type: "header",
			Text: &slack.TextObject{
				Type: "plain_text",
				Text: fmt.Sprintf("%s %s Alert", emoji, severityLabel),
			},
		},
		{
			Type: "section",
			Text: &slack.TextObject{
				Type: "mrkdwn",
				Text: fmt.Sprintf("*%s*\n%s", alert.Title, alert.Message),
			},
		},
	}

	// Add context block with metadata
	contextElements := []slack.TextObject{
		{
			Type: "mrkdwn",
			Text: fmt.Sprintf("*Type:* `%s`", alert.Type),
		},
	}

	if alert.Source != "" {
		contextElements = append(contextElements, slack.TextObject{
			Type: "mrkdwn",
			Text: fmt.Sprintf("*Source:* `%s`", alert.Source),
		})
	}

	if alert.ProjectPath != "" {
		contextElements = append(contextElements, slack.TextObject{
			Type: "mrkdwn",
			Text: fmt.Sprintf("*Project:* `%s`", alert.ProjectPath),
		})
	}

	blocks = append(blocks, slack.Block{
		Type:     "context",
		Elements: contextElements,
	})

	return blocks
}

func (c *SlackChannel) severityEmoji(severity Severity) string {
	switch severity {
	case SeverityCritical:
		return "🚨"
	case SeverityWarning:
		return "⚠️"
	default:
		return "ℹ️"
	}
}

func (c *SlackChannel) severityColor(severity Severity) string {
	switch severity {
	case SeverityCritical:
		return "danger"
	case SeverityWarning:
		return "warning"
	default:
		return "#0066cc"
	}
}

// TelegramChannel sends alerts to Telegram
type TelegramChannel struct {
	name   string
	client *telegram.Client
	chatID int64
}

// NewTelegramChannel creates a new Telegram alert channel
func NewTelegramChannel(name string, client *telegram.Client, chatID int64) *TelegramChannel {
	return &TelegramChannel{
		name:   name,
		client: client,
		chatID: chatID,
	}
}

func (c *TelegramChannel) Name() string { return c.name }
func (c *TelegramChannel) Type() string { return "telegram" }

func (c *TelegramChannel) Send(ctx context.Context, alert *Alert) error {
	text := c.formatMessage(alert)
	chatID := fmt.Sprintf("%d", c.chatID)
	// E4 (TASK-349): use MarkdownV2 to match escapeMarkdown, which escapes the
	// MarkdownV2 metacharacter set. Legacy "Markdown" left those escapes as literal
	// backslashes (e.g. "50\.00") or rejected the entities outright ("can't parse
	// entities"), silently dropping cost/failure alerts.
	_, err := c.client.SendMessage(ctx, chatID, text, "MarkdownV2")
	return err
}

func (c *TelegramChannel) formatMessage(alert *Alert) string {
	emoji := c.severityEmoji(alert.Severity)
	severityLabel := strings.ToUpper(string(alert.Severity))

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s *%s ALERT*\n\n", emoji, severityLabel))
	sb.WriteString(fmt.Sprintf("*%s*\n", escapeMarkdown(alert.Title)))
	sb.WriteString(fmt.Sprintf("%s\n\n", escapeMarkdown(alert.Message)))

	sb.WriteString(fmt.Sprintf("📋 *Type:* `%s`\n", alert.Type))

	if alert.Source != "" {
		sb.WriteString(fmt.Sprintf("🔗 *Source:* `%s`\n", alert.Source))
	}

	if alert.ProjectPath != "" {
		sb.WriteString(fmt.Sprintf("📁 *Project:* `%s`\n", alert.ProjectPath))
	}

	sb.WriteString(fmt.Sprintf("\n🕐 %s", alert.CreatedAt.Format(time.RFC822)))

	return sb.String()
}

func (c *TelegramChannel) severityEmoji(severity Severity) string {
	switch severity {
	case SeverityCritical:
		return "🚨"
	case SeverityWarning:
		return "⚠️"
	default:
		return "ℹ️"
	}
}

// escapeMarkdown escapes special characters for Telegram MarkdownV2
func escapeMarkdown(text string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(text)
}
