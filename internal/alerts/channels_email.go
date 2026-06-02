package alerts

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// EmailChannel sends alerts via email
type EmailChannel struct {
	name    string
	sender  EmailSender
	to      []string
	subject string // Optional template
}

// EmailSender interface for sending emails
type EmailSender interface {
	Send(ctx context.Context, to []string, subject, htmlBody string) error
}

// NewEmailChannel creates a new email alert channel
func NewEmailChannel(name string, sender EmailSender, config *EmailChannelConfig) *EmailChannel {
	return &EmailChannel{
		name:    name,
		sender:  sender,
		to:      config.To,
		subject: config.Subject,
	}
}

func (c *EmailChannel) Name() string { return c.name }
func (c *EmailChannel) Type() string { return "email" }

func (c *EmailChannel) Send(ctx context.Context, alert *Alert) error {
	subject := c.formatSubject(alert)
	body := c.formatBody(alert)

	return c.sender.Send(ctx, c.to, subject, body)
}

func (c *EmailChannel) formatSubject(alert *Alert) string {
	if c.subject != "" {
		// Simple template replacement
		s := c.subject
		s = strings.ReplaceAll(s, "{{severity}}", string(alert.Severity))
		s = strings.ReplaceAll(s, "{{type}}", string(alert.Type))
		s = strings.ReplaceAll(s, "{{title}}", alert.Title)
		return s
	}

	emoji := "🔔"
	switch alert.Severity {
	case SeverityCritical:
		emoji = "🚨"
	case SeverityWarning:
		emoji = "⚠️"
	}

	return fmt.Sprintf("%s [%s] Pilot Alert: %s", emoji, strings.ToUpper(string(alert.Severity)), alert.Title)
}

func (c *EmailChannel) formatBody(alert *Alert) string {
	var sb strings.Builder

	sb.WriteString(`<!DOCTYPE html>
<html>
<head>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; }
.container { max-width: 600px; margin: 0 auto; padding: 20px; }
.alert-box { border-radius: 8px; padding: 20px; margin-bottom: 20px; }
.critical { background: #fee2e2; border-left: 4px solid #dc2626; }
.warning { background: #fef3c7; border-left: 4px solid #f59e0b; }
.info { background: #dbeafe; border-left: 4px solid #3b82f6; }
.title { font-size: 18px; font-weight: 600; margin: 0 0 10px 0; }
.message { margin: 0 0 15px 0; }
.metadata { font-size: 14px; color: #666; }
.metadata dt { font-weight: 600; display: inline; }
.metadata dd { display: inline; margin: 0 15px 0 5px; }
.footer { margin-top: 20px; padding-top: 20px; border-top: 1px solid #eee; font-size: 12px; color: #999; }
code { background: #f3f4f6; padding: 2px 6px; border-radius: 4px; font-family: monospace; }
</style>
</head>
<body>
<div class="container">
`)

	cssClass := "info"
	switch alert.Severity {
	case SeverityCritical:
		cssClass = "critical"
	case SeverityWarning:
		cssClass = "warning"
	}

	sb.WriteString(fmt.Sprintf(`<div class="alert-box %s">`, cssClass))
	sb.WriteString(fmt.Sprintf(`<h2 class="title">%s</h2>`, alert.Title))
	sb.WriteString(fmt.Sprintf(`<p class="message">%s</p>`, alert.Message))

	sb.WriteString(`<dl class="metadata">`)
	sb.WriteString(fmt.Sprintf(`<dt>Type:</dt><dd><code>%s</code></dd>`, alert.Type))

	if alert.Source != "" {
		sb.WriteString(fmt.Sprintf(`<dt>Source:</dt><dd><code>%s</code></dd>`, alert.Source))
	}

	if alert.ProjectPath != "" {
		sb.WriteString(fmt.Sprintf(`<dt>Project:</dt><dd><code>%s</code></dd>`, alert.ProjectPath))
	}

	sb.WriteString(fmt.Sprintf(`<dt>Severity:</dt><dd>%s</dd>`, strings.ToUpper(string(alert.Severity))))
	sb.WriteString(`</dl>`)
	sb.WriteString(`</div>`)

	sb.WriteString(fmt.Sprintf(`<div class="footer">Alert ID: %s<br>Generated at %s</div>`,
		alert.ID, alert.CreatedAt.Format(time.RFC1123)))

	sb.WriteString(`</div></body></html>`)

	return sb.String()
}
