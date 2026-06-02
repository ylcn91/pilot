package dashboard

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ylcn91/pilot/internal/memory"
)

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "b":
			m.showBanner = !m.showBanner
			return m, tea.ClearScreen
		case "l":
			m.showLogs = !m.showLogs
			return m, tea.ClearScreen // GH-1249: Logs toggle changes height
		case "g":
			// Toggle git graph: Hidden ↔ Visible (auto-sizes)
			if m.gitGraphMode == GitGraphHidden {
				m.gitGraphMode = GitGraphVisible
			} else {
				m.gitGraphMode = GitGraphHidden
			}
			m.gitGraphFocus = false
			if m.gitGraphMode != GitGraphHidden {
				// Start refresh and 15s tick when becoming visible
				return m, tea.Batch(
					refreshGitGraphCmd(m.projectPath),
					gitRefreshTickCmd(),
					tea.ClearScreen,
				)
			}
			return m, tea.ClearScreen
		case "tab":
			if m.gitGraphMode != GitGraphHidden {
				m.gitGraphFocus = !m.gitGraphFocus
			}
		case "up", "k":
			if m.gitGraphFocus {
				if m.gitGraphScroll > 0 {
					m.gitGraphScroll--
				}
			} else if m.selectedTask > 0 {
				m.selectedTask--
				if cmd := m.syncGitGraphToSelectedTask(); cmd != nil {
					return m, cmd
				}
			}
		case "down", "j":
			if m.gitGraphFocus {
				if m.gitGraphState != nil {
					viewportH := m.gitGraphViewportHeight()
					maxScroll := len(m.gitGraphState.Lines) - viewportH
					if maxScroll < 0 {
						maxScroll = 0
					}
					if m.gitGraphScroll < maxScroll {
						m.gitGraphScroll++
					}
				}
			} else if m.selectedTask < len(m.tasks)-1 {
				m.selectedTask++
				if cmd := m.syncGitGraphToSelectedTask(); cmd != nil {
					return m, cmd
				}
			}
		case "ctrl+d":
			if m.gitGraphFocus && m.gitGraphState != nil {
				viewportH := m.gitGraphViewportHeight()
				m.gitGraphScroll += viewportH / 2
				maxScroll := len(m.gitGraphState.Lines) - viewportH
				if maxScroll < 0 {
					maxScroll = 0
				}
				if m.gitGraphScroll > maxScroll {
					m.gitGraphScroll = maxScroll
				}
			}
		case "ctrl+u":
			if m.gitGraphFocus {
				viewportH := m.gitGraphViewportHeight()
				m.gitGraphScroll -= viewportH / 2
				if m.gitGraphScroll < 0 {
					m.gitGraphScroll = 0
				}
			}
		case "enter":
			if m.selectedTask >= 0 && m.selectedTask < len(m.tasks) {
				task := m.tasks[m.selectedTask]
				if task.IssueURL != "" {
					_ = openBrowser(task.IssueURL)
				}
			}
		case "u":
			// Trigger upgrade if update is available and not already upgrading
			if m.updateInfo != nil && m.upgradeState == UpgradeStateAvailable && m.upgradeCh != nil {
				m.upgradeState = UpgradeStateInProgress
				m.upgradeProgress = 0
				m.upgradeMessage = "Starting upgrade..."
				// Non-blocking send to upgrade channel
				select {
				case m.upgradeCh <- struct{}{}:
				default:
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, tea.ClearScreen // GH-1249: Terminal resized → full repaint

	case tickMsg:
		m.sparklineTick = !m.sparklineTick
		m.shimmerTick++
		m.dbSyncTick++
		if m.autopilotPanel != nil {
			m.autopilotPanel.SetTick(m.autopilotPanel.tick + 1)
		}
		// GH-2248: Re-sync history and metrics from SQLite every 5 seconds
		// so external DB changes (orphan cleanup, manual edits) are reflected.
		if m.store != nil && m.dbSyncTick%5 == 0 {
			return m, tea.Batch(tickCmd(), storeRefreshCmd(m.store))
		}
		return m, tickCmd()

	case splashTickMsg:
		if !m.splashActive {
			return m, nil
		}
		if m.splashStart.IsZero() {
			m.splashStart = time.Time(msg)
		}
		m.splashFrame++
		if m.splashFrame >= splashFramesTotal {
			m.splashActive = false
			return m, tea.ClearScreen
		}
		return m, splashTickCmd()

	case updateTasksMsg:
		prevLen := len(m.tasks)
		m.tasks = msg
		// GH-2167: Sync git graph to selected task's project when task list updates
		gitCmd := m.syncGitGraphToSelectedTask()
		if len(m.tasks) != prevLen {
			// GH-1249: Task count changed → content height changed.
			// Force full repaint to prevent ghost lines from Bubbletea's diff renderer.
			if gitCmd != nil {
				return m, tea.Batch(gitCmd, tea.ClearScreen)
			}
			return m, tea.ClearScreen
		}
		if gitCmd != nil {
			return m, gitCmd
		}

	case addLogMsg:
		m.logs = append(m.logs, string(msg))
		if len(m.logs) > 100 {
			m.logs = m.logs[1:]
		}

	case updateTokensMsg:
		// Calculate delta and persist to session
		inputDelta := msg.InputTokens - m.tokenUsage.InputTokens
		outputDelta := msg.OutputTokens - m.tokenUsage.OutputTokens
		m.tokenUsage = TokenUsage(msg)
		m.persistTokenUsage(inputDelta, outputDelta)

		// Add deltas to lifetime metrics card totals (not replace with session values)
		m.metricsCard.InputTokens += inputDelta
		m.metricsCard.OutputTokens += outputDelta
		m.metricsCard.TotalTokens += inputDelta + outputDelta
		costModel := msg.Model
		if costModel == "" {
			costModel = memory.DefaultModel
		}
		m.metricsCard.TotalCostUSD += memory.EstimateCost(
			int64(inputDelta),
			int64(outputDelta),
			costModel,
		)
		if m.metricsCard.TotalTasks > 0 {
			m.metricsCard.CostPerTask = m.metricsCard.TotalCostUSD / float64(m.metricsCard.TotalTasks)
		}

	case addCompletedTaskMsg:
		prevLen := len(m.completedTasks)
		m.completedTasks = append(m.completedTasks, CompletedTask(msg))
		if len(m.completedTasks) > 5 {
			m.completedTasks = m.completedTasks[len(m.completedTasks)-5:]
		}

		// Update metrics card task counters
		m.metricsCard.TotalTasks++
		if CompletedTask(msg).Status == "success" {
			m.metricsCard.Succeeded++
		} else {
			m.metricsCard.Failed++
		}
		if m.metricsCard.TotalTasks > 0 {
			m.metricsCard.CostPerTask = m.metricsCard.TotalCostUSD / float64(m.metricsCard.TotalTasks)
		}

		// GH-1249: History count changed → force repaint
		if len(m.completedTasks) != prevLen {
			return m, tea.ClearScreen
		}

	case updateMetricsCardMsg:
		m.metricsCard = MetricsCardData(msg)

	case storeRefreshMsg:
		// GH-2248: Replace in-memory history and metrics with live DB state.
		prevLen := len(m.completedTasks)
		m.completedTasks = msg.completedTasks
		m.metricsCard = msg.metricsCard
		m.loadMetricsHistory()
		if len(m.completedTasks) != prevLen {
			return m, tea.ClearScreen
		}

	case updateAvailableMsg:
		m.updateInfo = &UpdateInfo{
			CurrentVersion: msg.CurrentVersion,
			LatestVersion:  msg.LatestVersion,
			ReleaseNotes:   msg.ReleaseNotes,
		}
		m.upgradeState = UpgradeStateAvailable
		return m, tea.ClearScreen // GH-1249: New panel added

	case upgradeProgressMsg:
		m.upgradeProgress = msg.Progress
		m.upgradeMessage = msg.Message

	case upgradeCompleteMsg:
		if msg.Success {
			m.upgradeState = UpgradeStateComplete
			m.upgradeMessage = "Upgrade complete! Restart Pilot to apply."
		} else {
			m.upgradeState = UpgradeStateFailed
			m.upgradeError = msg.Error
			m.upgradeMessage = "Upgrade failed"
		}

	case gitRefreshMsg:
		m.gitGraphState = msg.state
		// Re-arm the 15-second refresh tick if panel is still visible
		if m.gitGraphMode != GitGraphHidden {
			return m, gitRefreshTickCmd()
		}

	case gitRefreshTickMsg:
		// Only refresh when visible to save resources
		if m.gitGraphMode != GitGraphHidden {
			return m, refreshGitGraphCmd(m.projectPath)
		}
	}

	return m, nil
}
