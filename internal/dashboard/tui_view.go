package dashboard

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View renders the TUI
func (m Model) View() string {
	if m.quitting {
		return "Pilot stopped.\n"
	}

	if m.splashActive {
		return m.renderSplash()
	}

	dashboard := m.renderDashboard()

	var result string
	if m.gitGraphMode == GitGraphHidden {
		result = dashboard
	} else if m.width > 0 && m.width < panelTotalWidth+1+20 {
		// Terminal too narrow for side-by-side — stack graph below at full terminal width.
		dashLines := strings.Count(dashboard, "\n") + 1
		graphHeight := m.height - dashLines - 1 // -1 for help footer
		if graphHeight < 8 {
			graphHeight = 8 // minimum useful graph height
		}
		graphPanel := m.renderGitGraph(m.width, graphHeight)
		if graphPanel == "" {
			result = dashboard
		} else {
			result = dashboard + "\n" + graphPanel
		}
	} else {
		graphPanel := m.renderGitGraph()
		if graphPanel == "" {
			result = dashboard
		} else {
			result = lipgloss.JoinHorizontal(lipgloss.Top, dashboard, " ", graphPanel)
		}
	}

	// Help footer — appended after height truncation so it's never cut off.
	helpLine := m.renderHelp()

	// GH-1249: Pad or truncate output to terminal height to prevent ghost lines.
	// Reserve the last line for the help footer so it's always visible.
	if m.height > 1 {
		contentHeight := m.height - 1 // reserve 1 line for help footer
		lines := strings.Split(result, "\n")
		if len(lines) < contentHeight {
			for len(lines) < contentHeight {
				lines = append(lines, "")
			}
		} else if len(lines) > contentHeight {
			lines = lines[:contentHeight]
		}
		lines = append(lines, helpLine)
		result = strings.Join(lines, "\n")
	} else if m.height == 1 {
		result = helpLine
	} else {
		// height unknown — just append help
		result += "\n" + helpLine
	}

	return result
}

// renderDashboard builds the left-side dashboard column (all existing panels).
func (m Model) renderDashboard() string {
	var b strings.Builder

	// Set effective panel width on autopilot panel for stacked mode
	if m.autopilotPanel != nil {
		m.autopilotPanel.panelWidth = m.effectivePanelTotalWidth()
	}

	// Header: bordered banner frame (GH-2455 / GH-2459).
	// The ASCII logo is shown only during the splash; steady-state dashboard
	// uses the compact banner frame to keep header real-estate small.
	if m.showBanner {
		b.WriteString(m.renderBanner())
		b.WriteString("\n")
	}

	// Update notification (if available) — always visible regardless of banner
	if m.updateInfo != nil {
		b.WriteString(m.renderUpdateNotification())
		b.WriteString("\n")
	}

	// Metrics cards (tokens, cost, tasks)
	b.WriteString(m.renderMetricsCards())
	b.WriteString("\n")

	// Tasks
	b.WriteString(m.renderTasks())
	b.WriteString("\n")

	// Autopilot panel
	b.WriteString(m.autopilotPanel.View())
	b.WriteString("\n")

	// Findings panel (Architect findings: Radar / Dependency-Doctor). Shown
	// only when there are findings to surface, mirroring the update panel.
	if m.showFindings && len(m.findings) > 0 {
		b.WriteString(m.renderFindings())
		b.WriteString("\n")
	}

	// History
	b.WriteString(m.renderHistory())
	b.WriteString("\n")

	// Logs (if enabled)
	if m.showLogs {
		b.WriteString(m.renderLogs())
		b.WriteString("\n")
	}

	// Help footer rendered separately in View() to survive height truncation

	return b.String()
}

// renderHelp returns a context-aware help footer that fits within panelTotalWidth (69 chars).
// Keys shown depend on gitGraphMode and gitGraphFocus state.
func (m Model) renderHelp() string {
	var parts []string
	switch {
	case m.gitGraphMode == GitGraphHidden:
		// Graph hidden: show navigation and graph-open key
		parts = []string{"q: quit", "l: logs", "f: findings", "b: banner", "g: graph", "j/k: select"}
	case m.gitGraphFocus:
		// Graph visible, graph panel focused
		parts = []string{"q: quit", "b: banner", "g: close", "tab: dashboard"}
	default:
		// Graph visible, dashboard focused
		parts = []string{"q: quit", "b: banner", "g: close", "tab: graph"}
	}
	help := strings.Join(parts, "  ")
	tw := m.effectivePanelTotalWidth()
	if len(help) > tw {
		help = help[:tw-3] + "..."
	}
	return helpStyle.Render(help)
}
