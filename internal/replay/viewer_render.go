package replay

import (
	"fmt"
	"strings"
)

// View implements tea.Model
func (m *ViewerModel) View() string {
	if m.quit {
		return ""
	}

	if m.showHelp {
		return m.renderHelp()
	}

	var sb strings.Builder

	// Header
	header := m.renderHeader()
	sb.WriteString(header)
	sb.WriteString("\n")

	// Events
	visibleLines := m.height - 8
	if visibleLines < 5 {
		visibleLines = 5
	}

	start := m.scrollY
	end := start + visibleLines
	if end > len(m.filteredIdx) {
		end = len(m.filteredIdx)
	}

	for i := start; i < end; i++ {
		eventIdx := m.filteredIdx[i]
		event := m.events[eventIdx]

		isCurrent := i == m.current
		line := m.renderEvent(event, eventIdx, isCurrent)
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	// Pad remaining lines
	for i := end - start; i < visibleLines; i++ {
		sb.WriteString("\n")
	}

	// Footer
	footer := m.renderFooter()
	sb.WriteString(footer)

	return sb.String()
}

func (m *ViewerModel) renderHeader() string {
	var sb strings.Builder

	// Title bar
	title := fmt.Sprintf(" ▶ %s ", m.recording.ID)
	taskInfo := fmt.Sprintf(" Task: %s ", m.recording.TaskID)

	titleStyled := headerStyle.Render(title)
	taskStyled := statusStyle.Render(taskInfo)

	sb.WriteString(titleStyled)
	sb.WriteString(" ")
	sb.WriteString(taskStyled)
	sb.WriteString("\n")

	// Progress bar
	if len(m.filteredIdx) > 0 {
		progress := float64(m.current+1) / float64(len(m.filteredIdx))
		barWidth := m.width - 30
		if barWidth < 10 {
			barWidth = 10
		}
		filled := int(progress * float64(barWidth))
		bar := strings.Repeat("━", filled) + strings.Repeat("─", barWidth-filled)

		playState := "⏸"
		if m.playing {
			playState = "▶"
		}

		speedStr := fmt.Sprintf("%.1fx", m.speed)
		progressStr := fmt.Sprintf("%s %s [%d/%d] %s",
			playState,
			progressStyle.Render(bar),
			m.current+1,
			len(m.filteredIdx),
			speedStr,
		)
		sb.WriteString(progressStr)
	}
	sb.WriteString("\n")

	// Separator
	sb.WriteString(strings.Repeat("─", m.width))
	sb.WriteString("\n")

	return sb.String()
}

func (m *ViewerModel) renderEvent(event *StreamEvent, idx int, isCurrent bool) string {
	var sb strings.Builder

	// Timestamp and sequence
	ts := event.Timestamp.Format("15:04:05")
	prefix := fmt.Sprintf("%s #%-4d ", timestampStyle.Render(ts), event.Sequence)

	if isCurrent {
		prefix = "▶ " + prefix
	} else {
		prefix = "  " + prefix
	}

	sb.WriteString(prefix)

	// Event content
	content := m.formatEventContent(event)

	style := eventStyle
	if isCurrent {
		style = currentEventStyle
	}
	if event.Parsed != nil && event.Parsed.IsError {
		style = errorStyle
	}

	// Truncate to fit width
	maxLen := m.width - len(prefix) - 2
	if maxLen < 10 {
		maxLen = 10
	}
	if len(content) > maxLen {
		content = content[:maxLen-3] + "..."
	}

	sb.WriteString(style.Render(content))

	return sb.String()
}

func (m *ViewerModel) formatEventContent(event *StreamEvent) string {
	if event.Parsed == nil {
		return fmt.Sprintf("(%s)", event.Type)
	}

	p := event.Parsed

	switch p.Type {
	case "system":
		if p.Subtype == "init" {
			return "🚀 System initialized"
		}
		return fmt.Sprintf("⚙️ System: %s", p.Subtype)

	case "assistant":
		if p.ToolName != "" {
			icon := getToolIcon(p.ToolName)
			detail := formatToolDetail(p)
			if detail != "" {
				return fmt.Sprintf("%s %s: %s", icon, p.ToolName, detail)
			}
			return fmt.Sprintf("%s %s", icon, p.ToolName)
		}
		if p.Text != "" {
			text := strings.ReplaceAll(p.Text, "\n", " ")
			text = strings.TrimSpace(text)
			return fmt.Sprintf("💬 %s", text)
		}
		return "📝 (assistant)"

	case "user":
		return "📥 Tool result"

	case "result":
		if p.IsError {
			return fmt.Sprintf("❌ Error: %s", truncate(p.Result, 60))
		}
		tokens := ""
		if p.InputTokens > 0 || p.OutputTokens > 0 {
			tokens = fmt.Sprintf(" (%d in, %d out)", p.InputTokens, p.OutputTokens)
		}
		return fmt.Sprintf("✅ Completed%s", tokens)

	default:
		return fmt.Sprintf("(%s)", p.Type)
	}
}

func (m *ViewerModel) renderFooter() string {
	var sb strings.Builder

	// Separator
	sb.WriteString(strings.Repeat("─", m.width))
	sb.WriteString("\n")

	// Filter status
	filters := []string{}
	if m.filter.ShowTools {
		filters = append(filters, "Tools")
	}
	if m.filter.ShowText {
		filters = append(filters, "Text")
	}
	if m.filter.ShowResults {
		filters = append(filters, "Results")
	}
	if m.filter.ShowSystem {
		filters = append(filters, "System")
	}
	if m.filter.ShowErrors {
		filters = append(filters, "Errors")
	}

	filterStr := fmt.Sprintf("Showing: %s", strings.Join(filters, ", "))
	sb.WriteString(statusStyle.Render(filterStr))
	sb.WriteString("\n")

	// Help hints
	help := "Space: Play/Pause │ ←→: Navigate │ 1-4: Speed │ t/x/r/s/e: Filter │ ?: Help │ q: Quit"
	sb.WriteString(helpStyle.Render(help))

	return sb.String()
}

func (m *ViewerModel) renderHelp() string {
	var sb strings.Builder

	sb.WriteString(headerStyle.Render(" Interactive Replay Viewer - Help "))
	sb.WriteString("\n\n")

	help := `
  NAVIGATION
  ─────────────────────────────────────
  Space, p      Play/Pause playback
  n, Enter, ↓   Next event
  N, ↑          Previous event
  g             Go to start
  G             Go to end
  PgUp/PgDn     Jump 10 events

  SPEED CONTROL
  ─────────────────────────────────────
  1             0.5x speed (slow)
  2             1.0x speed (normal)
  3             2.0x speed (fast)
  4             4.0x speed (fastest)

  FILTERS
  ─────────────────────────────────────
  t             Toggle tool calls
  x             Toggle text/assistant
  r             Toggle results
  s             Toggle system events
  e             Toggle errors
  a             Show all events

  OTHER
  ─────────────────────────────────────
  ?, h          Toggle this help
  q, Ctrl+C     Quit viewer
`
	sb.WriteString(help)
	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render("Press any key to close help..."))

	return sb.String()
}
