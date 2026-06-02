package executor

import (
	"time"
)

// ClaudeCodeConfig contains Claude Code backend configuration.
type ClaudeCodeConfig struct {
	// Command is the path to the claude CLI (default: "claude")
	Command string `yaml:"command,omitempty"`

	// ExtraArgs are additional arguments to pass to the CLI
	ExtraArgs []string `yaml:"extra_args,omitempty"`

	// UseStructuredOutput enables --json-schema structured output for classifiers and post-execution summary (default: false)
	UseStructuredOutput bool `yaml:"use_structured_output,omitempty"`

	// UseSessionResume enables session resume for self-review (GH-1265).
	// When true, self-review uses --resume <session_id> to continue the
	// original session, eliminating ~40% token waste from context rebuild.
	// Default: false
	UseSessionResume bool `yaml:"use_session_resume,omitempty"`

	// UseFromPR enables --from-pr session resumption for autopilot fix issues (GH-1267).
	// When true and a FromPR is specified, uses --from-pr <N> to resume the session
	// linked to the original PR, giving Claude full context of previous changes.
	// Default: false
	UseFromPR bool `yaml:"use_from_pr,omitempty"`

	// Disable1MContext opts out of 1M context (sets CLAUDE_CODE_DISABLE_1M_CONTEXT=1).
	// When true, forces 200K context window. Default false = use Claude Code defaults.
	Disable1MContext bool `yaml:"disable_1m_context,omitempty"`

	// MaxOutputTokens sets CLAUDE_CODE_MAX_OUTPUT_TOKENS. Default 0 = Claude Code default (32K).
	MaxOutputTokens int `yaml:"max_output_tokens,omitempty"`

	// DisableNavigatorForEpic skips Navigator context injection (project README,
	// SOPs, knowledge graph, memories) for COMPLEX / EPIC tasks. GH-2332: large
	// Navigator prompts on Opus 4.7 have correlated with OOM-killed subprocesses
	// on long runs. When true, such tasks fall back to the lean non-Navigator
	// prompt. Default: false (Navigator context always injected when available).
	DisableNavigatorForEpic bool `yaml:"disable_navigator_for_epic,omitempty"`

	// AllowedTools restricts the set of tools the Claude Code subprocess may use.
	// Maps to the CLI's --allowedTools flag. Default (when unset, see
	// DefaultAllowedToolsExecution) keeps the standard execution toolbox while
	// excluding heavy MCP-loaded tools, cutting per-turn token cost. GH-2432.
	AllowedTools []string `yaml:"allowed_tools,omitempty"`

	// MCPConfigPath points to an MCP server config file passed via --mcp-config.
	// When empty, the subprocess does NOT load any MCP servers — drastically
	// reducing tool-definition tokens replayed on every turn. GH-2432.
	MCPConfigPath string `yaml:"mcp_config_path,omitempty"`
}

// PlanningConfig controls the model and behavior for epic planning subprocesses.
// Planning runs benefit from stronger reasoning (Opus); execution runs stay on
// the cheaper, verbose-friendly Sonnet. GH-2432.
type PlanningConfig struct {
	// Model is the Claude model name passed to the planning subprocess via
	// --model and the ANTHROPIC_MODEL env var. Default: "claude-opus-4-7".
	Model string `yaml:"model,omitempty"`
}

// DefaultPlanningConfig returns the default planning config (Opus 4.7).
func DefaultPlanningConfig() *PlanningConfig {
	return &PlanningConfig{Model: "claude-opus-4-7"}
}

// DefaultAllowedToolsExecution is the default --allowedTools list for execution
// subprocesses. Excludes MCP and Web* tools to cut per-turn context bloat.
// GH-2432.
func DefaultAllowedToolsExecution() []string {
	return []string{"Read", "Write", "Edit", "Bash", "Grep", "Glob", "Task"}
}

// DefaultAllowedToolsPlanning is the default --allowedTools list for the
// planning subprocess. Planning should not write code. GH-2432.
func DefaultAllowedToolsPlanning() []string {
	return []string{"Read", "Grep", "Glob"}
}

// QwenCodeConfig contains Qwen Code backend configuration.
// Qwen Code is an open-source CLI coding agent (Gemini CLI fork) by Alibaba.
// Uses subprocess execution with --output-format stream-json, similar to Claude Code.
type QwenCodeConfig struct {
	// Command is the path to the qwen CLI (default: "qwen")
	Command string `yaml:"command,omitempty"`

	// ExtraArgs are additional arguments to pass to the CLI
	ExtraArgs []string `yaml:"extra_args,omitempty"`

	// UseSessionResume enables --resume for session continuation.
	// Default: false
	UseSessionResume bool `yaml:"use_session_resume,omitempty"`
}

// CodexExecConfig contains Codex CLI non-interactive backend configuration.
type CodexExecConfig struct {
	// Command is the path to the codex CLI (default: "codex")
	Command string `yaml:"command,omitempty"`

	// Model is the default model passed with --model when ExecuteOptions.Model is empty.
	Model string `yaml:"model,omitempty"`

	// Effort is passed as -c model_reasoning_effort="<effort>" when ExecuteOptions.Effort is empty.
	Effort string `yaml:"effort,omitempty"`

	// Sandbox maps to codex exec --sandbox. Default: "workspace-write".
	Sandbox string `yaml:"sandbox,omitempty"`

	// ExtraArgs are additional arguments to pass before the prompt.
	ExtraArgs []string `yaml:"extra_args,omitempty"`

	// UseSessionResume enables codex exec resume for continued context.
	// Default: false.
	UseSessionResume bool `yaml:"use_session_resume,omitempty"`

	// Ephemeral runs without persisting Codex session files.
	Ephemeral bool `yaml:"ephemeral,omitempty"`

	// BypassApprovalsAndSandbox maps to --dangerously-bypass-approvals-and-sandbox.
	BypassApprovalsAndSandbox bool `yaml:"bypass_approvals_and_sandbox,omitempty"`

	// OutputSchemaPath passes --output-schema to codex exec.
	OutputSchemaPath string `yaml:"output_schema_path,omitempty"`
}

// OpenCodeConfig contains OpenCode backend configuration.
type OpenCodeConfig struct {
	// ServerURL is the OpenCode server URL (default: "http://127.0.0.1:4096")
	ServerURL string `yaml:"server_url,omitempty"`

	// Model is the model to use (e.g., "anthropic/claude-sonnet-4")
	Model string `yaml:"model,omitempty"`

	// Provider is the provider name (e.g., "anthropic")
	Provider string `yaml:"provider,omitempty"`

	// AutoStartServer starts the server if not running
	AutoStartServer bool `yaml:"auto_start_server,omitempty"`

	// ServerCommand is the command to start the server (default: "opencode serve")
	ServerCommand string `yaml:"server_command,omitempty"`

	// RequestTimeout is the maximum time to wait for OpenCode HTTP responses.
	// Applies to session creation, message send, and SSE response headers.
	// Default: "10m"
	RequestTimeout string `yaml:"request_timeout,omitempty"`
}

// EffectiveRequestTimeout returns the OpenCode HTTP request timeout.
// Falls back to 10m when empty or invalid.
func (c *OpenCodeConfig) EffectiveRequestTimeout() time.Duration {
	if c == nil || c.RequestTimeout == "" {
		return 10 * time.Minute
	}
	d, err := time.ParseDuration(c.RequestTimeout)
	if err != nil || d <= 0 {
		return 10 * time.Minute
	}
	return d
}

// OpenAIConfig contains configuration for the openai-api direct HTTP backend.
// Supports any provider exposing an OpenAI-compatible /v1/chat/completions endpoint:
// OpenAI, OpenRouter, Groq, Together, Synthetic, vLLM, Ollama, etc.
type OpenAIConfig struct {
	// APIKey is the Bearer token. Supports ${ENV_VAR} expansion.
	// Falls back to OPENAI_API_KEY env var when empty.
	APIKey string `yaml:"api_key,omitempty"`

	// BaseURL is the provider base URL (default: https://api.openai.com/v1).
	// Must expose /chat/completions under this prefix.
	BaseURL string `yaml:"base_url,omitempty"`

	// Model is the default model name (default: gpt-4o).
	Model string `yaml:"model,omitempty"`
}
