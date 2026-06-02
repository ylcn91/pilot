package executor

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// buildExecArgs assembles the Claude Code CLI argument list for a run.
// When allowFromPR is false, --from-pr is skipped even if opts.FromPR is set,
// enabling fallback retry without --from-pr when the session is not found.
func (b *ClaudeCodeBackend) buildExecArgs(opts ExecuteOptions, allowFromPR bool) []string {
	var args []string

	// GH-1267: Use --from-pr for session resumption from PR context
	// This takes precedence over --resume since it's more specific.
	if opts.FromPR > 0 && allowFromPR && b.config.UseFromPR {
		args = []string{
			"--from-pr", strconv.Itoa(opts.FromPR),
			"-p", opts.Prompt,
			"--verbose",
			"--output-format", "stream-json",
			"--dangerously-skip-permissions",
		}
		b.log.Info("Resuming session from PR context",
			slog.Int("pr", opts.FromPR),
		)
	} else if opts.ResumeSessionID != "" {
		// GH-1265: Use --resume for session continuation (e.g., self-review)
		args = []string{
			"--resume", opts.ResumeSessionID,
			"-p", opts.Prompt,
			"--verbose",
			"--output-format", "stream-json",
			"--dangerously-skip-permissions",
		}
		b.log.Info("Resuming session for context continuation",
			slog.String("session_id", opts.ResumeSessionID),
		)
	} else {
		args = []string{
			"-p", opts.Prompt,
			"--verbose",
			"--output-format", "stream-json",
			"--dangerously-skip-permissions",
		}
	}

	// Add model flag if specified (model routing GH-215)
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
		b.log.Info("Using routed model", slog.String("model", opts.Model))
	}

	// Add max-turns flag if set by .pilot/workflow.yaml agent.max_turns (TASK-304)
	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(opts.MaxTurns))
		b.log.Info("Using workflow max_turns override", slog.Int("max_turns", opts.MaxTurns))
	}

	// Add effort flag if specified (effort routing)
	// Note: Claude Code CLI may not support --effort yet; this is future-proofed.
	if opts.Effort != "" {
		args = append(args, "--effort", opts.Effort)
		b.log.Info("Using routed effort", slog.String("effort", opts.Effort))
	}

	// GH-2432: --allowedTools restricts the subprocess toolbox; --mcp-config
	// scopes which MCP servers (if any) the subprocess loads. Both cut the
	// per-turn token cost considerably when MCPs are not needed.
	if len(opts.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(opts.AllowedTools, ","))
	}
	if opts.MCPConfigPath != "" {
		args = append(args, "--mcp-config", opts.MCPConfigPath)
	}

	args = append(args, b.config.ExtraArgs...)

	return args
}

// buildExecEnv assembles the subprocess environment for a Claude Code run.
func (b *ClaudeCodeBackend) buildExecEnv() []string {
	// GH-2328: Signal executor mode to the child process. Project `CLAUDE.md`
	// and auto-memory can detect this and skip Navigator-only "DO NOT write
	// code" rules without relying on prompt-prefix heuristics.
	env := append(os.Environ(), "PILOT_EXECUTOR=1")
	// GH-2371: route the CC subprocess to the configured provider when set.
	// Values are appended after os.Environ() so they win on Node's last-write
	// lookup if the user's shell also exports ANTHROPIC_*.
	if b.apiBaseURL != "" {
		env = append(env, "ANTHROPIC_BASE_URL="+b.apiBaseURL)
	}
	if b.apiAuthToken != "" {
		env = append(env, "ANTHROPIC_AUTH_TOKEN="+b.apiAuthToken)
	}
	if b.defaultModel != "" {
		env = append(env, "ANTHROPIC_MODEL="+b.defaultModel)
	}
	// Pass context window and output token env vars if configured (GH-2163).
	if b.config.Disable1MContext {
		env = append(env, "CLAUDE_CODE_DISABLE_1M_CONTEXT=1")
	}
	if b.config.MaxOutputTokens > 0 {
		env = append(env, fmt.Sprintf("CLAUDE_CODE_MAX_OUTPUT_TOKENS=%d", b.config.MaxOutputTokens))
	}
	return env
}
