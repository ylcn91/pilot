package executor

import (
	"context"
	"log/slog"
	"os/exec"
	"time"

	"github.com/ylcn91/pilot/internal/logging"
)

// GracePeriod is the time to wait after context cancellation before hard killing the process.
// This allows the process to clean up gracefully if it responds to SIGTERM.
const GracePeriod = 5 * time.Second

// MaxStderrBufferBytes caps the in-memory stderr buffer for a single Claude Code
// invocation. GH-2332: long-running Navigator sessions (10+ min, 100+ tool calls)
// could accumulate unbounded stderr lines and push Pilot into OOM territory.
// When the cap is reached, the oldest bytes are dropped (tail-truncation) so the
// most recent stderr — which classifyClaudeCodeError actually inspects — is kept.
const MaxStderrBufferBytes = 1 << 20 // 1 MiB

// DefaultHeartbeatTimeout is the default time to wait for any stream-json event before considering the process hung.
const DefaultHeartbeatTimeout = 5 * time.Minute

// MinHeartbeatTimeout is the minimum allowed heartbeat timeout.
const MinHeartbeatTimeout = 1 * time.Minute

// MaxHeartbeatTimeout is the maximum allowed heartbeat timeout.
const MaxHeartbeatTimeout = 30 * time.Minute

// HeartbeatCheckInterval is how often to check for heartbeat timeout.
const HeartbeatCheckInterval = 30 * time.Second

// HeartbeatCallback is a callback invoked when heartbeat timeout is detected.
// Returns true if the callback wants to handle the timeout (process will be killed).
type HeartbeatCallback func(pid int, lastEventAge time.Duration)

// ClaudeCodeBackend implements Backend for Claude Code CLI.
type ClaudeCodeBackend struct {
	config           *ClaudeCodeConfig
	heartbeatTimeout time.Duration
	log              *slog.Logger

	// GH-2371: provider routing env vars injected into the subprocess.
	// Sourced from BackendConfig.APIBaseURL / APIAuthToken / DefaultModel
	// via the factory. Empty = preserve today's CC defaults (OAuth /
	// ~/.claude/settings.json / ANTHROPIC_API_KEY).
	apiBaseURL   string
	apiAuthToken string
	defaultModel string

	// subprocessLimits configures RSS telemetry and optional RLIMIT_AS cap. GH-3028.
	subprocessLimits *SubprocessLimitsConfig
}

// NewClaudeCodeBackend creates a new Claude Code backend.
func NewClaudeCodeBackend(config *ClaudeCodeConfig) *ClaudeCodeBackend {
	if config == nil {
		config = &ClaudeCodeConfig{Command: "claude"}
	}
	if config.Command == "" {
		config.Command = "claude"
	}
	return &ClaudeCodeBackend{
		config:           config,
		heartbeatTimeout: DefaultHeartbeatTimeout,
		log:              logging.WithComponent("executor.claudecode"),
	}
}

// SetHeartbeatTimeout sets a custom heartbeat timeout for this backend.
func (b *ClaudeCodeBackend) SetHeartbeatTimeout(d time.Duration) {
	b.heartbeatTimeout = d
}

// SetSubprocessLimits configures RSS telemetry and optional memory cap for the
// Claude Code subprocess. GH-3028.
func (b *ClaudeCodeBackend) SetSubprocessLimits(cfg *SubprocessLimitsConfig) {
	b.subprocessLimits = cfg
}

// SetProviderEnv configures provider-routing env vars injected into the
// Claude Code subprocess (GH-2371). When any value is non-empty, the
// corresponding ANTHROPIC_* env var is appended to the subprocess env,
// letting a single Pilot config route both Pilot-internal HTTP calls and
// the CC subprocess to a non-Anthropic provider (Z.AI, OpenRouter, etc.).
// All empty = today's behavior (CC uses its own auth).
func (b *ClaudeCodeBackend) SetProviderEnv(baseURL, authToken, model string) {
	b.apiBaseURL = baseURL
	b.apiAuthToken = authToken
	b.defaultModel = model
}

// Name returns the backend identifier.
func (b *ClaudeCodeBackend) Name() string {
	return BackendTypeClaudeCode
}

// IsAvailable checks if Claude Code CLI is installed.
func (b *ClaudeCodeBackend) IsAvailable() bool {
	_, err := exec.LookPath(b.config.Command)
	return err == nil
}

// Execute runs a prompt through Claude Code CLI.
// If --from-pr is used and fails with session not found, it falls back to executing without it.
func (b *ClaudeCodeBackend) Execute(ctx context.Context, opts ExecuteOptions) (*BackendResult, error) {
	result, err := b.executeWithFromPR(ctx, opts, true)

	// GH-1267: Fallback if --from-pr fails with session not found
	if err != nil && opts.FromPR > 0 && b.config.UseFromPR {
		if ccErr, ok := err.(*ClaudeCodeError); ok && ccErr.Type == ErrorTypeSessionNotFound {
			b.log.Warn("Session not found for --from-pr, retrying without it",
				slog.Int("pr", opts.FromPR),
				slog.String("error", ccErr.Message),
			)
			// Retry without --from-pr
			return b.executeWithFromPR(ctx, opts, false)
		}
	}

	// GH-2377: Fallback if --resume fails with session not found.
	// Self-review reuses the main-execution session ID to save tokens
	// (GH-1265); when CC has evicted that session, drop --resume and
	// run a fresh session rather than silently skipping self-review.
	if err != nil && opts.ResumeSessionID != "" {
		if ccErr, ok := err.(*ClaudeCodeError); ok && ccErr.Type == ErrorTypeSessionNotFound {
			b.log.Warn("Session not found for --resume, retrying without it",
				slog.String("session_id", opts.ResumeSessionID),
				slog.String("error", ccErr.Message),
			)
			opts.ResumeSessionID = ""
			return b.Execute(ctx, opts)
		}
	}

	return result, err
}
