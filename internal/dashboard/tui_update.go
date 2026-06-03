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
			m.banner.show = !m.banner.show
			return m, tea.ClearScreen
		case "l":
			m.showLogs = !m.showLogs
			return m, tea.ClearScreen // GH-1249: Logs toggle changes height
		case "f":
			m.showFindings = !m.showFindings
			return m, tea.ClearScreen // Findings toggle changes height
		case "g":
			// Toggle git graph: Hidden ↔ Visible (auto-sizes)
			if m.gitGraph.mode == GitGraphHidden {
				m.gitGraph.mode = GitGraphVisible
			} else {
				m.gitGraph.mode = GitGraphHidden
			}
			m.gitGraph.focus = false
			if m.gitGraph.mode != GitGraphHidden {
				// Start refresh and 15s tick when becoming visible
				return m, tea.Batch(
					refreshGitGraphCmd(m.gitGraph.projectPath),
					gitRefreshTickCmd(),
					tea.ClearScreen,
				)
			}
			return m, tea.ClearScreen
		case "tab":
			if m.gitGraph.mode != GitGraphHidden {
				m.gitGraph.focus = !m.gitGraph.focus
			}
		case "up", "k":
			if m.gitGraph.focus {
				if m.gitGraph.scroll > 0 {
					m.gitGraph.scroll--
				}
			} else if m.selectedTask > 0 {
				m.selectedTask--
				if cmd := m.syncGitGraphToSelectedTask(); cmd != nil {
					return m, cmd
				}
			}
		case "down", "j":
			if m.gitGraph.focus {
				if m.gitGraph.state != nil {
					viewportH := m.gitGraphViewportHeight()
					maxScroll := len(m.gitGraph.state.Lines) - viewportH
					if maxScroll < 0 {
						maxScroll = 0
					}
					if m.gitGraph.scroll < maxScroll {
						m.gitGraph.scroll++
					}
				}
			} else if m.selectedTask < len(m.tasks)-1 {
				m.selectedTask++
				if cmd := m.syncGitGraphToSelectedTask(); cmd != nil {
					return m, cmd
				}
			}
		case "ctrl+d":
			if m.gitGraph.focus && m.gitGraph.state != nil {
				viewportH := m.gitGraphViewportHeight()
				m.gitGraph.scroll += viewportH / 2
				maxScroll := len(m.gitGraph.state.Lines) - viewportH
				if maxScroll < 0 {
					maxScroll = 0
				}
				if m.gitGraph.scroll > maxScroll {
					m.gitGraph.scroll = maxScroll
				}
			}
		case "ctrl+u":
			if m.gitGraph.focus {
				viewportH := m.gitGraphViewportHeight()
				m.gitGraph.scroll -= viewportH / 2
				if m.gitGraph.scroll < 0 {
					m.gitGraph.scroll = 0
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
			if m.upgrade.info != nil && m.upgrade.state == UpgradeStateAvailable && m.upgrade.ch != nil {
				m.upgrade.state = UpgradeStateInProgress
				m.upgrade.progress = 0
				m.upgrade.message = "Starting upgrade..."
				// Non-blocking send to upgrade channel
				select {
				case m.upgrade.ch <- struct{}{}:
				default:
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, tea.ClearScreen // GH-1249: Terminal resized → full repaint

	case tickMsg:
		m.metrics.sparklineTick = !m.metrics.sparklineTick
		m.metrics.shimmerTick++
		m.gitGraph.dbSyncTick++
		if m.autopilotPanel != nil {
			m.autopilotPanel.SetTick(m.autopilotPanel.tick + 1)
		}
		// GH-2248: Re-sync history and metrics from SQLite every 5 seconds
		// so external DB changes (orphan cleanup, manual edits) are reflected.
		if m.store != nil && m.gitGraph.dbSyncTick%5 == 0 {
			return m, tea.Batch(tickCmd(), storeRefreshCmd(m.store))
		}
		return m, tickCmd()

	case splashTickMsg:
		if !m.splash.active {
			return m, nil
		}
		if m.splash.start.IsZero() {
			m.splash.start = time.Time(msg)
		}
		m.splash.frame++
		if m.splash.frame >= splashFramesTotal {
			m.splash.active = false
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

	case updateFindingsMsg:
		prevLen := len(m.findings)
		m.findings = msg
		// GH-1249 pattern: a changed finding count alters panel height, so
		// force a full repaint to avoid ghost lines from the diff renderer.
		if len(m.findings) != prevLen {
			return m, tea.ClearScreen
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
		m.metrics.card.InputTokens += inputDelta
		m.metrics.card.OutputTokens += outputDelta
		m.metrics.card.TotalTokens += inputDelta + outputDelta
		costModel := msg.Model
		if costModel == "" {
			costModel = memory.DefaultModel
		}
		m.metrics.card.TotalCostUSD += memory.EstimateCost(
			int64(inputDelta),
			int64(outputDelta),
			costModel,
		)
		if m.metrics.card.TotalTasks > 0 {
			m.metrics.card.CostPerTask = m.metrics.card.TotalCostUSD / float64(m.metrics.card.TotalTasks)
		}

	case addCompletedTaskMsg:
		prevLen := len(m.completedTasks)
		m.completedTasks = append(m.completedTasks, CompletedTask(msg))
		if len(m.completedTasks) > 5 {
			m.completedTasks = m.completedTasks[len(m.completedTasks)-5:]
		}

		// Update metrics card task counters
		m.metrics.card.TotalTasks++
		if CompletedTask(msg).Status == "success" {
			m.metrics.card.Succeeded++
		} else {
			m.metrics.card.Failed++
		}
		if m.metrics.card.TotalTasks > 0 {
			m.metrics.card.CostPerTask = m.metrics.card.TotalCostUSD / float64(m.metrics.card.TotalTasks)
		}

		// GH-1249: History count changed → force repaint
		if len(m.completedTasks) != prevLen {
			return m, tea.ClearScreen
		}

	case updateMetricsCardMsg:
		m.metrics.card = MetricsCardData(msg)

	case storeRefreshMsg:
		// GH-2248: Replace in-memory history and metrics with live DB state.
		prevLen := len(m.completedTasks)
		m.completedTasks = msg.completedTasks
		m.metrics.card = msg.metricsCard
		m.loadMetricsHistory()
		if len(m.completedTasks) != prevLen {
			return m, tea.ClearScreen
		}

	case updateAvailableMsg:
		m.upgrade.info = &UpdateInfo{
			CurrentVersion: msg.CurrentVersion,
			LatestVersion:  msg.LatestVersion,
			ReleaseNotes:   msg.ReleaseNotes,
		}
		m.upgrade.state = UpgradeStateAvailable
		return m, tea.ClearScreen // GH-1249: New panel added

	case upgradeProgressMsg:
		m.upgrade.progress = msg.Progress
		m.upgrade.message = msg.Message

	case upgradeCompleteMsg:
		if msg.Success {
			m.upgrade.state = UpgradeStateComplete
			m.upgrade.message = "Upgrade complete! Restart Pilot to apply."
		} else {
			m.upgrade.state = UpgradeStateFailed
			m.upgrade.err = msg.Error
			m.upgrade.message = "Upgrade failed"
		}

	case gitRefreshMsg:
		m.gitGraph.state = msg.state
		// Re-arm the 15-second refresh tick if panel is still visible
		if m.gitGraph.mode != GitGraphHidden {
			return m, gitRefreshTickCmd()
		}

	case gitRefreshTickMsg:
		// Only refresh when visible to save resources
		if m.gitGraph.mode != GitGraphHidden {
			return m, refreshGitGraphCmd(m.gitGraph.projectPath)
		}
	}

	return m, nil
}
