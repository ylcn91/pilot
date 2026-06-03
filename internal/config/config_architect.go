package config

import (
	"fmt"

	"github.com/robfig/cron/v3"

	"github.com/ylcn91/pilot/internal/executor"
)

// DefaultArchitectMaxTickets is the emit cap applied at use-site when an enabled
// ArchitectConfig leaves MaxTickets at zero. The Architect is noisy at SCAN by
// design; this keeps a single run from flooding a repo with issues.
const DefaultArchitectMaxTickets = 10

// Architect export targets select where a run files its proposals. github (the
// default, preserving prior behaviour) files GitHub issues; linear files one
// Linear sub-issue per finding under an existing parent epic; adr writes an ADR
// document and is only meaningful for the RFC lens.
const (
	ArchitectExportGitHub = "github"
	ArchitectExportLinear = "linear"
	ArchitectExportADR    = "adr"

	// DefaultArchitectExport is the export target applied when none is set,
	// preserving the historical GitHub-issue behaviour.
	DefaultArchitectExport = ArchitectExportGitHub
)

// ValidArchitectExports lists the accepted export targets, for validation and
// help text.
var ValidArchitectExports = []string{ArchitectExportGitHub, ArchitectExportLinear, ArchitectExportADR}

// ArchitectThresholds bounds the deterministic SCAN collectors. LOC flags Go
// files at or over the given line count; MinCoverage flags packages below the
// given coverage fraction. Zero values fall back to the collectors' own
// defaults at use-site.
type ArchitectThresholds struct {
	LOC         int     `yaml:"loc,omitempty"`
	MinCoverage float64 `yaml:"min_coverage,omitempty"`
}

// ArchitectConfig configures the proactive Architect pipeline (SCAN -> PROPOSE
// -> EMIT). When nil or Enabled=false, the `pilot architect` command and any
// scheduled run are inert. Backend (when set) selects the PROPOSE analyzer's
// backend, reusing the same StageConfig validation as the executor pipeline/TDD
// blocks; a nil Backend falls back to the run's primary executor backend.
type ArchitectConfig struct {
	// Enabled gates the entire Architect feature. False/absent => inert.
	Enabled bool `yaml:"enabled,omitempty"`

	// Schedule is an optional cron expression for unattended runs. Empty means
	// run on demand only. When set it must parse as a standard 5-field cron.
	Schedule string `yaml:"schedule,omitempty"`

	// Timezone names the IANA zone the Schedule is evaluated in (e.g.
	// "America/New_York"). Empty defaults to the host local time at use-site.
	Timezone string `yaml:"timezone,omitempty"`

	// Backend overrides the PROPOSE analyzer backend. Nil => primary backend.
	Backend *executor.StageConfig `yaml:"backend,omitempty"`

	// MaxTickets caps how many ranked proposals a run emits as issues. Zero =>
	// DefaultArchitectMaxTickets at use-site.
	MaxTickets int `yaml:"max_tickets,omitempty"`

	// Export selects where proposals are filed: "github" (default), "linear", or
	// "adr". Empty falls back to DefaultArchitectExport at use-site. The --export
	// flag overrides this per run.
	Export string `yaml:"export,omitempty"`

	// Labels are applied to every filed issue. Empty falls back to the
	// emitter's default pilot+architect label set.
	Labels []string `yaml:"labels,omitempty"`

	// Thresholds tunes the deterministic SCAN collectors.
	Thresholds ArchitectThresholds `yaml:"thresholds,omitempty"`

	// Signals optionally restricts which SCAN collectors run (by Kind). Empty
	// runs the full default collector set.
	Signals []string `yaml:"signals,omitempty"`
}

// Validate checks an enabled ArchitectConfig: the Backend (when present) must
// target a runnable backend, MaxTickets must be >= 1 when set, and a non-empty
// Schedule must cron-parse. A nil or disabled config is always valid.
//
// codex-app-server is rejected explicitly: it is a long-lived app runtime, not a
// runnable Backend for the Architect PROPOSE stage. Use codex-exec instead.
func (a *ArchitectConfig) Validate() error {
	if a == nil || !a.Enabled {
		return nil
	}

	if a.Backend != nil {
		if err := validateArchitectBackend(a.Backend); err != nil {
			return err
		}
	}

	if a.MaxTickets != 0 && a.MaxTickets < 1 {
		return fmt.Errorf("architect.max_tickets must be >= 1, got %d", a.MaxTickets)
	}

	if a.Export != "" && !isValidArchitectExport(a.Export) {
		return fmt.Errorf("architect.export must be one of %v, got %q", ValidArchitectExports, a.Export)
	}

	if a.Schedule != "" {
		if _, err := cron.ParseStandard(a.Schedule); err != nil {
			return fmt.Errorf("architect.schedule is not a valid cron expression: %w", err)
		}
	}

	return nil
}

func validateArchitectBackend(stage *executor.StageConfig) error {
	if stage.Type == "" {
		return fmt.Errorf("architect.backend.type is required when architect.backend is present")
	}
	switch stage.Type {
	case executor.BackendTypeCodexExec,
		executor.BackendTypeClaudeCode,
		executor.BackendTypeQwenCode,
		executor.BackendTypeAnthropicAPI,
		executor.BackendTypeOpenAIAPI,
		executor.BackendTypeOpenCode:
		return nil
	case "codex-app-server":
		return fmt.Errorf("architect.backend.type %q is not a runnable Backend for architect mode; use codex-exec", stage.Type)
	default:
		return fmt.Errorf("architect.backend.type %q is not a known backend (use one of: claude-code, codex-exec, qwen-code, anthropic-api, openai-api, opencode)", stage.Type)
	}
}

// isValidArchitectExport reports whether target names a supported export.
func isValidArchitectExport(target string) bool {
	for _, v := range ValidArchitectExports {
		if v == target {
			return true
		}
	}
	return false
}
