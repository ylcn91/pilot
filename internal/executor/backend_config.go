package executor

import (
	"time"
)

// BackendConfig contains configuration for executor backends.
type BackendConfig struct {
	// Type specifies which backend to use ("claude-code", "opencode", "qwen-code", or "codex-exec")
	Type string `yaml:"type"`

	// Pipeline optionally splits a run across per-phase backends (plan/execute/
	// review). When nil (default), every phase uses Type — today's behavior.
	Pipeline *PipelineConfig `yaml:"pipeline,omitempty"`

	// AutoCreatePR controls whether PRs are created by default after successful execution.
	// Default: true. Use --no-pr flag to disable for individual tasks.
	AutoCreatePR *bool `yaml:"auto_create_pr,omitempty"`

	// CreateSubIssues controls whether epic planning may create GitHub sub-issues.
	// Default: false. When disabled, planned epics execute as one task instead.
	CreateSubIssues bool `yaml:"create_sub_issues,omitempty"`

	// DirectCommit enables committing directly to main without branches or PRs.
	// DANGER: Requires BOTH this config option AND --direct-commit CLI flag.
	// Intended for users who rely on manual QA instead of code review.
	DirectCommit bool `yaml:"direct_commit,omitempty"`

	// DetectEphemeral enables automatic detection of ephemeral tasks (serve, run, etc.)
	// that shouldn't create PRs. When true (default), commands like "serve the app"
	// or "run dev server" will execute without creating a PR.
	DetectEphemeral *bool `yaml:"detect_ephemeral,omitempty"`

	// SkipSelfReview disables the self-review phase before PR creation.
	// When false (default), Pilot runs a self-review phase after quality gates pass
	// to catch issues like unwired config, undefined methods, or incomplete implementations.
	SkipSelfReview bool `yaml:"skip_self_review,omitempty"`

	// ClaudeCode contains Claude Code specific settings
	ClaudeCode *ClaudeCodeConfig `yaml:"claude_code,omitempty"`

	// OpenCode contains OpenCode specific settings
	OpenCode *OpenCodeConfig `yaml:"opencode,omitempty"`

	// QwenCode contains Qwen Code specific settings
	QwenCode *QwenCodeConfig `yaml:"qwen_code,omitempty"`

	// CodexExec contains Codex CLI non-interactive backend settings
	CodexExec *CodexExecConfig `yaml:"codex_exec,omitempty"`

	// OpenAI contains OpenAI-compatible direct HTTP backend settings
	OpenAI *OpenAIConfig `yaml:"openai,omitempty"`

	// ModelRouting contains model selection based on task complexity
	ModelRouting *ModelRoutingConfig `yaml:"model_routing,omitempty"`

	// Timeout contains execution timeout settings
	Timeout *TimeoutConfig `yaml:"timeout,omitempty"`

	// EffortRouting contains effort level selection based on task complexity
	EffortRouting *EffortRoutingConfig `yaml:"effort_routing,omitempty"`

	// EffortClassifier contains LLM-based effort classification settings (GH-727)
	EffortClassifier *EffortClassifierConfig `yaml:"effort_classifier,omitempty"`

	// Decompose contains auto-decomposition settings for complex tasks
	Decompose *DecomposeConfig `yaml:"decompose,omitempty"`

	// IntentJudge contains intent alignment settings for diff-vs-ticket verification
	IntentJudge *IntentJudgeConfig `yaml:"intent_judge,omitempty"`

	// PreFlightJudge contains pre-flight issue quality judge settings (GH-2802).
	// When enabled, each issue is evaluated by Haiku before dispatch; vague/question/
	// conflicting/stale/out-of-scope issues are declined without burning a worker slot.
	PreFlightJudge *PreFlightJudgeConfig `yaml:"pre_flight_judge,omitempty"`

	// Navigator contains Navigator auto-init settings
	Navigator *NavigatorConfig `yaml:"navigator,omitempty"`

	// Hooks contains Claude Code hooks settings for quality gates during execution
	Hooks *HooksConfig `yaml:"hooks,omitempty"`

	// Planning controls the model used for epic planning subprocesses (GH-2432).
	// Planning gets Opus while execution stays on Sonnet for cost savings.
	Planning *PlanningConfig `yaml:"planning,omitempty"`

	// UseWorktree enables git worktree isolation for execution.
	// When true, Pilot creates a temporary worktree for each task, allowing
	// execution even when the user has uncommitted changes in their working directory.
	// Default: false (opt-in feature)
	UseWorktree bool `yaml:"use_worktree,omitempty"`

	// WorktreePoolSize sets the number of pre-created worktrees to pool.
	// When > 0, worktrees are reused across tasks in sequential mode, saving 500ms-2s per task.
	// Pool paths: /tmp/pilot-worktree-pool-N/
	// Set to 0 to disable pooling (current behavior).
	// Default: 0 (disabled)
	WorktreePoolSize int `yaml:"worktree_pool_size,omitempty"`

	// SyncMainAfterTask enables syncing the local main branch with origin after task completion.
	// When true, Pilot fetches origin/main and resets local main to match after each task.
	// This prevents local/remote divergence over time.
	// Default: false (opt-in feature)
	// GH-1018: Added to prevent local/remote main branch divergence
	SyncMainAfterTask bool `yaml:"sync_main_after_task,omitempty"`

	// Retry contains error-type-specific retry strategies (GH-920)
	Retry *RetryConfig `yaml:"retry,omitempty"`

	// Stagnation contains stagnation detection settings (GH-925)
	Stagnation *StagnationConfig `yaml:"stagnation,omitempty"`

	// SubprocessLimits controls memory caps and RSS telemetry for the Claude Code
	// subprocess. Enabled=false by default; flip after collecting a baseline week of
	// peak_rss_mb data to choose a safe cap. GH-3028.
	SubprocessLimits *SubprocessLimitsConfig `yaml:"subprocess_limits,omitempty"`

	// Simplification contains code simplification settings (GH-995)
	// When enabled, Pilot auto-simplifies code after implementation for clarity.
	Simplification *SimplifyConfig `yaml:"simplification,omitempty"`

	// PrePushLint enables lint checking before pushing to remote.
	// When true (default), Pilot runs linter (golangci-lint for Go projects) after commit.
	// If fixable issues are found, they are auto-fixed and re-committed.
	// Unfixable issues are included in the execution result for self-review.
	// GH-1376: Added to prevent lint-failure cascades
	PrePushLint *bool `yaml:"pre_push_lint,omitempty"`

	// HeartbeatTimeout is the time to wait for any stream-json event before
	// considering the subprocess hung and killing it.
	// Valid range: 1m to 30m. Default: 5m.
	HeartbeatTimeout time.Duration `yaml:"heartbeat_timeout,omitempty"`

	// StallTimeoutMs is the duration (in milliseconds) without any agent event
	// before a session is considered stalled and killed. 0 disables stall detection.
	// Default: 180000 (3 minutes). TASK-308.
	StallTimeoutMs int `yaml:"stall_timeout_ms,omitempty"`

	// PlanningTimeout is the maximum time to wait for epic planning (PlanEpic).
	// If planning exceeds this timeout, execution falls through to direct (non-epic) mode.
	// Default: 2m
	PlanningTimeout time.Duration `yaml:"planning_timeout,omitempty"`

	// DefaultModel overrides all model name references throughout the executor.
	// When set, all internal LLM calls (classifiers, judges, parsers, summaries)
	// use this model instead of hardcoded Anthropic model names.
	// For claude-code backend, main execution does NOT pass --model (lets CC use its own settings).
	// When empty, existing Anthropic defaults are used.
	DefaultModel string `yaml:"default_model,omitempty"`

	// APIBaseURL overrides the Anthropic API base URL for all direct API calls.
	// Used by effort classifier, intent judge, subtask parser, release summary.
	// Example: "https://api.z.ai/api/anthropic" for Z.AI provider.
	// When empty, defaults to "https://api.anthropic.com".
	APIBaseURL string `yaml:"api_base_url,omitempty"`

	// APIAuthToken is the auth token for non-Anthropic providers (GH-2371).
	// Supports ${ENV_VAR} expansion (via os.ExpandEnv during config load).
	// When set together with APIBaseURL, Pilot injects ANTHROPIC_BASE_URL,
	// ANTHROPIC_AUTH_TOKEN, and ANTHROPIC_MODEL into the Claude Code
	// subprocess env so a single config drives both Pilot-internal HTTP calls
	// and the CC subprocess. When empty, the CC subprocess uses its own auth
	// (~/.claude/settings.json, ANTHROPIC_API_KEY, or CC OAuth).
	APIAuthToken string `yaml:"api_auth_token,omitempty"`

	// Version is the Pilot binary version, set at startup from the build-time version var.
	// Used for feature matrix updates and execution reports. Not a config file field.
	Version string `yaml:"-"`
}

// EffectiveStallTimeout returns the stall detection threshold, applying the
// default of 3 minutes when StallTimeoutMs is zero or unset.
// Returns 0 only when StallTimeoutMs is explicitly set to a negative value,
// which disables stall detection entirely.
func (c *BackendConfig) EffectiveStallTimeout() time.Duration {
	if c == nil || c.StallTimeoutMs == 0 {
		return 3 * time.Minute
	}
	if c.StallTimeoutMs < 0 {
		return 0
	}
	return time.Duration(c.StallTimeoutMs) * time.Millisecond
}

// EffectiveHeartbeatTimeout returns the heartbeat timeout to use, applying
// defaults and clamping to the valid range [1m, 30m].
func (c *BackendConfig) EffectiveHeartbeatTimeout() time.Duration {
	if c == nil || c.HeartbeatTimeout <= 0 {
		return DefaultHeartbeatTimeout
	}
	if c.HeartbeatTimeout < MinHeartbeatTimeout {
		return MinHeartbeatTimeout
	}
	if c.HeartbeatTimeout > MaxHeartbeatTimeout {
		return MaxHeartbeatTimeout
	}
	return c.HeartbeatTimeout
}

// ResolveModel returns the default model if set, otherwise falls back to the explicit model name.
// Use this wherever a model name is needed: classifiers, judges, parsers, summaries.
func (c *BackendConfig) ResolveModel(explicit string) string {
	if c != nil && c.DefaultModel != "" {
		return c.DefaultModel
	}
	return explicit
}

// ResolveAPIBaseURL returns the configured API base URL, or the Anthropic default.
// Callers should append "/v1/messages" for the full endpoint.
func (c *BackendConfig) ResolveAPIBaseURL() string {
	if c != nil && c.APIBaseURL != "" {
		return c.APIBaseURL
	}
	return "https://api.anthropic.com"
}

// DefaultBackendConfig returns default backend configuration.
func DefaultBackendConfig() *BackendConfig {
	autoCreatePR := true
	detectEphemeral := true
	prePushLint := true
	return &BackendConfig{
		Type:            BackendTypeCodexExec,
		AutoCreatePR:    &autoCreatePR,
		DetectEphemeral: &detectEphemeral,
		PrePushLint:     &prePushLint,
		PlanningTimeout: 2 * time.Minute,
		ClaudeCode: &ClaudeCodeConfig{
			Command:      "claude",
			AllowedTools: DefaultAllowedToolsExecution(),
		},
		QwenCode: &QwenCodeConfig{
			Command: "qwen",
		},
		CodexExec: &CodexExecConfig{
			Command: "codex",
			Sandbox: "workspace-write",
		},
		OpenCode: &OpenCodeConfig{
			ServerURL:       "http://127.0.0.1:4096",
			Model:           "anthropic/claude-sonnet-4-6",
			Provider:        "anthropic",
			AutoStartServer: true,
			ServerCommand:   "opencode serve",
			RequestTimeout:  "10m",
		},
		ModelRouting:     DefaultModelRoutingConfig(),
		Timeout:          DefaultTimeoutConfig(),
		EffortRouting:    DefaultEffortRoutingConfig(),
		EffortClassifier: DefaultEffortClassifierConfig(),
		Decompose:        DefaultDecomposeConfig(),
		IntentJudge:      DefaultIntentJudgeConfig(),
		Navigator:        DefaultNavigatorConfig(),
		Hooks:            DefaultHooksConfig(),
		Planning:         DefaultPlanningConfig(),
		Retry:            DefaultRetryConfig(),
		Stagnation:       DefaultStagnationConfig(),
		Simplification:   DefaultSimplifyConfig(),
		SubprocessLimits: DefaultSubprocessLimitsConfig(),
	}
}

// BackendType constants for configuration.
const (
	BackendTypeClaudeCode = "claude-code"
	BackendTypeOpenCode   = "opencode"
	BackendTypeQwenCode   = "qwen-code"
	BackendTypeCodexExec  = "codex-exec"
)
