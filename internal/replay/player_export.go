package replay

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ExportToHTML exports a recording to HTML format
func ExportToHTML(recording *Recording, events []*StreamEvent) (string, error) {
	var sb strings.Builder

	writeHTMLDocumentOpen(&sb, htmlDocument{
		htmlTag: "<html>",
		title:   fmt.Sprintf("Execution Recording: %s", recording.ID),
		styles:  recordingExportStyles(),
	})

	// Header
	sb.WriteString("<div class=\"header\">\n")
	sb.WriteString(fmt.Sprintf("<h1>📹 Recording: %s</h1>\n", recording.ID))
	sb.WriteString("<div class=\"meta\">\n")
	sb.WriteString(fmt.Sprintf("<div class=\"meta-item\"><div class=\"meta-label\">Task</div><div class=\"meta-value\">%s</div></div>\n", recording.TaskID))
	sb.WriteString(fmt.Sprintf("<div class=\"meta-item\"><div class=\"meta-label\">Status</div><div class=\"meta-value\">%s</div></div>\n", recording.Status))
	sb.WriteString(fmt.Sprintf("<div class=\"meta-item\"><div class=\"meta-label\">Duration</div><div class=\"meta-value\">%s</div></div>\n", recording.Duration.Round(time.Second)))
	sb.WriteString(fmt.Sprintf("<div class=\"meta-item\"><div class=\"meta-label\">Events</div><div class=\"meta-value\">%d</div></div>\n", recording.EventCount))
	if recording.TokenUsage != nil {
		sb.WriteString(fmt.Sprintf("<div class=\"meta-item\"><div class=\"meta-label\">Tokens</div><div class=\"meta-value\">%d</div></div>\n", recording.TokenUsage.TotalTokens))
		sb.WriteString(fmt.Sprintf("<div class=\"meta-item\"><div class=\"meta-label\">Cost</div><div class=\"meta-value\">$%.4f</div></div>\n", recording.TokenUsage.EstimatedCostUSD))
	}
	sb.WriteString("</div>\n</div>\n")

	// Events
	sb.WriteString("<div class=\"section\">\n<h2>Execution Events</h2>\n")
	for _, event := range events {
		class := "event"
		if event.Parsed != nil {
			if event.Parsed.IsError {
				class += " event-error"
			} else if event.Parsed.ToolName != "" {
				class += " event-tool"
			} else if event.Parsed.Text != "" {
				class += " event-text"
			} else if event.Parsed.Type == "result" {
				class += " event-result"
			}
		}

		sb.WriteString(fmt.Sprintf("<div class=\"%s\">\n", class))
		sb.WriteString(fmt.Sprintf("<span class=\"timestamp\">%s</span>", event.Timestamp.Format("15:04:05.000")))
		sb.WriteString(fmt.Sprintf("<span class=\"sequence\">#%d</span>\n", event.Sequence))

		if event.Parsed != nil {
			parsed := event.Parsed
			if parsed.ToolName != "" {
				sb.WriteString(fmt.Sprintf("<span class=\"tool-name\">%s</span>", parsed.ToolName))
				if detail := formatToolDetail(parsed); detail != "" {
					sb.WriteString(fmt.Sprintf("<span class=\"tool-detail\">%s</span>", escapeHTML(detail)))
				}
			} else if parsed.Text != "" {
				text := parsed.Text
				if len(text) > 200 {
					text = text[:200] + "..."
				}
				sb.WriteString(fmt.Sprintf("<div>%s</div>", escapeHTML(text)))
			} else if parsed.Type == "result" {
				if parsed.IsError {
					sb.WriteString(fmt.Sprintf("<div>❌ %s</div>", escapeHTML(truncate(parsed.Result, 200))))
				} else {
					sb.WriteString("<div>✅ Completed</div>")
				}
			}
		}

		sb.WriteString("</div>\n")
	}
	sb.WriteString("</div>\n")

	writeHTMLDocumentClose(&sb)

	return sb.String(), nil
}

// ExportToJSON exports a recording to JSON format
func ExportToJSON(recording *Recording, events []*StreamEvent) ([]byte, error) {
	export := struct {
		Recording *Recording     `json:"recording"`
		Events    []*StreamEvent `json:"events"`
	}{
		Recording: recording,
		Events:    events,
	}
	return json.MarshalIndent(export, "", "  ")
}
