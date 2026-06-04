package briefs

import "time"

// Brief represents a daily summary brief
type Brief struct {
	GeneratedAt    time.Time
	Period         BriefPeriod
	Completed      []TaskSummary
	InProgress     []TaskSummary
	Blocked        []BlockedTask
	Upcoming       []TaskSummary
	Metrics        BriefMetrics
	IncludeMetrics bool // When false, metrics are neither collected nor rendered
}

// BriefPeriod represents the time range for the brief
type BriefPeriod struct {
	Start time.Time
	End   time.Time
}

// TaskSummary represents a task in the brief
type TaskSummary struct {
	ID          string
	Title       string
	ProjectPath string
	Status      string
	Progress    int    // 0-100
	PRUrl       string // For completed tasks with PRs
	DurationMs  int64
	CompletedAt *time.Time
}

// BlockedTask represents a task that failed or is blocked
type BlockedTask struct {
	TaskSummary
	Error      string
	FailedAt   time.Time
	RetryCount int
}

// BriefMetrics contains aggregate metrics for the period
type BriefMetrics struct {
	TotalTasks       int
	CompletedCount   int
	FailedCount      int
	SuccessRate      float64 // 0.0-1.0
	AvgDurationMs    int64
	PRsCreated       int
	PRsMerged        int
	TotalTokensUsed  int64
	EstimatedCostUSD float64
}

// BriefConfig holds configuration for brief generation
type BriefConfig struct {
	Enabled  bool            `yaml:"enabled"`
	Schedule string          `yaml:"schedule"` // Cron syntax: "0 9 * * 1-5"
	Timezone string          `yaml:"timezone"`
	Channels []ChannelConfig `yaml:"channels"`
	Content  ContentConfig   `yaml:"content"`
	Filters  FilterConfig    `yaml:"filters"`
	Email    EmailConfig     `yaml:"email"` // SMTP settings for email channels
}

// EmailConfig holds SMTP settings used to construct the email sender.
type EmailConfig struct {
	Host          string `yaml:"host"`
	Port          int    `yaml:"port"`
	From          string `yaml:"from"`
	Username      string `yaml:"username"`
	Password      string `yaml:"password"`
	AllowInsecure bool   `yaml:"allow_insecure"` // Permit sending without STARTTLS (trusted relays only)
}

// IsConfigured reports whether enough SMTP settings are present to build a
// working sender (host and from address are the minimum).
func (e EmailConfig) IsConfigured() bool {
	return e.Host != "" && e.From != ""
}

// ChannelConfig defines a delivery channel
type ChannelConfig struct {
	Type       string   `yaml:"type"`       // "slack", "email", "telegram"
	Channel    string   `yaml:"channel"`    // For Slack: "#channel-name", For Telegram: chat_id
	Recipients []string `yaml:"recipients"` // For email
}

// ContentConfig controls what content is included
type ContentConfig struct {
	IncludeMetrics     bool `yaml:"include_metrics"`
	IncludeErrors      bool `yaml:"include_errors"`
	MaxItemsPerSection int  `yaml:"max_items_per_section"`
}

// FilterConfig filters which tasks to include
type FilterConfig struct {
	Projects []string `yaml:"projects"` // Empty = all projects
}

// DeliveryResult represents the result of sending a brief
type DeliveryResult struct {
	Channel   string
	Success   bool
	Error     error
	SentAt    time.Time
	MessageID string // Platform-specific message ID
}
