package executor

import "time"

// Config returns the runner's backend configuration.
func (r *Runner) Config() *BackendConfig {
	return r.config
}

// backendType returns the configured backend type, defaulting to "codex-exec".
func (r *Runner) backendType() string {
	if r.config != nil && r.config.Type != "" {
		return r.config.Type
	}
	return BackendTypeCodexExec
}

func (r *Runner) backendCommand() string {
	if r.config == nil {
		return ""
	}
	switch r.backendType() {
	case BackendTypeClaudeCode:
		if r.config.ClaudeCode != nil {
			return r.config.ClaudeCode.Command
		}
	case BackendTypeCodexExec:
		if r.config.CodexExec != nil {
			return r.config.CodexExec.Command
		}
	case BackendTypeQwenCode:
		if r.config.QwenCode != nil {
			return r.config.QwenCode.Command
		}
	}
	return ""
}

// selfReviewTimeout returns the per-backend timeout for the self-review phase.
// OpenCode runs are legitimately slower than Claude Code (server-managed
// session, larger streaming overhead); a 2-minute cap cancels review while the
// backend is still working and surfaces as a false regression. GH-2416.
// effectiveStallTimeout returns the stall detection threshold from config.
// Delegates to BackendConfig.EffectiveStallTimeout(); returns the 3m default when
// no config is set. TASK-308.
func (r *Runner) effectiveStallTimeout() time.Duration {
	return r.config.EffectiveStallTimeout()
}

func (r *Runner) selfReviewTimeout() time.Duration {
	if r.backendType() == BackendTypeOpenCode {
		return 10 * time.Minute
	}
	return 2 * time.Minute
}

// fallbackModelName returns the best-known model name for telemetry rows when
// the backend stream did not surface a model field. Used to distinguish
// "telemetry-missing" from "true-zero" runs in execution_metrics. Resolution:
//  1. config.DefaultModel (set when running via OpenCode/GLM/etc.)
//  2. OpenCode config.Model (e.g. "anthropic/claude-sonnet-4-6")
//  3. Backend type prefix (e.g. "claude-code", "opencode") — never empty.
//
// GH-2428: previously runner.go hardcoded "claude-opus-4-6" as the fallback,
// which (a) was stale (real Claude Code runs report 4-7) and (b) silently
// labelled OpenCode/GLM runs as Claude Opus, biasing cost/model metrics.
func (r *Runner) fallbackModelName() string {
	if r.config != nil {
		if r.config.DefaultModel != "" {
			return r.config.DefaultModel
		}
		if r.config.OpenCode != nil && r.config.Type == BackendTypeOpenCode && r.config.OpenCode.Model != "" {
			return r.config.OpenCode.Model
		}
	}
	return r.backendType()
}

// executionToolOptions returns the AllowedTools and MCPConfigPath that should
// be applied to every backend.Execute call site driven by this Runner. These
// shave the per-turn token cost by scoping the subprocess toolbox. GH-2432.
func (r *Runner) executionToolOptions() (allowed []string, mcpPath string) {
	if r.config != nil && r.config.ClaudeCode != nil {
		return r.config.ClaudeCode.AllowedTools, r.config.ClaudeCode.MCPConfigPath
	}
	return nil, ""
}

// effectiveStageModelEffort resolves the model/effort to pass in ExecuteOptions
// for a given pipeline/TDD stage, honoring the precedence:
//
//	stage override > run-level selected > backend default
//
// Backends prefer the per-call ExecuteOptions over their baked-in config (e.g.
// codex-exec's appendCommonArgs does firstNonEmpty(opts.Model, b.config.Model)),
// so without this the run-level selectedModel/selectedEffort would shadow a
// stage's NewStageBackend-baked override. A nil stage yields the run-level
// values unchanged (no-pipeline / no-TDD path is byte-identical to before).
func effectiveStageModelEffort(stage *StageConfig, selectedModel, selectedEffort string) (effModel, effEffort string) {
	if stage == nil {
		return selectedModel, selectedEffort
	}
	return firstNonEmpty(stage.Model, selectedModel), firstNonEmpty(stage.Effort, selectedEffort)
}

// resolveSelectedModel returns the model name to pass to the backend for a task.
// GH-2450: model_routing wins when configured. Only fall back to default_model
// (or CC empty-passthrough) when the router returned an empty string. Setting
// default_model previously clobbered routing for the Claude Code backend.
func (r *Runner) resolveSelectedModel(task *Task) string {
	model := r.modelRouter.SelectModel(task)
	if model != "" {
		return model
	}
	if r.config == nil || r.config.DefaultModel == "" {
		return ""
	}
	if r.config.Type == BackendTypeClaudeCode {
		// CC reads ANTHROPIC_MODEL / its own settings; pass empty to avoid
		// overriding the user's CC-side configuration.
		return ""
	}
	return r.config.DefaultModel
}
