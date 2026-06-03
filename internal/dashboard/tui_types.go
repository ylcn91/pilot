package dashboard

import (
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// Panel width (all panels same width)
const (
	panelTotalWidth = 69 // Total visual width including borders
	panelInnerWidth = 65 // panelTotalWidth - 4 (2 borders + 2 padding spaces)
)

// Metrics card dimensions
const (
	cardWidth      = 23 // 23*3 = 69 = panelTotalWidth (no gaps)
	cardInnerWidth = 17 // cardWidth - 6 (border + 2-char padding each side)
	cardGap        = 0  // no gap — cards fill full panel width
)

// sparkBlocks maps normalized levels (0-8) to Unicode block elements for sparkline rendering.
var sparkBlocks = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// MetricsCardData holds aggregated metrics for the dashboard metrics cards.
type MetricsCardData struct {
	TotalTokens, InputTokens, OutputTokens  int
	TotalCostUSD, CostPerTask               float64
	TotalTasks, Succeeded, Failed, Declined int
	// TASK-358: non-failure terminal outcomes, split out of "failed".
	NoOp, Stalled, RateLimited, Infra, Skipped int
	TokenHistory                               []int64   // 7 days
	CostHistory                                []float64 // 7 days
	TaskHistory                                []int     // 7 days
}

// Styles (muted terminal aesthetic)
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7eb8da")) // steel blue

	borderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3d4450")) // slate

	statusRunningStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7eb8da")) // steel blue

	statusPendingStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6e7681"))

	statusFailedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#d48a8a")) // dusty rose

	statusCompletedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7ec699")) // sage green

	statusQueuedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8b949e")) // mid gray

	statusDoneStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7ec699")) // sage green (same as completed)

	progressBarStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7eb8da")) // steel blue

	progressEmptyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#3d4450")) // slate

	progressBarDoneStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7ec699")) // sage green for done bars

	progressBarFailedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#d48a8a")) // dusty rose for failed bars

	shimmerDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3d4450")) // slate

	shimmerMidStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6e7681")) // between slate and mid gray

	shimmerBrightStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8b949e")) // mid gray

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8b949e"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8b949e"))

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#c9d1d9"))

	costStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7ec699")). // sage green
			Bold(true)

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d4a054")) // amber

	orangeBorderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#d4a054")) // amber

	orangeLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#d4a054")) // amber
)

// TaskDisplay represents a task for display
type TaskDisplay struct {
	ID          string
	Title       string
	Status      string
	Phase       string
	Progress    int
	Duration    string
	IssueURL    string
	PRURL       string
	ProjectPath string // Resolved project directory (GH-2167)
	ProjectName string // Short project name for git graph title (GH-2167)
}

// TokenUsage tracks token consumption
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	Model        string // model that produced these tokens; empty until first stream event
}

// CompletedTask represents a finished task for history
type CompletedTask struct {
	ID          string
	Title       string
	Status      string // "success" or "failed"
	Duration    string
	CompletedAt time.Time
	ParentID    string   // Parent issue ID for sub-issues (e.g. "GH-498")
	SubIssues   []string // Sub-issue IDs for epics (e.g. ["GH-501", "GH-502"])
	TotalSubs   int      // Total number of sub-issues (epic tracking)
	DoneSubs    int      // Number of completed sub-issues (epic tracking)
	IsEpic      bool     // Whether this task was decomposed into sub-issues
	// PeakRSSMB is the peak subprocess RSS in MiB from the RSS sampler. GH-3028.
	// Zero when the sampler had no data (pre-3028 executions, non-Linux/darwin).
	PeakRSSMB int
}

// UpdateInfo contains information about an available update
type UpdateInfo struct {
	CurrentVersion string
	LatestVersion  string
	ReleaseNotes   string
}

// UpgradeState tracks the current upgrade status
type UpgradeState int

const (
	UpgradeStateNone UpgradeState = iota
	UpgradeStateAvailable
	UpgradeStateInProgress
	UpgradeStateComplete
	UpgradeStateFailed
)

// Model is the TUI model
type Model struct {
	tasks          []TaskDisplay
	logs           []string
	width          int
	height         int
	showLogs       bool
	selectedTask   int
	quitting       bool
	tokenUsage     TokenUsage
	completedTasks []CompletedTask
	costPerMToken  float64
	autopilotPanel *AutopilotPanel
	// findings holds the latest Architect findings (Radar/Dependency-Doctor
	// push these via UpdateFindings); rendered by the FINDINGS panel.
	findings  []pilotapi.Finding
	version   string
	store     *memory.Store // SQLite persistence (GH-367)
	sessionID string        // Current session ID for persistence

	// Metrics cards
	metricsCard   MetricsCardData
	sparklineTick bool
	shimmerTick   int // Counter for queue shimmer animation (increments each tick)

	// Upgrade state
	updateInfo      *UpdateInfo
	upgradeState    UpgradeState
	upgradeProgress int
	upgradeMessage  string
	upgradeError    string
	upgradeCh       chan<- struct{} // Channel to trigger upgrade (write-only)

	// Banner toggle (GH-1520)
	showBanner bool

	// Findings panel toggle. When false the FINDINGS panel is hidden even if
	// findings are present.
	showFindings bool

	// Banner metadata (GH-2455 / GH-2459 rework): env name, model stack, adapter
	// status list.
	startTime      time.Time
	modelStack     string
	envName        string
	bannerAdapters []AdapterStatus
	// activeAdapters retained for backwards compatibility with SetBannerMeta callers.
	activeAdapters []string

	// Splash state — shown for the first ~1.5s of the session inside the same
	// tea.Program (avoids alt-screen flicker that a separate splash program caused).
	splashActive bool
	splashFrame  int       // increments each splashTickMsg
	splashStart  time.Time // first frame timestamp
	configPath   string    // shown in splash boot block ("~/.pilot/config.yaml")

	// Git graph panel (GH-1506)
	gitGraphMode       GitGraphMode
	gitGraphState      *GitGraphState
	gitGraphScroll     int
	gitGraphFocus      bool
	dbSyncTick         int    // Counter for periodic DB re-sync (GH-2248)
	projectPath        string // Working directory for git commands
	defaultProjectPath string // Fallback project path from config (GH-2167)
	gitProjectName     string // Current project name shown in git panel title (GH-2167)
}

// AdapterStatus describes a configured adapter for the banner status row.
// Active=true when the adapter was started this session (flag passed); false
// when it is configured but not running. Adapters absent from the slice are
// not rendered at all (not configured).
type AdapterStatus struct {
	Name   string
	Active bool
}

// tickMsg is sent periodically to refresh the display
type tickMsg time.Time

// updateTasksMsg updates the task list
type updateTasksMsg []TaskDisplay

// addLogMsg adds a log entry
type addLogMsg string

// updateTokensMsg updates token usage
type updateTokensMsg TokenUsage

// addCompletedTaskMsg adds a completed task to history
type addCompletedTaskMsg CompletedTask

// updateAvailableMsg signals that an update is available
type updateAvailableMsg UpdateInfo

// upgradeProgressMsg updates the upgrade progress
type upgradeProgressMsg struct {
	Progress int
	Message  string
}

// upgradeCompleteMsg signals upgrade completion
type upgradeCompleteMsg struct {
	Success bool
	Error   string
}

// storeRefreshMsg carries refreshed state from SQLite (GH-2248).
type storeRefreshMsg struct {
	completedTasks []CompletedTask
	metricsCard    MetricsCardData
}

// splashTickMsg drives the boot-screen lamp animation.
type splashTickMsg time.Time

// updateMetricsCardMsg updates the metrics card data
type updateMetricsCardMsg MetricsCardData
