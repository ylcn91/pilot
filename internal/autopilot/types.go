package autopilot

import (
	"fmt"
	"time"
)

// Environment defines automation environment behavior.
// Different environments have different levels of automation and approval requirements.
type Environment string

const (
	// EnvDev is the development environment with auto-merge, no approval required.
	EnvDev Environment = "dev"
	// EnvStage is the staging environment with auto-merge after CI passes.
	EnvStage Environment = "stage"
	// EnvProd is the production environment requiring human approval.
	EnvProd Environment = "prod"
)

// ApprovalSource specifies which channel to use for approval requests.
type ApprovalSource string

const (
	// ApprovalSourceTelegram uses Telegram for approval requests.
	ApprovalSourceTelegram ApprovalSource = "telegram"
	// ApprovalSourceSlack uses Slack for approval requests.
	ApprovalSourceSlack ApprovalSource = "slack"
	// ApprovalSourceGitHubReview uses GitHub PR reviews for approval.
	ApprovalSourceGitHubReview ApprovalSource = "github-review"
)

// GitHubReviewConfig holds configuration for GitHub PR review approval.
type GitHubReviewConfig struct {
	// PollInterval is how often to poll for PR reviews (default: 30s).
	PollInterval time.Duration `yaml:"poll_interval"`
}

// EnvironmentConfig defines an automation pipeline for one target environment.
type EnvironmentConfig struct {
	// Branch is the target branch for PRs (e.g., "main", "develop").
	Branch string `yaml:"branch"`
	// RequireApproval gates merge on human approval.
	RequireApproval bool `yaml:"require_approval"`
	// ApprovalSource specifies which channel for approvals (telegram, slack, github-review).
	ApprovalSource ApprovalSource `yaml:"approval_source,omitempty"`
	// ApprovalTimeout is how long to wait for human approval.
	ApprovalTimeout time.Duration `yaml:"approval_timeout,omitempty"`
	// CITimeout overrides the CI wait timeout for this environment.
	CITimeout time.Duration `yaml:"ci_timeout"`
	// SkipPostMergeCI skips post-merge CI monitoring (fast path).
	SkipPostMergeCI bool `yaml:"skip_post_merge_ci"`
	// MergeMethod overrides the default merge method for this environment.
	MergeMethod string `yaml:"merge_method,omitempty"`
	// Release holds per-environment release configuration.
	Release *ReleaseConfig `yaml:"release,omitempty"`
}

// Config holds autopilot configuration for automated PR handling.
type Config struct {
	// Enabled controls whether autopilot mode is active.
	Enabled bool `yaml:"enabled"`
	// Environment determines the automation level (dev/stage/prod).
	// DEPRECATED: use Environments map + DefaultEnvironment instead.
	Environment Environment `yaml:"environment,omitempty"`

	// DefaultEnvironment is the name of the environment used when --env is not specified.
	DefaultEnvironment string `yaml:"default_environment,omitempty"`
	// Environments is a map of named environment pipeline configs.
	Environments map[string]*EnvironmentConfig `yaml:"environments,omitempty"`

	// Runtime fields (not serialized to YAML).
	activeEnvName   string
	activeEnvConfig *EnvironmentConfig

	// Approval
	// ApprovalSource specifies which channel to use for approvals (telegram, slack, github-review).
	ApprovalSource ApprovalSource `yaml:"approval_source"`
	// GitHubReview holds configuration for GitHub PR review approval.
	GitHubReview *GitHubReviewConfig `yaml:"github_review"`

	// PR Handling
	// AutoReview enables automatic PR review comments.
	AutoReview bool `yaml:"auto_review"`
	// AutoMerge enables automatic PR merging when conditions are met.
	AutoMerge bool `yaml:"auto_merge"`
	// MergeMethod specifies how to merge PRs: merge, squash, or rebase.
	MergeMethod string `yaml:"merge_method"`

	// CI Monitoring
	// CIWaitTimeout is the maximum time to wait for CI to complete.
	CIWaitTimeout time.Duration `yaml:"ci_wait_timeout"`
	// DevCITimeout is the CI timeout for dev environment (default 5m, shorter than stage/prod).
	DevCITimeout time.Duration `yaml:"dev_ci_timeout"`
	// CIPollInterval is how often to check CI status.
	CIPollInterval time.Duration `yaml:"ci_poll_interval"`
	// RequiredChecks lists CI checks that must pass before merge.
	// Deprecated: Use CIChecks.Required instead.
	RequiredChecks []string `yaml:"required_checks"`
	// CIChecks holds CI check discovery configuration.
	CIChecks *CIChecksConfig `yaml:"ci_checks"`

	// Feedback Loop
	// AutoCreateIssues enables automatic issue creation for CI failures.
	AutoCreateIssues bool `yaml:"auto_create_issues"`
	// IssueLabels are labels applied to auto-created issues.
	IssueLabels []string `yaml:"issue_labels"`
	// NotifyOnFailure enables notifications when CI fails.
	NotifyOnFailure bool `yaml:"notify_on_failure"`

	// Review Feedback
	// ReviewFeedback configures automatic handling of PR review change requests.
	ReviewFeedback *ReviewFeedbackConfig `yaml:"review_feedback"`

	// Safety
	// MaxFailures is the circuit breaker threshold before pausing autopilot.
	MaxFailures int `yaml:"max_failures"`
	// MaxCIFixIterations limits how many CI fix issues can be chained before giving up.
	// Prevents infinite fix cascades where each fix creates a new issue that also fails CI.
	// Default: 3. Set to 0 to disable the limit.
	MaxCIFixIterations int `yaml:"max_ci_fix_iterations"`
	// MaxCIFixPRSize is the net-addition threshold above which autopilot refuses to spawn
	// a fix(ci) issue for the failing PR. A large failing PR is a cascade-contamination
	// signal — the same threshold (#2594) that gates auto-merge also gates fix-issue spawn.
	// Default: 200. Set to 0 to disable the guard.
	MaxCIFixPRSize int `yaml:"max_ci_fix_pr_size"`
	// FailureResetTimeout is how long after the last failure before the per-PR counter resets.
	// Default: 30 minutes.
	FailureResetTimeout time.Duration `yaml:"failure_reset_timeout"`
	// MaxMergesPerHour limits merge rate to prevent runaway automation.
	MaxMergesPerHour int `yaml:"max_merges_per_hour"`
	// MaxMergeAttempts is the hard cap on non-conflict merge retries before the PR
	// is transitioned to StageFailed and escalated to a human. The circuit breaker
	// (MaxFailures) provides transient backoff; this cap makes persistent failures
	// terminal so they don't loop indefinitely. Default: 5.
	MaxMergeAttempts int `yaml:"max_merge_attempts"`
	// ApprovalTimeout is how long to wait for human approval in prod.
	ApprovalTimeout time.Duration `yaml:"approval_timeout"`

	// Release holds auto-release configuration.
	Release *ReleaseConfig `yaml:"release"`

	// MergedPRScanWindow is how far back to look for merged PRs on startup (default: 30m).
	// This catches PRs that were merged while Pilot was offline.
	MergedPRScanWindow time.Duration `yaml:"merged_pr_scan_window"`

	// Name is a user-friendly label for this environment (e.g. "staging", "production").
	// When empty, defaults to the Environment value.
	Name string `yaml:"name"`
}

// ReviewFeedbackConfig holds configuration for handling PR review change requests.
type ReviewFeedbackConfig struct {
	// Enabled controls whether review feedback handling is active.
	Enabled bool `yaml:"enabled"`
	// MaxIterations limits how many revision issues can be chained before giving up.
	// Prevents infinite review-fix cycles. Default: 3. Set to 0 to disable the limit.
	MaxIterations int `yaml:"max_iterations"`
}

// CIChecksConfig holds configuration for CI check monitoring.
type CIChecksConfig struct {
	// Mode: "auto" (discover from API) or "manual" (use Required list).
	Mode string `yaml:"mode"`

	// Exclude lists check names to ignore in auto mode (supports glob patterns).
	Exclude []string `yaml:"exclude"`

	// Required lists check names for manual mode.
	Required []string `yaml:"required"`

	// DiscoveryGracePeriod: how long to wait for checks to appear (default 60s).
	DiscoveryGracePeriod time.Duration `yaml:"discovery_grace_period"`
}

// defaultEnvironments returns built-in environment configs matching legacy behavior.
func defaultEnvironments() map[string]*EnvironmentConfig {
	return map[string]*EnvironmentConfig{
		"dev": {
			Branch:          "main",
			RequireApproval: false,
			CITimeout:       5 * time.Minute,
			SkipPostMergeCI: true,
		},
		"stage": {
			Branch:          "main",
			RequireApproval: false,
			CITimeout:       30 * time.Minute,
			SkipPostMergeCI: false,
		},
		"prod": {
			Branch:          "main",
			RequireApproval: true,
			ApprovalSource:  ApprovalSourceTelegram,
			ApprovalTimeout: 1 * time.Hour,
			CITimeout:       30 * time.Minute,
			SkipPostMergeCI: false,
		},
	}
}

// ResolvedEnv returns the active environment config.
// If activeEnvName is set and the Environments map contains it, that entry is returned.
// Otherwise falls back to the legacy Environment field and synthesizes from defaultEnvironments.
func (c *Config) ResolvedEnv() *EnvironmentConfig {
	// New-style: runtime-selected environment takes priority.
	if c.activeEnvName != "" {
		if c.activeEnvConfig != nil {
			return c.activeEnvConfig
		}
		if c.Environments != nil {
			if env, ok := c.Environments[c.activeEnvName]; ok {
				return env
			}
		}
	}

	// Legacy: derive from the Environment field using built-in defaults.
	envName := string(c.Environment)
	if envName == "" {
		envName = "stage"
	}
	defaults := defaultEnvironments()
	if env, ok := defaults[envName]; ok {
		return env
	}
	// Unknown legacy environment: treat as stage (safe default).
	return defaults["stage"]
}

// EnvironmentName returns the human-readable active environment name.
// Checks Name field first (user-friendly label), then activeEnvName,
// then falls back to the Environment enum value.
func (c *Config) EnvironmentName() string {
	if c.Name != "" {
		return c.Name
	}
	if c.activeEnvName != "" {
		return c.activeEnvName
	}
	if c.Environment != "" {
		return string(c.Environment)
	}
	return "stage"
}

// SetActiveEnvironment sets the runtime-resolved environment by name.
// Checks the Environments map first, then falls back to built-in defaults.
// Called during CLI flag processing.
func (c *Config) SetActiveEnvironment(name string) error {
	// New-style: check user-defined Environments map first.
	if c.Environments != nil {
		if env, ok := c.Environments[name]; ok {
			c.activeEnvName = name
			c.activeEnvConfig = env
			c.Environment = Environment(name) // keep legacy field in sync
			return nil
		}
	}

	// Fall back to built-in defaults.
	defaults := defaultEnvironments()
	if env, ok := defaults[name]; ok {
		c.activeEnvName = name
		c.activeEnvConfig = env
		c.Environment = Environment(name) // keep legacy field in sync
		return nil
	}

	return fmt.Errorf("unknown environment %q: must be one of dev, stage, prod or defined in environments config", name)
}

// DefaultConfig returns sensible defaults for autopilot configuration.
func DefaultConfig() *Config {
	return &Config{
		Enabled:        false,
		Environment:    EnvStage,
		ApprovalSource: ApprovalSourceTelegram, // Default to Telegram for backward compatibility
		GitHubReview: &GitHubReviewConfig{
			PollInterval: 30 * time.Second,
		},
		AutoReview:     true,
		AutoMerge:      true,
		MergeMethod:    "squash",
		CIWaitTimeout:  30 * time.Minute,
		DevCITimeout:   5 * time.Minute,
		CIPollInterval: 30 * time.Second,
		RequiredChecks: nil, // Deprecated, use CIChecks
		CIChecks: &CIChecksConfig{
			Mode:                 "auto",
			Exclude:              []string{},
			Required:             []string{},
			DiscoveryGracePeriod: 60 * time.Second,
		},
		ReviewFeedback: &ReviewFeedbackConfig{
			Enabled:       true,
			MaxIterations: 3,
		},
		AutoCreateIssues:    false,
		IssueLabels:         []string{"pilot", "autopilot-fix"},
		NotifyOnFailure:     true,
		MaxFailures:         3,
		MaxCIFixIterations:  3,
		MaxCIFixPRSize:      200,
		FailureResetTimeout: 30 * time.Minute,
		MaxMergesPerHour:    10,
		MaxMergeAttempts:    5,
		ApprovalTimeout:     1 * time.Hour,
		Release:             nil, // Disabled by default
		MergedPRScanWindow:  30 * time.Minute,
		Environments:        defaultEnvironments(),
	}
}
