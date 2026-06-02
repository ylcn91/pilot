package executor

import (
	"context"
	"time"
)

// Backend defines the interface for AI execution backends.
// Implementations handle the specifics of invoking different AI coding agents
// (Claude Code, OpenCode, etc.) while providing a unified interface to the Runner.
type Backend interface {
	// Name returns the backend identifier (e.g., "claude-code", "opencode")
	Name() string

	// Execute runs a prompt against the backend and streams events.
	// The eventHandler is called for each event received from the backend.
	// Returns the final result or error.
	Execute(ctx context.Context, opts ExecuteOptions) (*BackendResult, error)

	// IsAvailable checks if the backend is properly configured and accessible.
	IsAvailable() bool
}

// ExecuteOptions contains parameters for backend execution.
type ExecuteOptions struct {
	// Prompt is the full prompt to send to the AI backend
	Prompt string

	// ProjectPath is the working directory for execution
	ProjectPath string

	// Verbose enables detailed output logging
	Verbose bool

	// Model specifies the model to use for execution (e.g., "claude-haiku", "claude-opus").
	// If empty, the backend's default model is used.
	Model string

	// Effort specifies the effort level for execution (e.g., "low", "medium", "high", "max").
	// If empty, the backend's default effort is used (high).
	// Maps to Claude API output_config.effort or Claude Code --effort flag.
	Effort string

	// MaxTurns limits the number of agentic turns Claude Code may take.
	// When > 0, passed as --max-turns to the Claude Code subprocess.
	// Zero means no limit (Claude Code default).
	MaxTurns int

	// ResumeSessionID enables session resume for continued context (GH-1265).
	// When set, uses --resume <session_id> to continue an existing Claude Code session,
	// eliminating context rebuild overhead (~40% token savings for self-review).
	ResumeSessionID string

	// FromPR specifies a PR number to use for --from-pr session resumption (GH-1267).
	// When set, uses --from-pr <N> to resume the session linked to that PR,
	// giving Claude full context of what was previously changed.
	FromPR int

	// EventHandler receives streaming events during execution
	// The handler receives the raw event line from the backend
	EventHandler func(event BackendEvent)

	// HeartbeatCallback is invoked when subprocess heartbeat timeout is detected.
	// The callback receives the process PID and the time since the last event.
	// After callback invocation, the process will be killed.
	HeartbeatCallback func(pid int, lastEventAge time.Duration)

	// WatchdogTimeout is the absolute time limit after which the subprocess will be
	// forcibly killed. This is a safety net for processes that ignore context cancellation.
	// When set (> 0), a watchdog goroutine will kill the process after this duration.
	WatchdogTimeout time.Duration

	// WatchdogCallback is invoked when the watchdog kills a subprocess.
	// The callback receives the process PID and the watchdog timeout duration.
	// Called BEFORE the process is killed, allowing for alert emission.
	WatchdogCallback func(pid int, watchdogTimeout time.Duration)

	// AllowedTools restricts the set of tools the subprocess may use.
	// Maps to the Claude Code CLI --allowedTools flag. GH-2432.
	AllowedTools []string

	// MCPConfigPath is passed to the subprocess via --mcp-config. When empty,
	// no MCP servers are loaded (avoiding tool-definition token bloat). GH-2432.
	MCPConfigPath string
}

// BackendEvent represents a streaming event from the backend.
// Each backend maps its native events to this common format.
type BackendEvent struct {
	// Type identifies the event category
	Type BackendEventType

	// Raw contains the original event data (JSON string)
	Raw string

	// Phase indicates the current execution phase (if detectable)
	Phase string

	// Message contains a human-readable description
	Message string

	// ToolName is set for tool_use events
	ToolName string

	// ToolInput contains tool parameters for tool_use events
	ToolInput map[string]interface{}

	// ToolResult contains the output for tool_result events
	ToolResult string

	// IsError indicates if this is an error event
	IsError bool

	// TokensInput is the input token count (if available)
	TokensInput int64

	// TokensOutput is the output token count (if available)
	TokensOutput int64

	// CacheCreationInputTokens is the cache creation input token count (GH-2164)
	CacheCreationInputTokens int64

	// CacheReadInputTokens is the cache read input token count (GH-2164)
	CacheReadInputTokens int64

	// Model is the model name used (if available)
	Model string

	// SessionID is the Claude Code session ID for resume support (GH-1265)
	SessionID string
}

// BackendError is implemented by all backend-specific error types (ClaudeCodeError,
// QwenCodeError, etc.) to enable unified error handling in retry logic and runner.
type BackendError interface {
	error
	// ErrorType returns the error category as a string (e.g., "rate_limit", "api_error").
	ErrorType() string
	// ErrorMessage returns the human-readable error description.
	ErrorMessage() string
	// ErrorStderr returns the captured stderr output.
	ErrorStderr() string
}

// BackendEventType categorizes backend events.
type BackendEventType string

const (
	// EventTypeInit indicates the backend is starting
	EventTypeInit BackendEventType = "init"

	// EventTypeText indicates a text/message block
	EventTypeText BackendEventType = "text"

	// EventTypeToolUse indicates a tool is being invoked
	EventTypeToolUse BackendEventType = "tool_use"

	// EventTypeToolResult indicates a tool execution result
	EventTypeToolResult BackendEventType = "tool_result"

	// EventTypeResult indicates final execution result
	EventTypeResult BackendEventType = "result"

	// EventTypeError indicates an error occurred
	EventTypeError BackendEventType = "error"

	// EventTypeProgress indicates a progress update
	EventTypeProgress BackendEventType = "progress"
)

// BackendResult contains the outcome of a backend execution.
type BackendResult struct {
	// Success indicates whether execution completed successfully
	Success bool

	// Output contains the final output text
	Output string

	// Error contains error details if execution failed
	Error string

	// TokensInput is the total input tokens consumed
	TokensInput int64

	// TokensOutput is the total output tokens generated
	TokensOutput int64

	// CacheCreationInputTokens is the total cache creation input tokens (GH-2164)
	CacheCreationInputTokens int64

	// CacheReadInputTokens is the total cache read input tokens (GH-2164)
	CacheReadInputTokens int64

	// Model is the model used for execution
	Model string

	// SessionID is the Claude Code session ID for resume support (GH-1265)
	SessionID string

	// SawSuccessResult tracks whether a successful result event was observed during
	// stream-json parsing. Used to recover success when the process exits with an error
	// after completing work (e.g., timeout on final summary). GH-2107.
	SawSuccessResult bool

	// Stderr is the full captured stderr output from the backend subprocess.
	// Populated even on success so Pilot can log warnings; critical on failure
	// for diagnosing `unknown: exit status 1`. GH-2328.
	Stderr string

	// LastAssistantText is the final assistant `text` block observed in the
	// stream-json output. When Claude refuses a task (exits 0 or non-zero with
	// no stderr), this captures the refusal reason for diagnosis. GH-2328.
	LastAssistantText string

	// ErrorType classifies the failure (rate_limit, api_error, oom_killed,
	// session_not_found, timeout, invalid_config, unknown). GH-2328.
	ErrorType string

	// PeakRSSMB is the peak resident set size of the subprocess in MiB. GH-3028.
	// Zero when RSS sampling is unavailable (non-Linux/darwin platforms).
	PeakRSSMB int

	// FinalRSSMB is the RSS at subprocess exit. GH-3028.
	FinalRSSMB int
}
