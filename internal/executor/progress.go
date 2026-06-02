package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Styles for progress display
var (
	phaseStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#10B981"))

	progressBarFilled = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7C3AED"))

	progressBarEmpty = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#374151"))

	logStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EF4444"))

	// Navigator-specific styles
	navigatorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F59E0B")).
			Bold(true)

	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#818CF8"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280"))

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E5E7EB"))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#10B981")).
			Bold(true)

	costStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FBBF24"))
)

// PhaseTimestamp records when a phase started
type PhaseTimestamp struct {
	Phase     string
	StartTime time.Time
	EndTime   time.Time
}

// ProgressDisplay handles real-time progress rendering
type ProgressDisplay struct {
	taskID       string
	taskTitle    string
	phase        string
	progress     int
	logs         []string
	startTime    time.Time
	mu           sync.Mutex
	maxLogs      int
	enabled      bool
	hasNavigator bool
	navMode      string // "nav-task", "nav-loop", etc.
	// Phase tracking for end report
	phaseHistory []PhaseTimestamp
	currentPhase *PhaseTimestamp
	// Files changed tracking
	filesChanged []string
}

// NewProgressDisplay creates a new progress display
func NewProgressDisplay(taskID, taskTitle string, enabled bool) *ProgressDisplay {
	return &ProgressDisplay{
		taskID:       taskID,
		taskTitle:    taskTitle,
		phase:        "Starting",
		progress:     0,
		logs:         []string{},
		startTime:    time.Now(),
		maxLogs:      5,
		enabled:      enabled,
		phaseHistory: []PhaseTimestamp{},
		filesChanged: []string{},
	}
}

// SetNavigator marks this execution as using Navigator
func (p *ProgressDisplay) SetNavigator(detected bool, mode string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hasNavigator = detected
	p.navMode = mode
}

// AddFileChanged records a file that was modified
func (p *ProgressDisplay) AddFileChanged(filePath string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Avoid duplicates
	for _, f := range p.filesChanged {
		if f == filePath {
			return
		}
	}
	p.filesChanged = append(p.filesChanged, filePath)
}

// Update updates the progress and re-renders
func (p *ProgressDisplay) Update(phase string, progress int, message string) {
	if !p.enabled {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Track phase transitions for timing
	if phase != p.phase {
		now := time.Now()
		// End current phase
		if p.currentPhase != nil {
			p.currentPhase.EndTime = now
			p.phaseHistory = append(p.phaseHistory, *p.currentPhase)
		}
		// Start new phase
		p.currentPhase = &PhaseTimestamp{
			Phase:     phase,
			StartTime: now,
		}
	}

	p.phase = phase
	// Enforce monotonic progress (never go backwards)
	if progress >= p.progress {
		p.progress = progress
	}

	if message != "" {
		timestamp := time.Now().Format("15:04:05")
		logEntry := fmt.Sprintf("[%s] %s", timestamp, message)
		p.logs = append(p.logs, logEntry)
		if len(p.logs) > p.maxLogs {
			p.logs = p.logs[1:]
		}
	}

	p.render()
}

// render outputs the progress display
func (p *ProgressDisplay) render() {
	// Clear previous output (move up and clear lines)
	// Navigator header (if present) + 1 task line + maxLogs log lines + 1 blank
	linesToClear := p.maxLogs + 2
	if p.hasNavigator {
		linesToClear++ // Extra line for Navigator indicator
	}
	for i := 0; i < linesToClear; i++ {
		fmt.Print("\033[A\033[K") // Move up and clear line
	}

	// Navigator indicator line (if detected)
	if p.hasNavigator {
		navIndicator := navigatorStyle.Render("🧭 Navigator")
		mode := p.navMode
		if mode == "" {
			mode = "active"
		}
		fmt.Printf("   %s: %s\n", navIndicator, dimStyle.Render(mode))
	}

	// Task line with progress bar
	duration := time.Since(p.startTime).Round(time.Second)
	progressBar := p.renderProgressBar()

	// Show Navigator phase prefix if applicable
	phaseDisplay := p.phase
	if p.hasNavigator && isNavigatorPhase(p.phase) {
		phaseDisplay = fmt.Sprintf("PHASE: %s", p.phase)
	}

	taskLine := fmt.Sprintf("   %s %s %s %3d%% %s",
		phaseStyle.Render(fmt.Sprintf("%-16s", phaseDisplay)),
		progressBar,
		p.taskID,
		p.progress,
		logStyle.Render(duration.String()),
	)
	fmt.Println(taskLine)

	// Log lines
	fmt.Println()
	for _, log := range p.logs {
		fmt.Printf("   %s\n", logStyle.Render(log))
	}
	// Pad remaining log lines
	for i := len(p.logs); i < p.maxLogs; i++ {
		fmt.Println()
	}
}

// isNavigatorPhase checks if a phase is a Navigator-specific phase
func isNavigatorPhase(phase string) bool {
	navPhases := []string{"Research", "Implement", "Verify", "Init", "Complete", "Loop Mode", "Task Mode"}
	for _, np := range navPhases {
		if strings.EqualFold(phase, np) {
			return true
		}
	}
	return false
}

// renderProgressBar creates a visual progress bar
func (p *ProgressDisplay) renderProgressBar() string {
	width := 20
	filled := p.progress * width / 100
	empty := width - filled

	bar := progressBarFilled.Render(strings.Repeat("█", filled)) +
		progressBarEmpty.Render(strings.Repeat("░", empty))

	return "[" + bar + "]"
}

// Start prints initial state and reserves space
func (p *ProgressDisplay) Start() {
	if !p.enabled {
		return
	}

	// Print initial lines to reserve space
	if p.hasNavigator {
		fmt.Println() // Navigator indicator placeholder
	}
	fmt.Println() // Task line placeholder
	fmt.Println() // Blank line
	for i := 0; i < p.maxLogs; i++ {
		fmt.Println() // Log line placeholders
	}

	// Render initial state
	p.render()
}

// StartWithNavigatorCheck prints Navigator detection status before progress
func (p *ProgressDisplay) StartWithNavigatorCheck(projectPath string) {
	if !p.enabled {
		return
	}

	// Check for .agent/ directory
	agentDir := filepath.Join(projectPath, ".agent")
	if isDir, err := osStatFunc(agentDir); err == nil && isDir {
		p.hasNavigator = true
		fmt.Printf("🧭 %s: %s (.agent/ exists)\n",
			navigatorStyle.Render("Navigator"),
			successStyle.Render("✓ detected"))
		fmt.Printf("   %s: %s\n",
			dimStyle.Render("Mode"),
			valueStyle.Render("awaiting skill activation"))
	} else {
		fmt.Printf("⚠️  %s: %s (running raw Claude Code)\n",
			navigatorStyle.Render("Navigator"),
			dimStyle.Render("not found"))
	}
	fmt.Println()

	// Continue with normal start
	p.Start()
}

// osStatFunc allows mocking os.Stat in tests
var osStatFunc = defaultOsStat

func defaultOsStat(name string) (bool, error) {
	fi, err := os.Stat(name)
	if err != nil {
		return false, err
	}
	return fi.IsDir(), nil
}
