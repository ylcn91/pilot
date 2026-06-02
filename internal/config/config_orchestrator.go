package config

import (
	"time"

	"github.com/ylcn91/pilot/internal/adapters/asana"
	"github.com/ylcn91/pilot/internal/adapters/azuredevops"
	"github.com/ylcn91/pilot/internal/adapters/discord"
	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/gitlab"
	"github.com/ylcn91/pilot/internal/adapters/jira"
	"github.com/ylcn91/pilot/internal/adapters/linear"
	"github.com/ylcn91/pilot/internal/adapters/plane"
	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/autopilot"
)

// TeamConfig holds settings for team-based project access control (GH-635).
// When configured, task execution is scoped to the member's allowed projects.
type TeamConfig struct {
	Enabled     bool   `yaml:"enabled"`
	TeamID      string `yaml:"team_id"`      // Team ID or name to scope execution
	MemberEmail string `yaml:"member_email"` // Email of the member executing tasks
}

// AdaptersConfig holds configuration for external service adapters.
// Each adapter connects Pilot to a different service (Linear, Slack, GitHub, GitLab, etc.).
type AdaptersConfig struct {
	Linear      *linear.Config      `yaml:"linear"`
	Slack       *slack.Config       `yaml:"slack"`
	Telegram    *telegram.Config    `yaml:"telegram"`
	GitHub      *github.Config      `yaml:"github"`
	GitLab      *gitlab.Config      `yaml:"gitlab"`
	AzureDevOps *azuredevops.Config `yaml:"azure_devops"`
	Jira        *jira.Config        `yaml:"jira"`
	Asana       *asana.Config       `yaml:"asana"`
	Plane       *plane.Config       `yaml:"plane"`
	Discord     *discord.Config     `yaml:"discord"`
}

// OrchestratorConfig holds settings for the task orchestrator including
// the AI model to use, concurrency limits, and daily brief scheduling.
type OrchestratorConfig struct {
	Model         string            `yaml:"model"`
	MaxConcurrent int               `yaml:"max_concurrent"`
	DailyBrief    *DailyBriefConfig `yaml:"daily_brief"`
	Execution     *ExecutionConfig  `yaml:"execution"`
	Autopilot     *autopilot.Config `yaml:"autopilot"`
}

// ExecutionConfig holds settings for task execution mode.
// Sequential mode executes one task at a time, waiting for PR merge before the next.
// Parallel mode (legacy) processes multiple tasks concurrently.
// Auto mode (default) uses parallel dispatch with scope-overlap guard.
type ExecutionConfig struct {
	Mode         string        `yaml:"mode"`           // "sequential", "parallel", or "auto"
	WaitForMerge bool          `yaml:"wait_for_merge"` // Wait for PR merge before next task
	PollInterval time.Duration `yaml:"poll_interval"`  // How often to check PR status (default: 30s)
	PRTimeout    time.Duration `yaml:"pr_timeout"`     // Max wait time for PR merge (default: 1h)
}

// DefaultExecutionConfig returns sensible defaults for execution config
func DefaultExecutionConfig() *ExecutionConfig {
	return &ExecutionConfig{
		Mode:         "auto",
		WaitForMerge: true,
		PollInterval: 30 * time.Second,
		PRTimeout:    1 * time.Hour,
	}
}

// DailyBriefConfig holds settings for automated daily summary reports
// including schedule, delivery channels, and content filters.
type DailyBriefConfig struct {
	Enabled  bool                 `yaml:"enabled"`
	Schedule string               `yaml:"schedule"` // Cron syntax: "0 9 * * 1-5"
	Time     string               `yaml:"time"`     // Deprecated: use schedule
	Timezone string               `yaml:"timezone"`
	Channels []BriefChannelConfig `yaml:"channels"`
	Content  BriefContentConfig   `yaml:"content"`
	Filters  BriefFilterConfig    `yaml:"filters"`
}

// BriefChannelConfig defines a delivery channel for daily briefs (Slack or email).
type BriefChannelConfig struct {
	Type       string   `yaml:"type"`       // "slack", "email"
	Channel    string   `yaml:"channel"`    // For Slack: "#channel-name"
	Recipients []string `yaml:"recipients"` // For email
}

// BriefContentConfig controls what content is included in daily briefs.
type BriefContentConfig struct {
	IncludeMetrics     bool `yaml:"include_metrics"`
	IncludeErrors      bool `yaml:"include_errors"`
	MaxItemsPerSection int  `yaml:"max_items_per_section"`
}

// BriefFilterConfig filters which tasks to include in daily briefs.
type BriefFilterConfig struct {
	Projects []string `yaml:"projects"` // Empty = all projects
}

// LearningConfig holds settings for the pattern learning system.
type LearningConfig struct {
	Enabled       bool    `yaml:"enabled"`        // Enable learning system (default: true)
	MinConfidence float64 `yaml:"min_confidence"` // Min confidence for prompt injection (default: 0.6)
	MaxPatterns   int     `yaml:"max_patterns"`   // Max patterns injected per task (default: 5)
	IncludeAnti   bool    `yaml:"include_anti"`   // Include anti-patterns (default: true)
}

// DefaultLearningConfig returns sensible defaults for the learning system.
func DefaultLearningConfig() *LearningConfig {
	return &LearningConfig{
		Enabled:       true,
		MinConfidence: 0.6,
		MaxPatterns:   5,
		IncludeAnti:   true,
	}
}

// MemoryConfig holds settings for the persistent memory/storage system.
type MemoryConfig struct {
	Path         string          `yaml:"path"`
	CrossProject bool            `yaml:"cross_project"`
	Learning     *LearningConfig `yaml:"learning"`
	// SyncToFiles exports experiential memories to markdown files under
	// .agent/knowledge/memories/ on the maintenance ticker. Default false:
	// Navigator owns that directory (slug-named files), so opt in explicitly.
	SyncToFiles bool `yaml:"sync_to_files"`
}

// DashboardConfig holds settings for the terminal UI dashboard.
type DashboardConfig struct {
	RefreshInterval int  `yaml:"refresh_interval"`
	ShowLogs        bool `yaml:"show_logs"`
}
