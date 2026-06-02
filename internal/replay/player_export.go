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

	sb.WriteString("<!DOCTYPE html>\n<html>\n<head>\n")
	sb.WriteString("<meta charset=\"UTF-8\">\n")
	sb.WriteString(fmt.Sprintf("<title>Execution Recording: %s</title>\n", recording.ID))
	sb.WriteString("<style>\n")
	sb.WriteString(`
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; margin: 40px; background: #1a1a2e; color: #eee; }
.header { background: #16213e; padding: 20px; border-radius: 8px; margin-bottom: 20px; }
.header h1 { margin: 0 0 10px 0; color: #0f4c75; }
.meta { display: flex; gap: 20px; flex-wrap: wrap; }
.meta-item { background: #0f3460; padding: 8px 16px; border-radius: 4px; }
.meta-label { color: #888; font-size: 12px; }
.meta-value { font-weight: bold; }
.event { padding: 12px 16px; border-left: 3px solid #333; margin: 8px 0; background: #16213e; border-radius: 0 4px 4px 0; }
.event:hover { background: #1a1a3e; }
.event-tool { border-left-color: #4a9eff; }
.event-text { border-left-color: #50c878; }
.event-result { border-left-color: #ffd700; }
.event-error { border-left-color: #ff4444; background: #2a1a1a; }
.timestamp { color: #666; font-size: 12px; margin-right: 10px; }
.sequence { color: #888; font-size: 11px; }
.tool-name { color: #4a9eff; font-weight: bold; }
.tool-detail { color: #aaa; margin-left: 8px; }
pre { background: #0a0a1a; padding: 12px; border-radius: 4px; overflow-x: auto; font-size: 13px; }
.section { margin: 24px 0; }
.section h2 { color: #0f4c75; border-bottom: 1px solid #333; padding-bottom: 8px; }
`)
	sb.WriteString("</style>\n</head>\n<body>\n")

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

	sb.WriteString("</body>\n</html>")

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
