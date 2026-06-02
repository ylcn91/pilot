package replay

import (
	"fmt"
	"strings"
)

// FormatEvent formats an event for display
func FormatEvent(event *StreamEvent, verbose bool) string {
	var sb strings.Builder

	// Timestamp and sequence
	ts := event.Timestamp.Format("15:04:05.000")
	sb.WriteString(fmt.Sprintf("[%s] #%d ", ts, event.Sequence))

	if event.Parsed == nil {
		sb.WriteString(fmt.Sprintf("(%s)", event.Type))
		return sb.String()
	}

	parsed := event.Parsed

	switch parsed.Type {
	case "system":
		if parsed.Subtype == "init" {
			sb.WriteString("🚀 System initialized")
		} else {
			sb.WriteString(fmt.Sprintf("⚙️  System: %s", parsed.Subtype))
		}

	case "assistant":
		if parsed.ToolName != "" {
			sb.WriteString(formatToolCall(parsed))
		} else if parsed.Text != "" {
			// Truncate long text
			text := parsed.Text
			if len(text) > 100 && !verbose {
				text = text[:97] + "..."
			}
			sb.WriteString(fmt.Sprintf("💬 %s", strings.TrimSpace(text)))
		}

	case "user":
		sb.WriteString("📥 Tool result")

	case "result":
		if parsed.IsError {
			sb.WriteString(fmt.Sprintf("❌ Error: %s", truncate(parsed.Result, 80)))
		} else {
			sb.WriteString("✅ Completed")
			if parsed.InputTokens > 0 || parsed.OutputTokens > 0 {
				sb.WriteString(fmt.Sprintf(" (tokens: %d in, %d out)", parsed.InputTokens, parsed.OutputTokens))
			}
		}

	default:
		sb.WriteString(fmt.Sprintf("(%s)", parsed.Type))
	}

	return sb.String()
}

// formatToolCall formats a tool call for display
func formatToolCall(parsed *ParsedEvent) string {
	tool := parsed.ToolName
	var detail string

	switch tool {
	case "Read":
		if fp, ok := parsed.ToolInput["file_path"].(string); ok {
			detail = shortenPath(fp)
		}
	case "Write":
		if fp, ok := parsed.ToolInput["file_path"].(string); ok {
			detail = shortenPath(fp)
		}
	case "Edit":
		if fp, ok := parsed.ToolInput["file_path"].(string); ok {
			detail = shortenPath(fp)
		}
	case "Bash":
		if cmd, ok := parsed.ToolInput["command"].(string); ok {
			detail = truncate(cmd, 50)
		}
	case "Glob":
		if pattern, ok := parsed.ToolInput["pattern"].(string); ok {
			detail = pattern
		}
	case "Grep":
		if pattern, ok := parsed.ToolInput["pattern"].(string); ok {
			detail = truncate(pattern, 30)
		}
	case "Task":
		if desc, ok := parsed.ToolInput["description"].(string); ok {
			detail = truncate(desc, 40)
		}
	case "Skill":
		if skill, ok := parsed.ToolInput["skill"].(string); ok {
			detail = skill
		}
	}

	icon := getToolIcon(tool)
	if detail != "" {
		return fmt.Sprintf("%s %s: %s", icon, tool, detail)
	}
	return fmt.Sprintf("%s %s", icon, tool)
}

// getToolIcon returns an emoji for the tool
func getToolIcon(tool string) string {
	switch tool {
	case "Read":
		return "📖"
	case "Write":
		return "✏️"
	case "Edit":
		return "📝"
	case "Bash":
		return "💻"
	case "Glob":
		return "🔍"
	case "Grep":
		return "🔎"
	case "Task":
		return "🤖"
	case "Skill":
		return "⚡"
	case "WebFetch":
		return "🌐"
	case "WebSearch":
		return "🔍"
	default:
		return "🔧"
	}
}

// shortenPath shortens a file path for display
func shortenPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) <= 3 {
		return path
	}
	return ".../" + strings.Join(parts[len(parts)-2:], "/")
}

// truncate truncates a string with ellipsis
func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// formatToolDetail formats tool input for display
func formatToolDetail(parsed *ParsedEvent) string {
	switch parsed.ToolName {
	case "Read", "Write", "Edit":
		if fp, ok := parsed.ToolInput["file_path"].(string); ok {
			return fp
		}
	case "Bash":
		if cmd, ok := parsed.ToolInput["command"].(string); ok {
			return truncate(cmd, 80)
		}
	case "Glob":
		if pattern, ok := parsed.ToolInput["pattern"].(string); ok {
			return pattern
		}
	case "Grep":
		if pattern, ok := parsed.ToolInput["pattern"].(string); ok {
			return truncate(pattern, 40)
		}
	}
	return ""
}

// escapeHTML escapes HTML special characters
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
