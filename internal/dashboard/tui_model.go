package dashboard

import (
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ylcn91/pilot/internal/autopilot"
	"github.com/ylcn91/pilot/internal/memory"
)

// isStackedMode returns true when the git graph is visible and the terminal is
// too narrow for side-by-side layout, so the graph stacks below the dashboard.
func (m Model) isStackedMode() bool {
	if m.gitGraphMode == GitGraphHidden || m.width <= 0 {
		return false
	}
	// Minimum for side-by-side: dashboard + gap + smallest useful graph (20)
	return m.width < panelTotalWidth+1+20
}

// effectivePanelTotalWidth returns the panel width for the current layout.
// In stacked mode with a wider terminal, panels stretch to fill terminal width.
func (m Model) effectivePanelTotalWidth() int {
	if m.isStackedMode() && m.width > panelTotalWidth {
		return m.width
	}
	return panelTotalWidth
}

// NewModel creates a new dashboard model
func NewModel(version string) Model {
	return Model{
		tasks:          []TaskDisplay{},
		logs:           []string{},
		showLogs:       true,
		showBanner:     true,
		completedTasks: []CompletedTask{},
		costPerMToken:  3.0,
		autopilotPanel: NewAutopilotPanel(nil), // Disabled by default
		version:        version,
	}
}

// NewModelWithStore creates a dashboard model with SQLite persistence.
// Hydrates token usage and task history from the store on startup.
func NewModelWithStore(version string, store *memory.Store) Model {
	m := Model{
		tasks:          []TaskDisplay{},
		logs:           []string{},
		showLogs:       true,
		showBanner:     true,
		completedTasks: []CompletedTask{},
		costPerMToken:  3.0,
		autopilotPanel: NewAutopilotPanel(nil),
		version:        version,
		store:          store,
	}
	m.hydrateFromStore()
	return m
}

// NewModelWithAutopilot creates a dashboard model with autopilot integration.
func NewModelWithAutopilot(version string, controller *autopilot.Controller) Model {
	return Model{
		tasks:          []TaskDisplay{},
		logs:           []string{},
		showLogs:       true,
		showBanner:     true,
		completedTasks: []CompletedTask{},
		costPerMToken:  3.0,
		autopilotPanel: NewAutopilotPanel(controller),
		version:        version,
	}
}

// NewModelWithStoreAndAutopilot creates a fully-featured dashboard model.
func NewModelWithStoreAndAutopilot(version string, store *memory.Store, controller *autopilot.Controller) Model {
	m := Model{
		tasks:          []TaskDisplay{},
		logs:           []string{},
		showLogs:       true,
		showBanner:     true,
		completedTasks: []CompletedTask{},
		costPerMToken:  3.0,
		autopilotPanel: NewAutopilotPanel(controller),
		version:        version,
		store:          store,
	}
	m.hydrateFromStore()
	return m
}

// NewModelWithOptions creates a dashboard model with all options including upgrade support.
func NewModelWithOptions(version string, store *memory.Store, controller *autopilot.Controller, upgradeCh chan<- struct{}) Model {
	m := Model{
		tasks:          []TaskDisplay{},
		logs:           []string{},
		showLogs:       true,
		showBanner:     true,
		completedTasks: []CompletedTask{},
		costPerMToken:  3.0,
		autopilotPanel: NewAutopilotPanel(controller),
		version:        version,
		store:          store,
		upgradeCh:      upgradeCh,
	}
	m.hydrateFromStore()
	return m
}

// SetProjectPath sets the working directory used for git graph commands.
// The first call also sets the default fallback path (GH-2167).
func (m *Model) SetProjectPath(path string) {
	m.projectPath = path
	if m.defaultProjectPath == "" {
		m.defaultProjectPath = path
	}
}

// RenderBannerForTest exposes renderBanner for cross-package tests
// (cmd/pilot verifies applyDashboardBannerMeta wiring end-to-end).
func (m Model) RenderBannerForTest() string { return m.renderBanner() }

// SetBannerMeta configures optional metadata shown in the dashboard banner (GH-2455).
// Adapters provided here are all rendered as Active=true (legacy contract).
// New callers should use SetBannerAdapters for richer state (active vs configured).
func (m *Model) SetBannerMeta(envName, modelStack string, adapters []string, startTime time.Time) {
	m.envName = envName
	m.modelStack = modelStack
	m.activeAdapters = adapters
	// Mirror into bannerAdapters with Active=true so renderBanner has a single source.
	m.bannerAdapters = make([]AdapterStatus, 0, len(adapters))
	for _, a := range adapters {
		m.bannerAdapters = append(m.bannerAdapters, AdapterStatus{Name: a, Active: true})
	}
	if startTime.IsZero() {
		m.startTime = time.Now()
	} else {
		m.startTime = startTime
	}
}

// EnableSplash turns on the in-program splash overlay shown for the first
// ~1.5s after the dashboard starts. configPath is displayed in the boot
// block (e.g. "~/.pilot/config.yaml").
func (m *Model) EnableSplash(configPath string) {
	m.splashActive = true
	m.configPath = configPath
}

// SetBannerAdapters replaces the adapter status list shown in the banner.
// Pass an entry with Active=false for adapters that are configured but not
// running this session; omit entries entirely for adapters with no config.
func (m *Model) SetBannerAdapters(adapters []AdapterStatus) {
	m.bannerAdapters = adapters
	// Mirror Active-true names into legacy field for any consumers still reading it.
	names := make([]string, 0, len(adapters))
	for _, a := range adapters {
		if a.Active {
			names = append(names, a.Name)
		}
	}
	m.activeAdapters = names
}

// syncGitGraphToSelectedTask updates projectPath to match the selected task's project.
// Returns a tea.Cmd to refresh the git graph if the project changed, nil otherwise.
// Falls back to defaultProjectPath when no task is selected or the task has no project. (GH-2167)
func (m *Model) syncGitGraphToSelectedTask() tea.Cmd {
	if m.gitGraphMode == GitGraphHidden {
		return nil
	}

	newPath := m.defaultProjectPath
	newName := ""

	if m.selectedTask >= 0 && m.selectedTask < len(m.tasks) {
		task := m.tasks[m.selectedTask]
		if task.ProjectPath != "" {
			newPath = task.ProjectPath
			newName = task.ProjectName
			if newName == "" {
				newName = filepath.Base(newPath)
			}
		}
	}

	if newPath == m.projectPath {
		// Project unchanged — just update display name if needed
		m.gitProjectName = newName
		return nil
	}

	m.projectPath = newPath
	m.gitProjectName = newName
	m.gitGraphScroll = 0
	return refreshGitGraphCmd(m.projectPath)
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd(), tea.EnterAltScreen}
	if m.splashActive {
		cmds = append(cmds, splashTickCmd())
	}
	return tea.Batch(cmds...)
}

// tickCmd creates a tick command
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// splashTickCmd schedules the next splash frame (~150ms cadence).
func splashTickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
		return splashTickMsg(t)
	})
}

// splashFramesTotal: number of frames the splash plays before dismissal.
// 10 frames * 150ms = 1.5s.
const splashFramesTotal = 10
