package dashboard

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// renderLogs renders the logs section
func (m Model) renderLogs() string {
	var content strings.Builder
	tw := m.effectivePanelTotalWidth()
	iw := tw - 4
	w := iw - 4 // Account for indent (2 spaces each side)

	if len(m.logs) == 0 {
		content.WriteString("  No logs yet")
	} else {
		start := len(m.logs) - 10
		if start < 0 {
			start = 0
		}

		for i, log := range m.logs[start:] {
			if i > 0 {
				content.WriteString("\n")
			}
			content.WriteString("  " + truncateVisual(log, w))
		}
	}

	return renderPanel("LOGS", content.String(), tw)
}

// UpdateMetricsCard sends updated metrics card data to the TUI
func UpdateMetricsCard(data MetricsCardData) tea.Cmd {
	return func() tea.Msg {
		return updateMetricsCardMsg(data)
	}
}

// UpdateTasks sends updated tasks to the TUI
func UpdateTasks(tasks []TaskDisplay) tea.Cmd {
	return func() tea.Msg {
		return updateTasksMsg(tasks)
	}
}

// AddLog sends a log entry to the TUI
func AddLog(log string) tea.Cmd {
	return func() tea.Msg {
		return addLogMsg(log)
	}
}

// UpdateTokens sends updated token usage to the TUI.
// model is the model name that produced the tokens; may be empty before the first
// stream event, in which case the handler falls back to memory.DefaultModel.
func UpdateTokens(input, output int, model string) tea.Cmd {
	return func() tea.Msg {
		return updateTokensMsg(TokenUsage{
			InputTokens:  input,
			OutputTokens: output,
			TotalTokens:  input + output,
			Model:        model,
		})
	}
}

// AddCompletedTask sends a completed task to the TUI history.
// parentID is the parent issue ID for sub-issues (empty string if none).
// isEpic indicates whether the task was decomposed into sub-issues.
func AddCompletedTask(id, title, status, duration string, parentID string, isEpic bool) tea.Cmd {
	return func() tea.Msg {
		return addCompletedTaskMsg(CompletedTask{
			ID:          id,
			Title:       title,
			Status:      status,
			Duration:    duration,
			CompletedAt: time.Now(),
			ParentID:    parentID,
			IsEpic:      isEpic,
		})
	}
}

// renderUpdateNotification renders the update notification panel
func (m Model) renderUpdateNotification() string {
	var content strings.Builder
	var title string
	var hint string
	tw := m.effectivePanelTotalWidth()
	iw := tw - 4

	switch m.upgradeState {
	case UpgradeStateAvailable:
		title = "^ UPDATE"
		// Left: version info, Right: will be hint below panel
		leftText := fmt.Sprintf("%s -> %s available", m.updateInfo.CurrentVersion, m.updateInfo.LatestVersion)
		rightText := ""
		content.WriteString(formatPanelRow(leftText, rightText, iw))
		hint = "u: upgrade"

	case UpgradeStateInProgress:
		title = "* UPGRADING"
		bar := m.renderProgressBar(m.upgradeProgress, 30)
		content.WriteString(fmt.Sprintf("  Installing %s... %s %d%%", m.updateInfo.LatestVersion, bar, m.upgradeProgress))
		if m.upgradeMessage != "" {
			content.WriteString("\n  " + m.upgradeMessage)
		}

	case UpgradeStateComplete:
		title = "+ UPGRADED"
		content.WriteString(fmt.Sprintf("  Upgrade to %s installed — restart Pilot manually to apply.", m.updateInfo.LatestVersion))

	case UpgradeStateFailed:
		title = "! UPGRADE FAILED"
		if m.upgradeError != "" {
			content.WriteString("  " + m.upgradeError)
		} else {
			content.WriteString("  " + m.upgradeMessage)
		}

	default:
		return ""
	}

	result := renderOrangePanel(title, content.String(), tw)
	if hint != "" {
		// Right-align hint under panel
		hintLine := fmt.Sprintf("%*s", tw, hint)
		result += "\n" + dimStyle.Render(hintLine)
	}
	return result
}

// formatPanelRow creates a full-width row with left and right aligned text
func formatPanelRow(left, right string, iw int) string {
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	padding := iw - leftWidth - rightWidth - 4 // 4 for indent
	if padding < 1 {
		padding = 1
	}
	return fmt.Sprintf("  %s%s%s", left, strings.Repeat(" ", padding), right)
}

// SetUpgradeChannel sets the channel used to trigger upgrades
func (m *Model) SetUpgradeChannel(ch chan<- struct{}) {
	m.upgradeCh = ch
}

// NotifyUpdateAvailable sends an update available message to the TUI
func NotifyUpdateAvailable(current, latest, releaseNotes string) tea.Cmd {
	return func() tea.Msg {
		return updateAvailableMsg{
			CurrentVersion: current,
			LatestVersion:  latest,
			ReleaseNotes:   releaseNotes,
		}
	}
}

// NotifyUpgradeProgress sends an upgrade progress update to the TUI
func NotifyUpgradeProgress(progress int, message string) tea.Cmd {
	return func() tea.Msg {
		return upgradeProgressMsg{
			Progress: progress,
			Message:  message,
		}
	}
}

// NotifyUpgradeComplete sends an upgrade completion message to the TUI
func NotifyUpgradeComplete(success bool, err string) tea.Cmd {
	return func() tea.Msg {
		return upgradeCompleteMsg{
			Success: success,
			Error:   err,
		}
	}
}

// Run starts the TUI with the given version
func Run(version string) error {
	p := tea.NewProgram(
		NewModel(version),
		tea.WithAltScreen(),
	)

	_, err := p.Run()
	return err
}

// openBrowser opens the specified URL in the default browser
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return cmd.Start()
}
